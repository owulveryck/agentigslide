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

// RunInteractive executes the EditPlanner in an interactive loop. It produces
// an initial edit plan, then repeatedly accepts user feedback to refine it.
// The feedbackFn is called with the current plan; it returns either:
//   - ("", nil) if the user approves and wants to proceed
//   - (feedback, nil) if the user wants changes
//   - ("", err) on input error
func (a *Agent) RunInteractive(
	ctx context.Context,
	presentationID string,
	existingSlides []model.ExistingSlideInfo,
	userRequest string,
	compactCatalog string,
	templateInstructions string,
	feedbackFn func(*model.EditPlan) (string, error),
) (*model.EditPlan, []vertex.Usage, error) {
	slog.Info("[agent:editplanner] starting interactive mode", "model", a.model)
	start := time.Now()

	presentationDesc := formatPresentationState(existingSlides)

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
	sysBlocks := agent.BuildSystemBlocks(systemPrompt, templateInstructions)
	opts := []vertex.Option{
		vertex.WithSystemBlocks(sysBlocks),
		vertex.WithTools([]vertex.Tool{tool}),
		vertex.WithToolChoice(map[string]any{"type": "tool", "name": "produce_edit_plan"}),
		vertex.WithTemperature(0.2),
		vertex.WithMaxTokens(a.maxTokens),
	}

	var allUsages []vertex.Usage

	for round := 1; ; round++ {
		if ctx.Err() != nil {
			return nil, allUsages, ctx.Err()
		}

		slog.Info("[agent:editplanner] interactive round", "round", round)

		resp, err := a.client.RawPredictFull(ctx, a.model, messages, opts...)
		if err != nil {
			return nil, allUsages, fmt.Errorf("editplanner API call failed (round %d): %w", round, err)
		}

		allUsages = append(allUsages, resp.Usage)

		if resp.StopReason == "max_tokens" {
			return nil, allUsages, fmt.Errorf("editplanner: response truncated (round %d)", round)
		}

		block := resp.ToolUseBlock()
		if block == nil {
			return nil, allUsages, fmt.Errorf("editplanner: no tool_use block (round %d)", round)
		}

		var result struct {
			Operations []model.EditOperation `json:"operations"`
		}
		if err := json.Unmarshal(block.Input, &result); err != nil {
			return nil, allUsages, fmt.Errorf("editplanner: failed to parse (round %d): %w", round, err)
		}

		plan := &model.EditPlan{
			PresentationID: presentationID,
			Operations:     result.Operations,
		}

		slog.Info("[agent:editplanner] plan produced",
			"round", round,
			"operations", len(plan.Operations),
			"elapsed", time.Since(start).Round(time.Millisecond),
		)

		feedback, err := feedbackFn(plan)
		if err != nil {
			return nil, allUsages, fmt.Errorf("editplanner: feedback error: %w", err)
		}
		if feedback == "" {
			slog.Info("[agent:editplanner] user approved plan", "round", round)
			return plan, allUsages, nil
		}

		slog.Info("[agent:editplanner] user requested changes", "round", round)

		var respBlocks []vertex.ContentBlock
		for _, cb := range resp.Content {
			switch cb.Type {
			case "text":
				respBlocks = append(respBlocks, vertex.ContentBlock{Type: "text", Text: cb.Text})
			case "tool_use":
				respBlocks = append(respBlocks, vertex.ToolUseContentBlock(cb.ID, cb.Name, cb.Input))
			}
		}

		messages = append(messages,
			vertex.Message{Role: "assistant", Content: respBlocks},
			vertex.Message{
				Role: "user",
				Content: []vertex.ContentBlock{
					vertex.ToolResultContentBlock(block.ID, "Plan reçu. L'utilisateur demande des modifications."),
					{Type: "text", Text: fmt.Sprintf("Feedback de l'utilisateur :\n%s", feedback)},
				},
			},
		)
	}
}

// FormatEditPlan produces a human-readable summary of an edit plan.
func FormatEditPlan(plan *model.EditPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Plan d'édition (%d opérations) :\n\n", len(plan.Operations))
	for i, op := range plan.Operations {
		fmt.Fprintf(&b, "  %d. [%s] slide %d", i+1, op.Type, op.SlideIndex)
		if op.Intention != "" {
			fmt.Fprintf(&b, " — %s", op.Intention)
		}
		fmt.Fprintf(&b, "\n     %s\n", op.Rationale)
		if len(op.Modifications) > 0 {
			for _, mod := range op.Modifications {
				fmt.Fprintf(&b, "     → %s : %q\n", mod.VariableName, truncate(mod.NewText, 80))
			}
		}
		if op.NewSourceSlide > 0 {
			fmt.Fprintf(&b, "     template: slide %d\n", op.NewSourceSlide)
		}
		b.WriteByte('\n')
	}
	return b.String()
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
