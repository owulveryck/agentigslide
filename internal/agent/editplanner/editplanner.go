package editplanner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/owulveryck/agentigslide/internal/agent"
	"github.com/owulveryck/agentigslide/internal/model"
	"github.com/owulveryck/agentigslide/internal/vertex"
)

// Agent analyzes an existing presentation and a user's edit request, then
// produces a structured EditPlan describing the modifications to apply.
type Agent struct {
	client    *vertex.Client
	model     string
	maxTokens int
}

// New creates an Agent with the given Vertex AI client, model name, and
// max output tokens.
func New(client *vertex.Client, model string, maxTokens int) *Agent {
	return &Agent{client: client, model: model, maxTokens: maxTokens}
}

func (a *Agent) editPlanTool() vertex.Tool {
	return vertex.Tool{
		Name:        "produce_edit_plan",
		Description: "Produit le plan de modifications à appliquer à la présentation existante.",
		InputSchema: json.RawMessage(`{
	"type": "object",
	"properties": {
		"operations": {
			"type": "array",
			"items": {
				"type": "object",
				"properties": {
					"type": {
						"type": "string",
						"enum": ["modify_content", "replace_slide", "insert_slide", "delete_slide"],
						"description": "Type d'opération"
					},
					"slideIndex": {
						"type": "integer",
						"description": "Index de la slide cible (base 0, dans l'état actuel)"
					},
					"modifications": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"variableName": {
									"type": "string",
									"description": "ObjectID de l'élément texte à modifier"
								},
								"newText": {
									"type": "string",
									"description": "Nouveau contenu textuel (supporte le markdown : **gras**, *italique*, listes à puces)"
								}
							},
							"required": ["variableName", "newText"]
						},
						"description": "Modifications textuelles (pour modify_content)"
					},
					"newSourceSlide": {
						"type": "integer",
						"description": "Numéro de slide template pour remplacement/insertion"
					},
					"slideContent": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"variableName": {
									"type": "string",
									"description": "Nom de variable du champ dans le template"
								},
								"newText": {
									"type": "string",
									"description": "Contenu textuel pour ce champ"
								}
							},
							"required": ["variableName", "newText"]
						},
						"description": "Contenu pour les champs du template (pour replace_slide et insert_slide)"
					},
					"insertPosition": {
						"type": "integer",
						"description": "Position d'insertion (base 0, pour insert_slide)"
					},
					"intention": {
						"type": "string",
						"description": "Objectif de la slide (pour insert_slide et replace_slide)"
					},
					"rationale": {
						"type": "string",
						"description": "Justification de l'opération"
					}
				},
				"required": ["type", "slideIndex", "rationale"]
			}
		}
	},
	"required": ["operations"]
}`),
	}
}

// Run executes the EditPlanner agent: sends the presentation state and user
// request to Claude and parses the structured EditPlan from the tool_use response.
func (a *Agent) Run(ctx context.Context, presentationID string, slides []model.ExistingSlideInfo, userRequest string, compactCatalog string, templateInstructions string) (*model.EditPlan, vertex.Usage, error) {
	slog.Info("[agent:editplanner] starting edit analysis", "model", a.model, "slides", len(slides))
	start := time.Now()

	presentationDesc := formatPresentationState(slides)

	var userPrompt strings.Builder
	userPrompt.WriteString("PRÉSENTATION EXISTANTE :\n\n")
	userPrompt.WriteString(presentationDesc)
	userPrompt.WriteString("\n\n---\n\nCATALOGUE DE TEMPLATES DISPONIBLES :\n\n")
	userPrompt.WriteString(compactCatalog)
	userPrompt.WriteString("\n\n---\n\nDEMANDE DE MODIFICATION :\n\n")
	userPrompt.WriteString(userRequest)

	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{{
			Type: "text",
			Text: userPrompt.String(),
		}},
	}}

	tool := a.editPlanTool()
	resp, err := a.client.RawPredictFull(ctx, a.model, messages,
		vertex.WithSystemBlocks(agent.BuildSystemBlocks(systemPrompt, templateInstructions)),
		vertex.WithTools([]vertex.Tool{tool}),
		vertex.WithToolChoice(map[string]any{"type": "tool", "name": "produce_edit_plan"}),
		vertex.WithTemperature(0.2),
		vertex.WithMaxTokens(a.maxTokens),
	)
	if err != nil {
		return nil, vertex.Usage{}, fmt.Errorf("editplanner API call failed: %w", err)
	}

	slog.Info("[agent:editplanner] API usage",
		"inputTokens", resp.Usage.InputTokens,
		"outputTokens", resp.Usage.OutputTokens,
		"cacheRead", resp.Usage.CacheReadInputTokens,
		"cacheWrite", resp.Usage.CacheCreationInputTokens,
	)

	if resp.StopReason == "max_tokens" {
		return nil, resp.Usage, fmt.Errorf("editplanner: response truncated (max_tokens reached)")
	}

	block := resp.ToolUseBlock()
	if block == nil {
		return nil, resp.Usage, fmt.Errorf("editplanner: no tool_use block in response")
	}

	var result struct {
		Operations []model.EditOperation `json:"operations"`
	}
	if err := json.Unmarshal(block.Input, &result); err != nil {
		slog.Error("[agent:editplanner] failed to parse tool_use input",
			"error", err,
			"raw", string(block.Input[:min(len(block.Input), 500)]),
		)
		return nil, resp.Usage, fmt.Errorf("editplanner: failed to parse edit plan: %w", err)
	}

	plan := &model.EditPlan{
		PresentationID: presentationID,
		Operations:     result.Operations,
	}

	slog.Info("[agent:editplanner] done",
		"operations", len(plan.Operations),
		"duration", time.Since(start).Round(time.Millisecond),
	)

	return plan, resp.Usage, nil
}

// formatPresentationState produces a human-readable description of the
// presentation's current slides and their text content.
func formatPresentationState(slides []model.ExistingSlideInfo) string {
	var b strings.Builder
	for _, slide := range slides {
		fmt.Fprintf(&b, "Slide %d (pageId: %s)\n", slide.Index, slide.PageObjectID)
		if len(slide.TextElements) == 0 {
			b.WriteString("  (aucun élément texte)\n")
		}
		for _, el := range slide.TextElements {
			if el.CellLocation != nil {
				fmt.Fprintf(&b, "  - [%s] objectId=%s cellule(%d,%d) : %q\n",
					el.ShapeType, el.ObjectID, el.CellLocation.RowIndex, el.CellLocation.ColumnIndex, truncate(el.Content, 120))
			} else {
				fmt.Fprintf(&b, "  - [%s] objectId=%s : %q\n",
					el.ShapeType, el.ObjectID, truncate(el.Content, 120))
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
