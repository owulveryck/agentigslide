package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/owulveryck/slideAppScripter/internal/model"
	"github.com/owulveryck/slideAppScripter/internal/vertex"
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
// user request and catalog constraints.
func (a *ReviewerAgent) Run(ctx context.Context, plan *model.GenerationPlan, userRequest, compactCatalog string) (*ReviewResult, error) {
	slog.Info("[agent:reviewer] validating assembled plan", "model", a.model, "slides", len(plan.Slides))
	start := time.Now()

	planJSON, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("reviewer: failed to marshal plan: %w", err)
	}

	prompt := fmt.Sprintf(`PLAN DE PRÉSENTATION À VALIDER :
%s

DEMANDE UTILISATEUR ORIGINALE :
"""
%s
"""

CATALOGUE DES SLIDES TEMPLATE (pour vérifier les capacités) :
%s

Vérifie ce plan selon les critères de qualité et soumets ta revue.`, string(planJSON), userRequest, compactCatalog)

	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{{
			Type: "text",
			Text: prompt,
		}},
	}}

	tool := a.reviewerTool()
	resp, err := a.client.RawPredictFull(ctx, a.model, messages,
		vertex.WithSystem(reviewerSystemPrompt),
		vertex.WithTools([]vertex.Tool{tool}),
		vertex.WithToolChoice(map[string]any{"type": "tool", "name": "submit_review"}),
		vertex.WithTemperature(0.0),
		vertex.WithMaxTokens(8192),
	)
	if err != nil {
		return nil, fmt.Errorf("reviewer API call failed: %w", err)
	}

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
