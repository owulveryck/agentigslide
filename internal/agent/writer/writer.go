package writer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/owulveryck/agentigslide/internal/agent"
	"github.com/owulveryck/agentigslide/internal/model"
	"github.com/owulveryck/agentigslide/internal/vertex"
)

// Agent generates the text content for a single slide.
type Agent struct {
	client *vertex.Client
	model  string
}

// New creates an Agent with the given Vertex AI client and model name.
func New(client *vertex.Client, model string) *Agent {
	return &Agent{client: client, model: model}
}

// BuildWriterTool constructs the tool schema for a slide's editable fields.
// Exported for use by the orchestrator when building tools dynamically.
func BuildWriterTool(fields []agent.TemplateField) vertex.Tool {
	properties := make(map[string]any, len(fields))
	required := make([]string, 0, len(fields))

	for _, f := range fields {
		prop := map[string]any{
			"type":        "string",
			"description": fmt.Sprintf("Contenu pour le champ %s (%s, markdown autorisé: **gras**, *italique*, `code`, ligne vide pour saut de paragraphe)", f.VariableName, f.Role),
		}
		if f.MaxChars > 0 {
			prop["maxLength"] = f.MaxChars * 9 / 10
		}
		properties[f.VariableName] = prop
		required = append(required, f.VariableName)
	}

	sort.Strings(required)

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
	schemaJSON, _ := json.Marshal(schema)

	return vertex.Tool{
		Name:        "produce_slide_content",
		Description: "Produit le contenu textuel final pour chaque champ du slide.",
		InputSchema: schemaJSON,
	}
}

// WriteSlide generates text modifications for a single slide based on the
// template fields and content items from the outline. The writer maps
// content items to the template's actual fields. Optional feedback from a
// previous review pass is injected into the prompt so the Writer can
// correct its output.
func (a *Agent) WriteSlide(ctx context.Context, sourceSlide int, slideNeed agent.SlideNeed, templateFields []agent.TemplateField, templateInstructions string, feedback ...agent.ReviewIssue) (*agent.SlideContent, vertex.Usage, error) {
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

	tool := BuildWriterTool(templateFields)
	resp, err := a.client.RawPredictFull(ctx, a.model, messages,
		vertex.WithSystemBlocks(agent.BuildSystemBlocks(systemPrompt, templateInstructions)),
		vertex.WithTools([]vertex.Tool{tool}),
		vertex.WithToolChoice(map[string]any{"type": "tool", "name": "produce_slide_content"}),
		vertex.WithTemperature(0.2),
		vertex.WithMaxTokens(4096),
	)
	if err != nil {
		return nil, vertex.Usage{}, fmt.Errorf("writer API call failed for slide %d: %w", sourceSlide, err)
	}

	slog.Info("[agent:writer] API usage",
		"sourceSlide", sourceSlide,
		"inputTokens", resp.Usage.InputTokens,
		"outputTokens", resp.Usage.OutputTokens,
		"cacheRead", resp.Usage.CacheReadInputTokens,
		"cacheWrite", resp.Usage.CacheCreationInputTokens,
	)

	if resp.StopReason == "max_tokens" {
		return nil, resp.Usage, fmt.Errorf("writer: response truncated for slide %d (max_tokens reached)", sourceSlide)
	}

	block := resp.ToolUseBlock()
	if block == nil {
		return nil, resp.Usage, fmt.Errorf("writer: no tool_use block in response for slide %d", sourceSlide)
	}

	var fieldValues map[string]string
	if err := json.Unmarshal(block.Input, &fieldValues); err != nil {
		return nil, resp.Usage, fmt.Errorf("writer: failed to parse content for slide %d: %w", sourceSlide, err)
	}

	var mods []model.TextModification
	for varName, text := range fieldValues {
		mods = append(mods, model.TextModification{VariableName: varName, NewText: text})
	}

	slog.Info("[agent:writer] done",
		"sourceSlide", sourceSlide,
		"fields", len(mods),
		"duration", time.Since(start).Round(time.Millisecond),
	)

	return &agent.SlideContent{
		SourceSlide:   sourceSlide,
		Modifications: mods,
	}, resp.Usage, nil
}
