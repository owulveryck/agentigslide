package selector

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/owulveryck/agentigslide/internal/agent"
	"github.com/owulveryck/agentigslide/internal/vertex"
)

// Agent maps each SlideNeed from the outline to the best template slide
// from the catalog.
type Agent struct {
	client *vertex.Client
	model  string
}

// New creates an Agent with the given Vertex AI client and model name.
func New(client *vertex.Client, model string) *Agent {
	return &Agent{client: client, model: model}
}

func (a *Agent) selectorTool() vertex.Tool {
	return vertex.Tool{
		Name:        "select_templates",
		Description: "Sélectionne les templates les plus adaptés pour chaque besoin de slide.",
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
					}
				},
				"required": ["outlineIndex", "sourceSlide", "rationale"]
			}
		}
	},
	"required": ["selections"]
}`),
	}
}

// Run executes the Selector agent: sends the outline and catalog to Claude
// and returns the template selection plan.
func (a *Agent) Run(ctx context.Context, outline *agent.PresentationOutline, compactCatalog string, templateInstructions string, previousErrors ...string) (*agent.SelectionPlan, vertex.Usage, error) {
	slog.Info("[agent:selector] mapping outline to templates", "model", a.model)
	start := time.Now()

	outlineJSON, err := json.MarshalIndent(outline, "", "  ")
	if err != nil {
		return nil, vertex.Usage{}, fmt.Errorf("selector: failed to marshal outline: %w", err)
	}

	outlinePrompt := fmt.Sprintf("PLAN STRUCTURÉ DE LA PRÉSENTATION :\n%s\n\nPour chaque SlideNeed du plan, sélectionne le template le plus adapté du catalogue et mappe les champs.\nL'outlineIndex est l'index global du SlideNeed en parcourant toutes les sections dans l'ordre (0-based).", string(outlineJSON))

	if len(previousErrors) > 0 && previousErrors[0] != "" {
		slog.Info("[agent:selector] retrying with validation feedback", "model", a.model)
		outlinePrompt += fmt.Sprintf("\n\nERREURS DE VALIDATION DE LA TENTATIVE PRÉCÉDENTE :\n%s\n\nCORRIGE ces erreurs en choisissant des templates qui existent dans le catalogue.\nVérifie que chaque sourceSlide correspond bien à un numéro de SLIDE listé dans le catalogue.", previousErrors[0])
	}

	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{
			{
				Type:         "text",
				Text:         "CATALOGUE DES SLIDES TEMPLATE DISPONIBLES :\n" + compactCatalog,
				CacheControl: &vertex.CacheControl{Type: "ephemeral"},
			},
			{
				Type: "text",
				Text: outlinePrompt,
			},
		},
	}}

	tool := a.selectorTool()
	resp, err := a.client.RawPredictFull(ctx, a.model, messages,
		vertex.WithSystemBlocks(agent.BuildSystemBlocks(systemPrompt, templateInstructions)),
		vertex.WithTools([]vertex.Tool{tool}),
		vertex.WithToolChoice(map[string]any{"type": "tool", "name": "select_templates"}),
		vertex.WithTemperature(0.1),
		vertex.WithMaxTokens(16384),
	)
	if err != nil {
		return nil, vertex.Usage{}, fmt.Errorf("selector API call failed: %w", err)
	}

	slog.Info("[agent:selector] API usage",
		"inputTokens", resp.Usage.InputTokens,
		"outputTokens", resp.Usage.OutputTokens,
		"cacheRead", resp.Usage.CacheReadInputTokens,
		"cacheWrite", resp.Usage.CacheCreationInputTokens,
	)

	if resp.StopReason == "max_tokens" {
		return nil, resp.Usage, fmt.Errorf("selector: response truncated (max_tokens reached)")
	}

	block := resp.ToolUseBlock()
	if block == nil {
		return nil, resp.Usage, fmt.Errorf("selector: no tool_use block in response")
	}

	var selPlan agent.SelectionPlan
	if err := json.Unmarshal(block.Input, &selPlan); err != nil {
		return nil, resp.Usage, fmt.Errorf("selector: failed to parse selection plan: %w", err)
	}

	for i, sel := range selPlan.Selections {
		slog.Info("[agent:selector]   slide mapped",
			"position", i+1,
			"sourceSlide", sel.SourceSlide,
			"rationale", sel.Rationale,
		)
	}

	slog.Info("[agent:selector] done",
		"selections", len(selPlan.Selections),
		"duration", time.Since(start).Round(time.Millisecond),
	)

	return &selPlan, resp.Usage, nil
}
