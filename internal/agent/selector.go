package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/owulveryck/slideAppScripter/internal/vertex"
)

const selectorSystemPrompt = `Tu es un expert en sélection de slides template pour des présentations professionnelles.
Ton rôle est de faire correspondre chaque besoin de slide (décrit dans le plan structuré) avec le template le plus adapté du catalogue disponible.

LECTURE DU CATALOGUE :
Le catalogue indique pour chaque slide ses champs catégorisés : [T titre, S sous-titre, C contenu].
- "titre" = champ titre principal (maintitleShape, slidetitleShape, titlemainShape)
- "sous-titre" = champ sous-titre (subtitleShape)
- "contenu" = zones de contenu (tout le reste : quadrants, colonnes, cartes, blocs...)

RÈGLES DE SÉLECTION STRICTES :
1. CORRESPONDANCE CONTENU : itemCount du SlideNeed doit être EXACTEMENT ÉGAL au nombre de zones "contenu" du template. Exemple : itemCount=3 → template [1 titre, 3 contenu], PAS [1 titre, 4 contenu]. Toutes les zones contenu DOIVENT être remplies — un template partiellement rempli produit des zones vides avec du texte placeholder visible.
2. TITRE : Si needsTitle=true, le template doit avoir au moins 1 champ "titre". Le titre n'est PAS compté dans itemCount.
3. SOUS-TITRE : Ne sélectionne JAMAIS un template avec des champs "sous-titre" sauf si needsSubtitle=true. Un sous-titre non rempli laisse un placeholder visible sur la slide.
4. ADÉQUATION TAILLE : La longueur maximale d'un élément (maxItemLength) doit rentrer dans les capacités ~N caractères des champs contenu. Choisis des champs suffisamment grands.
5. ADÉQUATION DISPOSITION : La disposition de la slide doit correspondre au contenu. 3 éléments parallèles → slide 3 colonnes.
6. DIVERSITÉ : Utilise des slides variées. Ne réutilise pas toujours les mêmes templates.
7. TYPE DE SLIDE : Le type (cover, section_divider, content, data, conclusion) doit correspondre au slideType demandé.

MAPPING DES CHAMPS :
- Pour chaque slide sélectionnée, mappe TOUS les champs contenu du template (pas seulement certains).
- contentIndex est l'index (0-based) dans le tableau contentItems du SlideNeed correspondant.
- Reporte aussi le maxChars du champ pour guider la rédaction.
- Si needsTitle=true, mappe aussi le champ titre (contentIndex=-1 signifie que le Writer générera le titre depuis l'intent).`

// SelectorAgent maps each SlideNeed from the outline to the best template
// slide from the catalog.
type SelectorAgent struct {
	client *vertex.Client
	model  string
}

// NewSelectorAgent creates a SelectorAgent.
func NewSelectorAgent(client *vertex.Client, model string) *SelectorAgent {
	return &SelectorAgent{client: client, model: model}
}

func (a *SelectorAgent) selectorTool() vertex.Tool {
	return vertex.Tool{
		Name:        "select_templates",
		Description: "Sélectionne les templates les plus adaptés pour chaque besoin de slide et produit le plan de sélection.",
		InputSchema: json.RawMessage(`{
	"type": "object",
	"properties": {
		"selections": {
			"type": "array",
			"items": {
				"type": "object",
				"properties": {
					"outlineIndex": {
						"type": "integer",
						"description": "Index global du SlideNeed dans le plan (0-based, en comptant tous les slideNeeds de toutes les sections dans l'ordre)"
					},
					"sourceSlide": {
						"type": "integer",
						"description": "Numéro du slide template choisi dans le catalogue"
					},
					"rationale": {
						"type": "string",
						"description": "Justification du choix de ce template"
					},
					"fieldMapping": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"variableName": {
									"type": "string",
									"description": "Nom du champ dans le template (doit correspondre exactement au catalogue)"
								},
								"contentIndex": {
									"type": "integer",
									"description": "Index (0-based) du contentItem du SlideNeed à placer dans ce champ"
								},
								"maxChars": {
									"type": "integer",
									"description": "Capacité maximale du champ en caractères (depuis le catalogue)"
								}
							},
							"required": ["variableName", "contentIndex", "maxChars"]
						}
					}
				},
				"required": ["outlineIndex", "sourceSlide", "rationale", "fieldMapping"]
			}
		}
	},
	"required": ["selections"]
}`),
	}
}

// Run executes the Selector agent: sends the outline and catalog to Claude
// and returns the template selection plan.
func (a *SelectorAgent) Run(ctx context.Context, outline *PresentationOutline, compactCatalog string, templateInstructions string) (*SelectionPlan, error) {
	slog.Info("[agent:selector] mapping outline to templates", "model", a.model)
	start := time.Now()

	outlineJSON, err := json.MarshalIndent(outline, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("selector: failed to marshal outline: %w", err)
	}

	prompt := fmt.Sprintf(`PLAN STRUCTURÉ DE LA PRÉSENTATION :
%s

CATALOGUE DES SLIDES TEMPLATE DISPONIBLES :
%s

Pour chaque SlideNeed du plan, sélectionne le template le plus adapté et mappe les champs.
L'outlineIndex est l'index global du SlideNeed en parcourant toutes les sections dans l'ordre (0-based).`, string(outlineJSON), compactCatalog)

	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{{
			Type: "text",
			Text: prompt,
		}},
	}}

	systemPrompt := selectorSystemPrompt
	if templateInstructions != "" {
		systemPrompt += "\n\nINSTRUCTIONS SPÉCIFIQUES AU TEMPLATE :\n" + templateInstructions
	}

	tool := a.selectorTool()
	resp, err := a.client.RawPredictFull(ctx, a.model, messages,
		vertex.WithSystem(systemPrompt),
		vertex.WithTools([]vertex.Tool{tool}),
		vertex.WithToolChoice(map[string]any{"type": "tool", "name": "select_templates"}),
		vertex.WithTemperature(0.1),
		vertex.WithMaxTokens(16384),
	)
	if err != nil {
		return nil, fmt.Errorf("selector API call failed: %w", err)
	}

	if resp.StopReason == "max_tokens" {
		return nil, fmt.Errorf("selector: response truncated (max_tokens reached)")
	}

	block := resp.ToolUseBlock()
	if block == nil {
		return nil, fmt.Errorf("selector: no tool_use block in response")
	}

	var selPlan SelectionPlan
	if err := json.Unmarshal(block.Input, &selPlan); err != nil {
		return nil, fmt.Errorf("selector: failed to parse selection plan: %w", err)
	}

	for i, sel := range selPlan.Selections {
		slog.Info("[agent:selector]   slide mapped",
			"position", i+1,
			"sourceSlide", sel.SourceSlide,
			"fields", len(sel.FieldMapping),
			"rationale", sel.Rationale,
		)
	}

	slog.Info("[agent:selector] done",
		"selections", len(selPlan.Selections),
		"duration", time.Since(start).Round(time.Millisecond),
	)

	return &selPlan, nil
}
