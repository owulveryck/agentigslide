package selector

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
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

// selectorTool builds the select_templates tool. When the catalog is known,
// sourceSlide is constrained to an enum of existing slide numbers (plus -1
// for diagrams) so an out-of-catalog selection is impossible at the API
// level instead of being caught later by validation.
func (a *Agent) selectorTool(catalog *agent.CatalogInfo) vertex.Tool {
	sourceSlideSchema := `"type": "integer",
					"description": "Numéro du slide template choisi dans le catalogue (-1 pour les slides diagram qui n'ont pas besoin de template)"`
	if catalog != nil && len(catalog.SlideNumbers) > 0 {
		nums := make([]int, 0, len(catalog.SlideNumbers)+1)
		nums = append(nums, -1)
		for n := range catalog.SlideNumbers {
			nums = append(nums, n)
		}
		sort.Ints(nums)
		enumJSON, err := json.Marshal(nums)
		if err == nil {
			sourceSlideSchema = fmt.Sprintf(`"type": "integer",
					"enum": %s,
					"description": "Numéro du slide template choisi dans le catalogue (-1 pour les slides diagram qui n'ont pas besoin de template)"`, enumJSON)
		}
	}
	return vertex.Tool{
		Name:        "select_templates",
		Description: "Sélectionne les templates les plus adaptés pour chaque besoin de slide.",
		InputSchema: json.RawMessage(fmt.Sprintf(`{
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
						%s
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
}`, sourceSlideSchema)),
	}
}

// Run executes the Selector agent: sends the outline and catalog to Claude
// and returns the template selection plan.
func (a *Agent) Run(ctx context.Context, outline *agent.PresentationOutline, compactCatalog string, templateInstructions string, agentMemory string, previousErrors ...string) (*agent.SelectionPlan, vertex.Usage, error) {
	slog.Info("[agent:selector] mapping outline to templates", "model", a.model)
	start := time.Now()

	outlineJSON, err := json.MarshalIndent(outline, "", "  ")
	if err != nil {
		return nil, vertex.Usage{}, fmt.Errorf("selector: failed to marshal outline: %w", err)
	}

	flatNeeds := agent.FlattenNeeds(outline)
	totalNeeds := len(flatNeeds)

	catalogInfo := agent.ParseCatalog(compactCatalog)

	var indexListing strings.Builder
	idx := 0
	for _, sec := range outline.Sections {
		for _, need := range sec.SlideNeeds {
			constraints := agent.NeedConstraintsWithCatalog(need, &catalogInfo)
			if constraints != "" {
				fmt.Fprintf(&indexListing, "  outlineIndex=%d : slideType=%s, intent=%q | CONTRAINTES: %s\n", idx, need.SlideType, need.Intent, constraints)
			} else {
				fmt.Fprintf(&indexListing, "  outlineIndex=%d : slideType=%s, intent=%q\n", idx, need.SlideType, need.Intent)
			}
			idx++
		}
	}

	outlinePrompt := fmt.Sprintf(
		"PLAN STRUCTURÉ DE LA PRÉSENTATION :\n%s\n\n"+
			"NOMBRE TOTAL DE SLIDE NEEDS : %d\n"+
			"INDEX GLOBAL DE CHAQUE SLIDE NEED (0-based) :\n%s\n"+
			"Tu DOIS produire EXACTEMENT %d sélections, une par SlideNeed, avec les outlineIndex de 0 à %d.\n"+
			"Ne crée PAS de slides supplémentaires (pas d'intercalaires ni de section_dividers que le plan ne contient pas).\n"+
			"Pour chaque SlideNeed du plan, sélectionne le template le plus adapté du catalogue.\n"+
			"L'outlineIndex est l'index global du SlideNeed en parcourant toutes les sections dans l'ordre (0-based).",
		string(outlineJSON), totalNeeds, indexListing.String(), totalNeeds, totalNeeds-1,
	)

	if len(previousErrors) > 0 && previousErrors[0] != "" {
		slog.Info("[agent:selector] retrying with validation feedback", "model", a.model)
		outlinePrompt += fmt.Sprintf("\n\nERREURS DE VALIDATION DE LA TENTATIVE PRÉCÉDENTE :\n%s\n\nCORRIGE ces erreurs en choisissant des templates qui existent dans le catalogue.\nVérifie que chaque sourceSlide correspond bien à un numéro de SLIDE listé dans le catalogue.", previousErrors[0])
	}

	catalogWithSummary := agent.CapacitySummary(compactCatalog) + "\n" + compactCatalog

	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{
			{
				Type:         "text",
				Text:         "CATALOGUE DES SLIDES TEMPLATE DISPONIBLES :\n" + catalogWithSummary,
				CacheControl: &vertex.CacheControl{Type: "ephemeral"},
			},
			{
				Type: "text",
				Text: outlinePrompt,
			},
		},
	}}

	tool := a.selectorTool(&catalogInfo)
	resp, err := a.client.RawPredictFull(ctx, a.model, messages,
		vertex.WithSystemBlocks(agent.BuildSystemBlocks(systemPrompt, templateInstructions, agentMemory)),
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

// RunPartial re-asks the selector only for the selections that failed
// validation, instead of regenerating the whole plan. The corrected entries
// are merged into current in place. Each failed entry is presented with its
// validation reason and the eligible slide numbers computed deterministically
// from the catalog.
func (a *Agent) RunPartial(ctx context.Context, outline *agent.PresentationOutline, compactCatalog string, templateInstructions string, agentMemory string, current *agent.SelectionPlan, issues []agent.SelectionIssue) (vertex.Usage, error) {
	slog.Info("[agent:selector] partial retry on failed selections", "model", a.model, "failed", len(issues))
	start := time.Now()

	catalogInfo := agent.ParseCatalog(compactCatalog)
	needs := agent.FlattenNeeds(outline)

	var failedListing strings.Builder
	for _, issue := range issues {
		if issue.OutlineIndex < 0 || issue.OutlineIndex >= len(needs) {
			continue
		}
		need := needs[issue.OutlineIndex]
		eligible := agent.EligibleSlidesForNeed(need, &catalogInfo)
		fmt.Fprintf(&failedListing, "  outlineIndex=%d : slideType=%s, intent=%q\n    ERREUR : %s\n",
			issue.OutlineIndex, need.SlideType, need.Intent, issue.Reason)
		if constraints := agent.NeedConstraintsWithCatalog(need, &catalogInfo); constraints != "" {
			fmt.Fprintf(&failedListing, "    CONTRAINTES : %s\n", constraints)
		} else if len(eligible) > 0 {
			fmt.Fprintf(&failedListing, "    SLIDES ÉLIGIBLES : %v\n", eligible)
		}
	}

	var currentListing strings.Builder
	for _, sel := range current.Selections {
		fmt.Fprintf(&currentListing, "  outlineIndex=%d → sourceSlide %d\n", sel.OutlineIndex, sel.SourceSlide)
	}

	prompt := fmt.Sprintf(
		"Le plan de sélection ci-dessous est presque valide. Seules %d sélections ont échoué à la validation.\n\n"+
			"SÉLECTIONS ACTUELLES (pour contexte, NE PAS les re-produire) :\n%s\n"+
			"SÉLECTIONS À CORRIGER (produis EXACTEMENT %d sélections, une par outlineIndex listé ci-dessous) :\n%s\n"+
			"Choisis pour chaque outlineIndex un template qui respecte ses contraintes. "+
			"Ne renvoie QUE les sélections corrigées.",
		len(issues), currentListing.String(), len(issues), failedListing.String(),
	)

	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{
			{
				Type:         "text",
				Text:         "CATALOGUE DES SLIDES TEMPLATE DISPONIBLES :\n" + agent.CapacitySummary(compactCatalog) + "\n" + compactCatalog,
				CacheControl: &vertex.CacheControl{Type: "ephemeral"},
			},
			{
				Type: "text",
				Text: prompt,
			},
		},
	}}

	tool := a.selectorTool(&catalogInfo)
	resp, err := a.client.RawPredictFull(ctx, a.model, messages,
		vertex.WithSystemBlocks(agent.BuildSystemBlocks(systemPrompt, templateInstructions, agentMemory)),
		vertex.WithTools([]vertex.Tool{tool}),
		vertex.WithToolChoice(map[string]any{"type": "tool", "name": "select_templates"}),
		vertex.WithTemperature(0.1),
		vertex.WithMaxTokens(4096),
	)
	if err != nil {
		return vertex.Usage{}, fmt.Errorf("selector partial retry API call failed: %w", err)
	}

	block := resp.ToolUseBlock()
	if block == nil {
		return resp.Usage, fmt.Errorf("selector partial retry: no tool_use block in response")
	}

	var partial agent.SelectionPlan
	if err := json.Unmarshal(block.Input, &partial); err != nil {
		return resp.Usage, fmt.Errorf("selector partial retry: failed to parse selections: %w", err)
	}

	indexByOutline := make(map[int]int, len(issues))
	for _, issue := range issues {
		indexByOutline[issue.OutlineIndex] = issue.SelectionIndex
	}
	merged := 0
	for _, sel := range partial.Selections {
		selIdx, ok := indexByOutline[sel.OutlineIndex]
		if !ok || selIdx < 0 || selIdx >= len(current.Selections) {
			slog.Warn("[agent:selector] partial retry returned unexpected outlineIndex, ignoring",
				"outlineIndex", sel.OutlineIndex)
			continue
		}
		current.Selections[selIdx] = sel
		merged++
		slog.Info("[agent:selector]   slide remapped",
			"outlineIndex", sel.OutlineIndex,
			"sourceSlide", sel.SourceSlide,
			"rationale", sel.Rationale,
		)
	}
	if merged == 0 {
		return resp.Usage, fmt.Errorf("selector partial retry: no usable corrected selections returned")
	}

	slog.Info("[agent:selector] partial retry done",
		"corrected", merged,
		"duration", time.Since(start).Round(time.Millisecond),
	)
	return resp.Usage, nil
}
