package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/owulveryck/agentigslide/internal/model"
	"github.com/owulveryck/agentigslide/internal/vertex"
)

const reviewerSystemPrompt = `Tu es un relecteur qualité pour des présentations professionnelles.
Ton rôle est de vérifier un plan de présentation assemblé et de détecter les problèmes avant sa mise en production.

CRITÈRES DE VALIDATION :
1. OVERFLOW : Aucun texte ne doit dépasser la capacité max du champ (si connue).
2. DUPLICATION : Aucun texte identique ou quasi-identique ne doit apparaître dans deux champs différents de la présentation.
3. CONTENU MANQUANT : Chaque section de la demande utilisateur doit être couverte par au moins un slide.
4. TEMPLATE INADAPTÉ : Le nombre de champs remplis doit correspondre au nombre de zones du template.
5. COHÉRENCE : Les intercalaires de section doivent précéder les slides de contenu qu'ils introduisent.
6. PAS D'INVENTION : Le contenu doit provenir exclusivement de la demande utilisateur.

Si aucun problème n'est détecté, approuve le plan.
Si des problèmes sont détectés, liste-les avec des suggestions de correction.`

// ReviewerAgent validates the assembled GenerationPlan against quality rules.
type ReviewerAgent struct {
	client *vertex.Client
	model  string
}

// NewReviewerAgent creates a ReviewerAgent.
func NewReviewerAgent(client *vertex.Client, model string) *ReviewerAgent {
	return &ReviewerAgent{client: client, model: model}
}

func (a *ReviewerAgent) reviewerTool() vertex.Tool {
	return vertex.Tool{
		Name:        "submit_review",
		Description: "Soumet le résultat de la revue qualité du plan de présentation.",
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
					"slideIndex": {
						"type": "integer",
						"description": "Index (0-based) du slide concerné dans le plan"
					},
					"field": {
						"type": "string",
						"description": "Nom du champ concerné (variableName), vide si le problème concerne le slide entier"
					},
					"issueType": {
						"type": "string",
						"enum": ["overflow", "duplicate", "missing_content", "wrong_template", "incoherence", "invented_content"],
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
				"required": ["slideIndex", "issueType", "description", "suggestion"]
			}
		}
	},
	"required": ["approved", "issues"]
}`),
	}
}

// Run executes the Reviewer agent: validates the assembled plan against the
// user request and catalog constraints. If thinkingBudget > 0, extended
// thinking is enabled for deeper reasoning (forces temperature to 1.0).
func (a *ReviewerAgent) Run(ctx context.Context, plan *model.GenerationPlan, userRequest, compactCatalog, templateInstructions string, thinkingBudget int) (*ReviewResult, error) {
	slog.Info("[agent:reviewer] validating assembled plan", "model", a.model, "slides", len(plan.Slides))
	start := time.Now()

	planJSON, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("reviewer: failed to marshal plan: %w", err)
	}

	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{
			{
				Type:         "text",
				Text:         "CATALOGUE DES SLIDES TEMPLATE (pour vérifier les capacités) :\n" + compactCatalog,
				CacheControl: &vertex.CacheControl{Type: "ephemeral"},
			},
			{
				Type:         "text",
				Text:         "DEMANDE UTILISATEUR ORIGINALE :\n\"\"\"\n" + userRequest + "\n\"\"\"",
				CacheControl: &vertex.CacheControl{Type: "ephemeral"},
			},
			{
				Type: "text",
				Text: fmt.Sprintf("PLAN DE PRÉSENTATION À VALIDER :\n%s\n\nVérifie ce plan selon les critères de qualité et soumets ta revue.", string(planJSON)),
			},
		},
	}}

	tool := a.reviewerTool()
	opts := []vertex.Option{
		vertex.WithSystemBlocks(buildSystemBlocks(reviewerSystemPrompt, templateInstructions)),
		vertex.WithTools([]vertex.Tool{tool}),
		vertex.WithMaxTokens(16384),
	}
	if thinkingBudget > 0 {
		opts = append(opts, vertex.WithThinking(thinkingBudget))
		opts = append(opts, vertex.WithToolChoice(map[string]any{"type": "auto"}))
	} else {
		opts = append(opts, vertex.WithTemperature(0.0))
		opts = append(opts, vertex.WithToolChoice(map[string]any{"type": "tool", "name": "submit_review"}))
	}

	resp, err := a.client.RawPredictFull(ctx, a.model, messages, opts...)
	if err != nil {
		return nil, fmt.Errorf("reviewer API call failed: %w", err)
	}

	slog.Info("[agent:reviewer] API usage",
		"inputTokens", resp.Usage.InputTokens,
		"outputTokens", resp.Usage.OutputTokens,
		"cacheRead", resp.Usage.CacheReadInputTokens,
		"cacheWrite", resp.Usage.CacheCreationInputTokens,
	)

	if resp.StopReason == "max_tokens" {
		return nil, fmt.Errorf("reviewer: response truncated (max_tokens reached)")
	}

	block := resp.ToolUseBlock()
	if block == nil {
		return nil, fmt.Errorf("reviewer: no tool_use block in response")
	}

	var result ReviewResult
	if err := json.Unmarshal(block.Input, &result); err != nil {
		return nil, fmt.Errorf("reviewer: failed to parse review: %w", err)
	}

	if result.Approved {
		slog.Info("[agent:reviewer] plan approved",
			"duration", time.Since(start).Round(time.Millisecond),
		)
	} else {
		for _, issue := range result.Issues {
			slog.Warn("[agent:reviewer]   issue",
				"slide", issue.SlideIndex,
				"field", issue.Field,
				"type", issue.IssueType,
				"description", issue.Description,
			)
		}
		slog.Info("[agent:reviewer] issues found",
			"count", len(result.Issues),
			"duration", time.Since(start).Round(time.Millisecond),
		)
	}

	return &result, nil
}

// RunSubset validates only specific slides that were corrected after a previous
// review pass. This avoids re-processing the entire plan (~114K tokens) and
// focuses the reviewer on verifying that the corrections addressed the issues.
func (a *ReviewerAgent) RunSubset(ctx context.Context, plan *model.GenerationPlan, correctedIndices []int, previousIssues []ReviewIssue, userRequest, compactCatalog, templateInstructions string, thinkingBudget int) (*ReviewResult, error) {
	slog.Info("[agent:reviewer] validating corrected slides only", "model", a.model, "correctedSlides", len(correctedIndices))
	start := time.Now()

	type indexedSlide struct {
		Index int                `json:"index"`
		Slide model.SlideRequest `json:"slide"`
	}

	subset := make([]indexedSlide, 0, len(correctedIndices))
	for _, idx := range correctedIndices {
		if idx >= 0 && idx < len(plan.Slides) {
			subset = append(subset, indexedSlide{Index: idx, Slide: plan.Slides[idx]})
		}
	}

	subsetJSON, err := json.MarshalIndent(subset, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("reviewer: failed to marshal slide subset: %w", err)
	}

	var issueLines []string
	for _, issue := range previousIssues {
		line := fmt.Sprintf("- Slide %d, champ %q [%s]: %s", issue.SlideIndex, issue.Field, issue.IssueType, issue.Description)
		issueLines = append(issueLines, line)
	}

	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{
			{
				Type:         "text",
				Text:         "CATALOGUE DES SLIDES TEMPLATE (pour vérifier les capacités) :\n" + compactCatalog,
				CacheControl: &vertex.CacheControl{Type: "ephemeral"},
			},
			{
				Type:         "text",
				Text:         "DEMANDE UTILISATEUR ORIGINALE :\n\"\"\"\n" + userRequest + "\n\"\"\"",
				CacheControl: &vertex.CacheControl{Type: "ephemeral"},
			},
			{
				Type: "text",
				Text: fmt.Sprintf("SLIDES CORRIGÉES À VÉRIFIER :\nLes slides suivantes ont été corrigées suite à ta revue précédente. Vérifie que les corrections sont satisfaisantes.\n\nISSUES PRÉCÉDENTES :\n%s\n\nSLIDES CORRIGÉES (avec leur index dans le plan) :\n%s\n\nVérifie UNIQUEMENT ces slides corrigées. Si toutes les corrections sont satisfaisantes, approuve. Sinon, signale les problèmes restants.",
					joinLines(issueLines), string(subsetJSON)),
			},
		},
	}}

	tool := a.reviewerTool()
	opts := []vertex.Option{
		vertex.WithSystemBlocks(buildSystemBlocks(reviewerSystemPrompt, templateInstructions)),
		vertex.WithTools([]vertex.Tool{tool}),
		vertex.WithMaxTokens(8192),
	}
	if thinkingBudget > 0 {
		opts = append(opts, vertex.WithThinking(thinkingBudget))
		opts = append(opts, vertex.WithToolChoice(map[string]any{"type": "auto"}))
	} else {
		opts = append(opts, vertex.WithTemperature(0.0))
		opts = append(opts, vertex.WithToolChoice(map[string]any{"type": "tool", "name": "submit_review"}))
	}

	resp, err := a.client.RawPredictFull(ctx, a.model, messages, opts...)
	if err != nil {
		return nil, fmt.Errorf("reviewer subset API call failed: %w", err)
	}

	slog.Info("[agent:reviewer] API usage (subset)",
		"inputTokens", resp.Usage.InputTokens,
		"outputTokens", resp.Usage.OutputTokens,
		"cacheRead", resp.Usage.CacheReadInputTokens,
		"cacheWrite", resp.Usage.CacheCreationInputTokens,
	)

	if resp.StopReason == "max_tokens" {
		return nil, fmt.Errorf("reviewer: response truncated (max_tokens reached)")
	}

	block := resp.ToolUseBlock()
	if block == nil {
		return nil, fmt.Errorf("reviewer: no tool_use block in response")
	}

	var result ReviewResult
	if err := json.Unmarshal(block.Input, &result); err != nil {
		return nil, fmt.Errorf("reviewer: failed to parse review: %w", err)
	}

	if result.Approved {
		slog.Info("[agent:reviewer] corrections approved",
			"duration", time.Since(start).Round(time.Millisecond),
		)
	} else {
		for _, issue := range result.Issues {
			slog.Warn("[agent:reviewer]   issue (subset)",
				"slide", issue.SlideIndex,
				"field", issue.Field,
				"type", issue.IssueType,
				"description", issue.Description,
			)
		}
		slog.Info("[agent:reviewer] issues remaining",
			"count", len(result.Issues),
			"duration", time.Since(start).Round(time.Millisecond),
		)
	}

	return &result, nil
}

func joinLines(lines []string) string {
	result := ""
	for i, l := range lines {
		if i > 0 {
			result += "\n"
		}
		result += l
	}
	return result
}
