package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/owulveryck/slideAppScripter/internal/vertex"
)

const outlinerSystemPrompt = `Tu es un expert en structuration de présentations professionnelles.
Ton rôle est d'analyser une demande utilisateur et de produire un plan de présentation structuré.

IMPORTANT : Tu ne connais PAS les slides template disponibles. Tu dois raisonner uniquement sur la structure logique de la présentation.

RÈGLES :
1. Extrais le titre de la présentation.
2. Identifie les sections logiques (introduction, sections de contenu, conclusion).
3. Pour chaque section, détermine les slides nécessaires.
4. Pour chaque slide, extrais les éléments de contenu TEXTUELS directement depuis la demande utilisateur.
5. Ne FABRIQUE aucun texte. Utilise uniquement le contenu de la demande.
6. Compte précisément le nombre d'éléments de contenu (itemCount) et la longueur du plus long (maxItemLength).
   Indique si la slide nécessite un titre (needsTitle) et/ou un sous-titre (needsSubtitle).
   needsSubtitle=true UNIQUEMENT si la demande utilisateur fournit explicitement un sous-titre pour cette slide.
7. Classe chaque slide par type : "cover", "section_divider", "content", "data", "conclusion".
8. Chaque slide doit avoir un "intent" décrivant ce qu'elle doit transmettre.

TYPES DE SLIDES :
- "cover" : slide de couverture/titre
- "section_divider" : intercalaire de section (titre de section uniquement)
- "content" : slide de contenu (textes, bullet points, arguments)
- "data" : slide de données (chiffres, tableaux, graphiques)
- "conclusion" : slide de conclusion ou remerciement`

// OutlinerAgent analyzes the user request and produces a structured
// presentation outline independently of available templates.
type OutlinerAgent struct {
	client *vertex.Client
	model  string
}

// NewOutlinerAgent creates an OutlinerAgent with the given Vertex AI client and model name.
func NewOutlinerAgent(client *vertex.Client, model string) *OutlinerAgent {
	return &OutlinerAgent{client: client, model: model}
}

func (a *OutlinerAgent) outlinerTool() vertex.Tool {
	return vertex.Tool{
		Name:        "produce_outline",
		Description: "Produit le plan structuré de la présentation à partir de la demande utilisateur.",
		InputSchema: json.RawMessage(`{
	"type": "object",
	"properties": {
		"presentationTitle": {
			"type": "string",
			"description": "Titre de la présentation"
		},
		"sections": {
			"type": "array",
			"items": {
				"type": "object",
				"properties": {
					"title": {
						"type": "string",
						"description": "Titre de la section"
					},
					"purpose": {
						"type": "string",
						"description": "Rôle de la section : introduction, contenu, conclusion"
					},
					"slideNeeds": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"intent": {
									"type": "string",
									"description": "Ce que cette slide doit transmettre"
								},
								"contentItems": {
									"type": "array",
									"items": {"type": "string"},
									"description": "Textes extraits de la demande utilisateur pour cette slide"
								},
								"itemCount": {
									"type": "integer",
									"description": "Nombre d'éléments de contenu"
								},
								"maxItemLength": {
									"type": "integer",
									"description": "Longueur en caractères du plus long élément"
								},
								"needsTitle": {
									"type": "boolean",
									"description": "True si la slide a besoin d'un titre"
								},
								"needsSubtitle": {
									"type": "boolean",
									"description": "True si la slide a besoin d'un sous-titre (uniquement si la demande utilisateur fournit explicitement un sous-titre pour cette slide)"
								},
								"slideType": {
									"type": "string",
									"enum": ["cover", "section_divider", "content", "data", "conclusion"],
									"description": "Type de slide"
								}
							},
							"required": ["intent", "contentItems", "itemCount", "maxItemLength", "needsTitle", "needsSubtitle", "slideType"]
						}
					}
				},
				"required": ["title", "purpose", "slideNeeds"]
			}
		}
	},
	"required": ["presentationTitle", "sections"]
}`),
	}
}

// Run executes the Outliner agent: sends the user request to Claude and parses
// the structured PresentationOutline from the tool_use response.
func (a *OutlinerAgent) Run(ctx context.Context, userRequest string, templateInstructions string) (*PresentationOutline, error) {
	slog.Info("[agent:outliner] starting structural analysis", "model", a.model)
	start := time.Now()

	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{{
			Type: "text",
			Text: fmt.Sprintf("Analyse cette demande de présentation et produis un plan structuré :\n\n%s", userRequest),
		}},
	}}

	systemPrompt := outlinerSystemPrompt
	if templateInstructions != "" {
		systemPrompt += "\n\nINSTRUCTIONS SPÉCIFIQUES AU TEMPLATE :\n" + templateInstructions
	}

	tool := a.outlinerTool()
	resp, err := a.client.RawPredictFull(ctx, a.model, messages,
		vertex.WithSystem(systemPrompt),
		vertex.WithTools([]vertex.Tool{tool}),
		vertex.WithToolChoice(map[string]any{"type": "tool", "name": "produce_outline"}),
		vertex.WithTemperature(0.2),
		vertex.WithMaxTokens(16384),
	)
	if err != nil {
		return nil, fmt.Errorf("outliner API call failed: %w", err)
	}

	if resp.StopReason == "max_tokens" {
		return nil, fmt.Errorf("outliner: response truncated (max_tokens reached), input may be too large")
	}

	block := resp.ToolUseBlock()
	if block == nil {
		return nil, fmt.Errorf("outliner: no tool_use block in response")
	}

	var outline PresentationOutline
	if err := json.Unmarshal(block.Input, &outline); err != nil {
		slog.Error("[agent:outliner] failed to parse tool_use input",
			"error", err,
			"raw", string(block.Input[:min(len(block.Input), 500)]),
		)
		return nil, fmt.Errorf("outliner: failed to parse outline: %w", err)
	}

	totalSlides := 0
	for _, sec := range outline.Sections {
		totalSlides += len(sec.SlideNeeds)
		slog.Info("[agent:outliner]   section",
			"title", sec.Title,
			"purpose", sec.Purpose,
			"slides", len(sec.SlideNeeds),
		)
	}

	slog.Info("[agent:outliner] done",
		"title", outline.PresentationTitle,
		"sections", len(outline.Sections),
		"totalSlides", totalSlides,
		"duration", time.Since(start).Round(time.Millisecond),
	)

	return &outline, nil
}
