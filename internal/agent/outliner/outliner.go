package outliner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/owulveryck/agentigslide/internal/agent"
	"github.com/owulveryck/agentigslide/internal/vertex"
)

// Agent analyzes the user request and produces a structured presentation
// outline independently of available templates.
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

func (a *Agent) outlinerTool() vertex.Tool {
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
									"enum": ["cover", "section_divider", "content", "data", "conclusion", "diagram"],
									"description": "Type de slide (diagram pour les flowcharts, architectures, schémas)"
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

// Run executes the Outliner agent: sends the user request to Claude and
// parses the structured PresentationOutline from the tool_use response.
func (a *Agent) Run(ctx context.Context, userRequest string, templateInstructions string, agentMemory string) (*agent.PresentationOutline, vertex.Usage, error) {
	slog.Info("[agent:outliner] starting structural analysis", "model", a.model)
	start := time.Now()

	userMsg := fmt.Sprintf("Analyse cette demande de présentation et produis un plan structuré :\n\n%s", userRequest)
	if detectStructuredInput(userRequest) {
		slog.Info("[agent:outliner] structured input detected, switching to preservation mode")
		userMsg = buildStructuredUserMessage(userRequest)
	}

	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{{
			Type: "text",
			Text: userMsg,
		}},
	}}

	tool := a.outlinerTool()
	resp, err := a.client.RawPredictFull(ctx, a.model, messages,
		vertex.WithSystemBlocks(agent.BuildSystemBlocks(systemPrompt, templateInstructions, agentMemory)),
		vertex.WithTools([]vertex.Tool{tool}),
		vertex.WithToolChoice(map[string]any{"type": "tool", "name": "produce_outline"}),
		vertex.WithTemperature(0.2),
		vertex.WithMaxTokens(a.maxTokens),
	)
	if err != nil {
		return nil, vertex.Usage{}, fmt.Errorf("outliner API call failed: %w", err)
	}

	slog.Info("[agent:outliner] API usage",
		"inputTokens", resp.Usage.InputTokens,
		"outputTokens", resp.Usage.OutputTokens,
		"cacheRead", resp.Usage.CacheReadInputTokens,
		"cacheWrite", resp.Usage.CacheCreationInputTokens,
	)

	if resp.StopReason == "max_tokens" {
		return nil, resp.Usage, fmt.Errorf("outliner: response truncated (max_tokens reached), input may be too large")
	}

	block := resp.ToolUseBlock()
	if block == nil {
		return nil, resp.Usage, fmt.Errorf("outliner: no tool_use block in response")
	}

	var outline agent.PresentationOutline
	if err := json.Unmarshal(block.Input, &outline); err != nil {
		slog.Error("[agent:outliner] failed to parse tool_use input",
			"error", err,
			"raw", string(block.Input[:min(len(block.Input), 500)]),
		)
		return nil, resp.Usage, fmt.Errorf("outliner: failed to parse outline: %w", err)
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

	return &outline, resp.Usage, nil
}

// RunInteractive executes the outliner in an interactive loop. It produces
// an initial outline, then repeatedly accepts user feedback to refine it.
// The feedbackFn is called with the current outline; it returns either:
//   - ("", nil) if the user approves and wants to proceed
//   - (feedback, nil) if the user wants changes
//   - ("", err) on input error (e.g. terminal closed)
func (a *Agent) RunInteractive(
	ctx context.Context,
	userRequest string,
	templateInstructions string,
	feedbackFn func(*agent.PresentationOutline) (string, error),
	agentMemory string,
) (*agent.PresentationOutline, []vertex.Usage, error) {
	slog.Info("[agent:outliner] starting interactive mode", "model", a.model)
	start := time.Now()

	userMsg := fmt.Sprintf("Analyse cette demande de présentation et produis un plan structuré :\n\n%s", userRequest)
	if detectStructuredInput(userRequest) {
		slog.Info("[agent:outliner] structured input detected (interactive mode), switching to preservation mode")
		userMsg = buildStructuredUserMessage(userRequest)
	}

	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{{
			Type: "text",
			Text: userMsg,
		}},
	}}

	tool := a.outlinerTool()
	sysBlocks := agent.BuildSystemBlocks(systemPrompt, templateInstructions, agentMemory)
	opts := []vertex.Option{
		vertex.WithSystemBlocks(sysBlocks),
		vertex.WithTools([]vertex.Tool{tool}),
		vertex.WithToolChoice(map[string]any{"type": "tool", "name": "produce_outline"}),
		vertex.WithTemperature(0.2),
		vertex.WithMaxTokens(a.maxTokens),
	}

	var allUsages []vertex.Usage

	for round := 1; ; round++ {
		if ctx.Err() != nil {
			return nil, allUsages, ctx.Err()
		}

		slog.Info("[agent:outliner] interactive round", "round", round)

		resp, err := a.client.RawPredictFull(ctx, a.model, messages, opts...)
		if err != nil {
			return nil, allUsages, fmt.Errorf("outliner API call failed (round %d): %w", round, err)
		}

		allUsages = append(allUsages, resp.Usage)

		slog.Info("[agent:outliner] API usage",
			"round", round,
			"inputTokens", resp.Usage.InputTokens,
			"outputTokens", resp.Usage.OutputTokens,
			"cacheRead", resp.Usage.CacheReadInputTokens,
			"cacheWrite", resp.Usage.CacheCreationInputTokens,
		)

		if resp.StopReason == "max_tokens" {
			return nil, allUsages, fmt.Errorf("outliner: response truncated (round %d)", round)
		}

		block := resp.ToolUseBlock()
		if block == nil {
			return nil, allUsages, fmt.Errorf("outliner: no tool_use block in response (round %d)", round)
		}

		var outline agent.PresentationOutline
		if err := json.Unmarshal(block.Input, &outline); err != nil {
			return nil, allUsages, fmt.Errorf("outliner: failed to parse outline (round %d): %w", round, err)
		}

		logOutlineSummary(&outline, round, time.Since(start))

		feedback, err := feedbackFn(&outline)
		if err != nil {
			return nil, allUsages, fmt.Errorf("outliner: feedback error: %w", err)
		}
		if feedback == "" {
			slog.Info("[agent:outliner] user approved outline", "round", round, "duration", time.Since(start).Round(time.Millisecond))
			return &outline, allUsages, nil
		}

		slog.Info("[agent:outliner] user requested changes", "round", round)

		messages = append(messages,
			vertex.Message{
				Role:    "assistant",
				Content: responseToMessageBlocks(resp),
			},
			vertex.Message{
				Role: "user",
				Content: []vertex.ContentBlock{
					vertex.ToolResultContentBlock(block.ID, "Outline reçu. L'utilisateur demande des modifications."),
					{Type: "text", Text: fmt.Sprintf("Rappel de la demande originale :\n\n%s\n\n---\n\nFeedback de l'utilisateur :\n%s", userRequest, feedback)},
				},
			},
		)
	}
}

func logOutlineSummary(outline *agent.PresentationOutline, round int, elapsed time.Duration) {
	totalSlides := 0
	for _, sec := range outline.Sections {
		totalSlides += len(sec.SlideNeeds)
	}
	slog.Info("[agent:outliner] outline produced",
		"round", round,
		"title", outline.PresentationTitle,
		"sections", len(outline.Sections),
		"totalSlides", totalSlides,
		"elapsed", elapsed.Round(time.Millisecond),
	)
}

func responseToMessageBlocks(resp *vertex.FullResponse) []vertex.ContentBlock {
	var blocks []vertex.ContentBlock
	for _, cb := range resp.Content {
		switch cb.Type {
		case "text":
			blocks = append(blocks, vertex.ContentBlock{Type: "text", Text: cb.Text})
		case "tool_use":
			blocks = append(blocks, vertex.ToolUseContentBlock(cb.ID, cb.Name, cb.Input))
		}
	}
	return blocks
}
