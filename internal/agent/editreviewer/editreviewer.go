package editreviewer

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

// Agent validates an EditPlan against the original intentions and existing
// presentation content.
type Agent struct {
	client *vertex.Client
	model  string
}

// New creates an Agent with the given Vertex AI client and model name.
func New(client *vertex.Client, model string) *Agent {
	return &Agent{client: client, model: model}
}

func (a *Agent) reviewerTool() vertex.Tool {
	return vertex.Tool{
		Name:        "submit_edit_review",
		Description: "Soumet le résultat de la revue qualité du plan d'édition.",
		InputSchema: json.RawMessage(`{
	"type": "object",
	"properties": {
		"approved": {
			"type": "boolean",
			"description": "true si le plan est validé, false si des problèmes sont détectés"
		},
		"issues": {
			"type": "array",
			"items": {
				"type": "object",
				"properties": {
					"operationIndex": {
						"type": "integer",
						"description": "Index (0-based) de l'opération concernée dans le plan"
					},
					"field": {
						"type": "string",
						"description": "Nom du champ concerné (variableName), vide si le problème concerne l'opération entière"
					},
					"issueType": {
						"type": "string",
						"enum": ["intention_mismatch", "coherence_break", "over_modification", "quality_issue", "missing_content"],
						"description": "Type de problème"
					},
					"description": {
						"type": "string",
						"description": "Description du problème"
					},
					"suggestion": {
						"type": "string",
						"description": "Suggestion de correction"
					}
				},
				"required": ["operationIndex", "issueType", "description", "suggestion"]
			}
		}
	},
	"required": ["approved", "issues"]
}`),
	}
}

// Run validates the filled EditPlan against the skeleton intentions and
// existing presentation content. If thinkingBudget > 0, extended thinking
// is enabled.
func (a *Agent) Run(
	ctx context.Context,
	plan *model.EditPlan,
	skeleton *model.EditSkeleton,
	existingSlides []model.ExistingSlideInfo,
	userRequest string,
	templateInstructions string,
	thinkingBudget int,
) (*agent.ReviewResult, vertex.Usage, error) {
	slog.Info("[agent:editreviewer] validating edit plan", "model", a.model, "operations", len(plan.Operations))
	start := time.Now()

	var prompt strings.Builder
	prompt.WriteString("DEMANDE UTILISATEUR :\n")
	prompt.WriteString(userRequest)
	prompt.WriteString("\n\n---\n\nPRÉSENTATION EXISTANTE :\n")
	for _, slide := range existingSlides {
		fmt.Fprintf(&prompt, "\nSlide %d (pageId: %s)\n", slide.Index, slide.PageObjectID)
		for _, el := range slide.TextElements {
			fmt.Fprintf(&prompt, "  - [%s] %s : %q\n", el.ShapeType, el.ObjectID, el.Content)
		}
	}

	prompt.WriteString("\n---\n\nINTENTIONS DU PLAN (squelette) :\n")
	for i, op := range skeleton.Operations {
		fmt.Fprintf(&prompt, "\n[%d] %s slide %d — %s\n", i, op.Type, op.SlideIndex, op.Rationale)
		for _, mod := range op.Modifications {
			fmt.Fprintf(&prompt, "  → %s : %s\n", mod.VariableName, mod.Intention)
		}
		for _, ci := range op.ContentIntents {
			fmt.Fprintf(&prompt, "  → %s : %s\n", ci.VariableName, ci.Intention)
		}
	}

	prompt.WriteString("\n---\n\nPLAN D'ÉDITION AVEC TEXTE GÉNÉRÉ :\n")
	for i, op := range plan.Operations {
		fmt.Fprintf(&prompt, "\n[%d] %s slide %d\n", i, op.Type, op.SlideIndex)
		for _, mod := range op.Modifications {
			fmt.Fprintf(&prompt, "  → %s : %q\n", mod.VariableName, mod.NewText)
		}
		for _, sc := range op.SlideContent {
			fmt.Fprintf(&prompt, "  → %s : %q\n", sc.VariableName, sc.NewText)
		}
	}

	prompt.WriteString("\n\nValide ce plan d'édition selon les critères de qualité.")

	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{{
			Type: "text",
			Text: prompt.String(),
		}},
	}}

	tool := a.reviewerTool()
	opts := []vertex.Option{
		vertex.WithSystemBlocks(agent.BuildSystemBlocks(systemPrompt, templateInstructions)),
		vertex.WithTools([]vertex.Tool{tool}),
		vertex.WithToolChoice(map[string]any{"type": "tool", "name": "submit_edit_review"}),
		vertex.WithMaxTokens(8192),
	}
	if thinkingBudget > 0 {
		opts = append(opts, vertex.WithThinking(thinkingBudget))
	} else {
		opts = append(opts, vertex.WithTemperature(0.2))
	}

	resp, err := a.client.RawPredictFull(ctx, a.model, messages, opts...)
	if err != nil {
		return nil, vertex.Usage{}, fmt.Errorf("editreviewer API call failed: %w", err)
	}

	slog.Info("[agent:editreviewer] API usage",
		"inputTokens", resp.Usage.InputTokens,
		"outputTokens", resp.Usage.OutputTokens,
		"cacheRead", resp.Usage.CacheReadInputTokens,
		"cacheWrite", resp.Usage.CacheCreationInputTokens,
	)

	if resp.StopReason == "max_tokens" {
		return nil, resp.Usage, fmt.Errorf("editreviewer: response truncated")
	}

	block := resp.ToolUseBlock()
	if block == nil {
		return nil, resp.Usage, fmt.Errorf("editreviewer: no tool_use block in response")
	}

	var result struct {
		Approved bool `json:"approved"`
		Issues   []struct {
			OperationIndex int    `json:"operationIndex"`
			Field          string `json:"field"`
			IssueType      string `json:"issueType"`
			Description    string `json:"description"`
			Suggestion     string `json:"suggestion"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(block.Input, &result); err != nil {
		return nil, resp.Usage, fmt.Errorf("editreviewer: failed to parse response: %w", err)
	}

	review := &agent.ReviewResult{
		Approved: result.Approved,
	}
	for _, issue := range result.Issues {
		review.Issues = append(review.Issues, agent.ReviewIssue{
			SlideIndex:  issue.OperationIndex,
			Field:       issue.Field,
			IssueType:   issue.IssueType,
			Description: issue.Description,
			Suggestion:  issue.Suggestion,
		})
	}

	slog.Info("[agent:editreviewer] done",
		"approved", review.Approved,
		"issues", len(review.Issues),
		"duration", time.Since(start).Round(time.Millisecond),
	)

	return review, resp.Usage, nil
}
