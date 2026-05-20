package editwriter

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

// Agent generates new text content for modify_content operations based on
// modification intentions from the EditPlanner.
type Agent struct {
	client *vertex.Client
	model  string
}

// New creates an Agent with the given Vertex AI client and model name.
func New(client *vertex.Client, model string) *Agent {
	return &Agent{client: client, model: model}
}

func buildEditWriterTool(modifications []model.ModificationIntent) vertex.Tool {
	properties := make(map[string]any, len(modifications))
	required := make([]string, 0, len(modifications))

	for _, mod := range modifications {
		properties[mod.VariableName] = map[string]any{
			"type":        "string",
			"description": fmt.Sprintf("Nouveau texte pour l'élément %s. Intention : %s", mod.VariableName, mod.Intention),
		}
		required = append(required, mod.VariableName)
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
	schemaJSON, _ := json.Marshal(schema)

	return vertex.Tool{
		Name:        "produce_modifications",
		Description: "Produit le nouveau texte pour chaque élément à modifier.",
		InputSchema: schemaJSON,
	}
}

// WriteBatch generates new text for multiple modifications on a single slide.
// Batching per slide ensures coherence between related modifications.
func (a *Agent) WriteBatch(ctx context.Context, modifications []model.ModificationIntent, templateInstructions string) ([]model.TextModification, vertex.Usage, error) {
	slog.Info("[agent:editwriter] starting",
		"model", a.model,
		"modifications", len(modifications),
	)
	start := time.Now()

	var prompt strings.Builder
	prompt.WriteString("ÉLÉMENTS À MODIFIER :\n\n")
	for i, mod := range modifications {
		fmt.Fprintf(&prompt, "[%d] ObjectID: %s\n", i+1, mod.VariableName)
		fmt.Fprintf(&prompt, "    Texte actuel : %q\n", mod.CurrentText)
		fmt.Fprintf(&prompt, "    Intention : %s\n\n", mod.Intention)
	}
	prompt.WriteString("Produis le nouveau texte pour chaque élément en respectant les intentions.")

	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{{
			Type: "text",
			Text: prompt.String(),
		}},
	}}

	tool := buildEditWriterTool(modifications)
	resp, err := a.client.RawPredictFull(ctx, a.model, messages,
		vertex.WithSystemBlocks(agent.BuildSystemBlocks(systemPrompt, templateInstructions)),
		vertex.WithTools([]vertex.Tool{tool}),
		vertex.WithToolChoice(map[string]any{"type": "tool", "name": "produce_modifications"}),
		vertex.WithTemperature(0.2),
		vertex.WithMaxTokens(4096),
	)
	if err != nil {
		return nil, vertex.Usage{}, fmt.Errorf("editwriter API call failed: %w", err)
	}

	slog.Info("[agent:editwriter] API usage",
		"inputTokens", resp.Usage.InputTokens,
		"outputTokens", resp.Usage.OutputTokens,
		"cacheRead", resp.Usage.CacheReadInputTokens,
		"cacheWrite", resp.Usage.CacheCreationInputTokens,
	)

	if resp.StopReason == "max_tokens" {
		return nil, resp.Usage, fmt.Errorf("editwriter: response truncated (max_tokens reached)")
	}

	block := resp.ToolUseBlock()
	if block == nil {
		return nil, resp.Usage, fmt.Errorf("editwriter: no tool_use block in response")
	}

	var fieldValues map[string]string
	if err := json.Unmarshal(block.Input, &fieldValues); err != nil {
		return nil, resp.Usage, fmt.Errorf("editwriter: failed to parse response: %w", err)
	}

	var result []model.TextModification
	for varName, text := range fieldValues {
		result = append(result, model.TextModification{VariableName: varName, NewText: text})
	}

	slog.Info("[agent:editwriter] done",
		"modifications", len(result),
		"duration", time.Since(start).Round(time.Millisecond),
	)

	return result, resp.Usage, nil
}
