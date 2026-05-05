package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/owulveryck/slideAppScripter/internal/model"
	"github.com/owulveryck/slideAppScripter/internal/vertex"
)

const writerSystemPrompt = `Tu es un rédacteur de contenu pour des slides de présentation professionnelle.
Ton rôle est de prendre les éléments de contenu bruts extraits de la demande utilisateur et de les mapper dans les champs d'un slide template.

RÈGLES :
1. PAS D'INVENTION : Utilise uniquement le texte fourni dans les éléments de contenu. Ne fabrique rien.
2. RESPECT DES TAILLES : Chaque champ a une capacité maximale en caractères. Ne la dépasse JAMAIS. Si le texte est trop long, résume-le.
3. Formatage markdown autorisé : **gras**, *italique*, listes avec - (2 espaces pour sous-items)
4. Adapte le style au rôle du champ (titre = court et percutant, contenu = structuré et clair).
5. REMPLIS TOUS les champs du template — un champ vide affiche un placeholder visible.
6. Si le nombre de contentItems ne correspond pas au nombre de champs contenu, adapte : fusionne des items ou répartis un item sur plusieurs champs.
7. Les champs de rôle "titre" ou "titre_principal" reçoivent un titre concis généré depuis l'intent.
8. Les champs de rôle "sous-titre" qui n'ont pas de contenu dédié doivent recevoir un court texte contextuel ou un espace.`

// WriterAgent generates the text content for a single slide.
type WriterAgent struct {
	client *vertex.Client
	model  string
}

// NewWriterAgent creates a WriterAgent.
func NewWriterAgent(client *vertex.Client, model string) *WriterAgent {
	return &WriterAgent{client: client, model: model}
}

func (a *WriterAgent) writerTool() vertex.Tool {
	return vertex.Tool{
		Name:        "produce_slide_content",
		Description: "Produit le contenu textuel final pour chaque champ du slide.",
		InputSchema: json.RawMessage(`{
	"type": "object",
	"properties": {
		"modifications": {
			"type": "array",
			"items": {
				"type": "object",
				"properties": {
					"variableName": {
						"type": "string",
						"description": "Nom du champ (doit correspondre exactement au template)"
					},
					"newText": {
						"type": "string",
						"description": "Texte final pour ce champ (markdown autorisé)"
					}
				},
				"required": ["variableName", "newText"]
			}
		}
	},
	"required": ["modifications"]
}`),
	}
}

// WriteSlide generates text modifications for a single slide based on the
// template fields and content items from the outline. The writer maps content
// items to the template's actual fields. Optional feedback from a previous
// review pass is injected into the prompt so the Writer can correct its output.
func (a *WriterAgent) WriteSlide(ctx context.Context, sourceSlide int, slideNeed SlideNeed, templateFields []TemplateField, templateInstructions string, feedback ...ReviewIssue) (*SlideContent, error) {
	slog.Info("[agent:writer] starting",
		"sourceSlide", sourceSlide,
		"model", a.model,
		"templateFields", len(templateFields),
		"intent", slideNeed.Intent,
		"contentItems", len(slideNeed.ContentItems),
		"feedback", len(feedback),
	)
	start := time.Now()

	var fieldDescriptions []string
	for _, tf := range templateFields {
		label := tf.Role
		if tf.MaxChars > 0 {
			label += fmt.Sprintf(", max ~%d chars", tf.MaxChars)
		}
		fieldDescriptions = append(fieldDescriptions, fmt.Sprintf(
			"- %s (%s)", tf.VariableName, label,
		))
	}

	var contentSection string
	if len(slideNeed.ContentItems) > 0 {
		var items []string
		for i, item := range slideNeed.ContentItems {
			items = append(items, fmt.Sprintf("  [%d] %s", i, item))
		}
		contentSection = fmt.Sprintf("\n\nCONTENU À PLACER :\n%s", strings.Join(items, "\n"))
	}

	var feedbackSection string
	if len(feedback) > 0 {
		var issues []string
		for _, fb := range feedback {
			entry := fmt.Sprintf("- [%s] %s", fb.IssueType, fb.Description)
			if fb.Field != "" {
				entry = fmt.Sprintf("- [%s] Champ \"%s\" : %s", fb.IssueType, fb.Field, fb.Description)
			}
			if fb.Suggestion != "" {
				entry += fmt.Sprintf(" → Suggestion : %s", fb.Suggestion)
			}
			issues = append(issues, entry)
		}
		feedbackSection = fmt.Sprintf("\n\nCORRECTIONS DEMANDÉES (issues détectées lors de la revue précédente) :\n%s\n\nCorrige ces problèmes dans ta réponse.", strings.Join(issues, "\n"))
	}

	prompt := fmt.Sprintf(`SLIDE : Template n°%d
INTENTION : %s

CHAMPS DU TEMPLATE :
%s%s%s

Mappe chaque contentItem dans le champ contenu le plus adapté.
Pour les champs titre, génère un titre concis depuis l'intent.
Remplis TOUS les champs — adapte le contenu si le nombre d'items ne correspond pas exactement au nombre de champs.
Respecte les capacités maximales.`,
		sourceSlide,
		slideNeed.Intent,
		strings.Join(fieldDescriptions, "\n"),
		contentSection,
		feedbackSection,
	)

	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{{
			Type: "text",
			Text: prompt,
		}},
	}}

	systemPrompt := writerSystemPrompt
	if templateInstructions != "" {
		systemPrompt += "\n\nINSTRUCTIONS SPÉCIFIQUES AU TEMPLATE :\n" + templateInstructions
	}

	tool := a.writerTool()
	resp, err := a.client.RawPredictFull(ctx, a.model, messages,
		vertex.WithSystem(systemPrompt),
		vertex.WithTools([]vertex.Tool{tool}),
		vertex.WithToolChoice(map[string]any{"type": "tool", "name": "produce_slide_content"}),
		vertex.WithTemperature(0.2),
		vertex.WithMaxTokens(4096),
	)
	if err != nil {
		return nil, fmt.Errorf("writer API call failed for slide %d: %w", sourceSlide, err)
	}

	if resp.StopReason == "max_tokens" {
		return nil, fmt.Errorf("writer: response truncated for slide %d (max_tokens reached)", sourceSlide)
	}

	block := resp.ToolUseBlock()
	if block == nil {
		return nil, fmt.Errorf("writer: no tool_use block in response for slide %d", sourceSlide)
	}

	var result struct {
		Modifications []model.TextModification `json:"modifications"`
	}
	if err := json.Unmarshal(block.Input, &result); err != nil {
		return nil, fmt.Errorf("writer: failed to parse content for slide %d: %w", sourceSlide, err)
	}

	slog.Info("[agent:writer] done",
		"sourceSlide", sourceSlide,
		"fields", len(result.Modifications),
		"duration", time.Since(start).Round(time.Millisecond),
	)

	return &SlideContent{
		SourceSlide:   sourceSlide,
		Modifications: result.Modifications,
	}, nil
}
