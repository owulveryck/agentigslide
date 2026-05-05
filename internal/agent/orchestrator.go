package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/owulveryck/slideAppScripter/internal/model"
	"github.com/owulveryck/slideAppScripter/internal/vertex"
)

// Orchestrator coordinates the multi-agent pipeline: Outliner -> Selector ->
// Writers (parallel) -> Assembler -> Reviewer (with retry loop).
type Orchestrator struct {
	client *vertex.Client
	config Config
}

// NewOrchestrator creates an Orchestrator with the given Vertex AI client and
// agent configuration.
func NewOrchestrator(client *vertex.Client, cfg Config) *Orchestrator {
	return &Orchestrator{client: client, config: cfg}
}

// Generate runs the full agentic pipeline and returns a GenerationPlan
// compatible with the existing plan.EnrichPlan / pipeline.ExecutePlan flow.
func (o *Orchestrator) Generate(ctx context.Context, userRequest, compactCatalog, templateInstructions string) (*model.GenerationPlan, error) {
	pipelineStart := time.Now()
	slog.Info("[pipeline] starting multi-agent generation")

	state := &PipelineState{
		UserRequest:          userRequest,
		CompactCatalog:       compactCatalog,
		TemplateInstructions: templateInstructions,
	}

	slog.Info("[pipeline] step 1/5: outliner")
	if err := o.runOutliner(ctx, state); err != nil {
		return nil, fmt.Errorf("outliner: %w", err)
	}
	if err := validateOutline(state.Outline); err != nil {
		return nil, fmt.Errorf("outline validation: %w", err)
	}

	slog.Info("[pipeline] step 2/5: selector")
	var selectorErr error
	for attempt := 0; attempt <= o.config.MaxSelectorRetries; attempt++ {
		var validationErrStr string
		if selectorErr != nil {
			validationErrStr = selectorErr.Error()
		}

		if err := o.runSelector(ctx, state, validationErrStr); err != nil {
			return nil, fmt.Errorf("selector: %w", err)
		}

		selectorErr = validateSelection(state.Selections, state.Outline, state.CompactCatalog)
		if selectorErr == nil {
			break
		}

		if attempt == o.config.MaxSelectorRetries {
			return nil, fmt.Errorf("selection validation (after %d retries): %w", attempt, selectorErr)
		}

		slog.Warn("[pipeline] selector validation failed, retrying",
			"attempt", attempt+1,
			"error", selectorErr,
		)
	}

	slog.Info("[pipeline] step 3/5: writers", "count", len(state.Selections.Selections), "maxParallel", o.config.MaxParallel)
	if err := o.runWriters(ctx, state); err != nil {
		return nil, fmt.Errorf("writers: %w", err)
	}

	slog.Info("[pipeline] step 4/5: assembling plan")
	o.assemble(state)

	slog.Info("[pipeline] step 5/5: reviewer")
	for attempt := 0; attempt <= o.config.MaxReviewRetries; attempt++ {
		if err := o.runReviewer(ctx, state); err != nil {
			slog.Warn("[pipeline] reviewer failed, proceeding without review", "error", err, "attempt", attempt)
			break
		}

		if state.ReviewResult.Approved {
			break
		}

		if attempt == o.config.MaxReviewRetries {
			slog.Warn("[pipeline] reviewer did not approve after max retries, proceeding anyway",
				"issues", len(state.ReviewResult.Issues),
			)
			break
		}

		slog.Info("[pipeline] review iteration: re-running affected writers",
			"issues", len(state.ReviewResult.Issues),
			"attempt", attempt+1,
		)

		if err := o.handleReviewIssues(ctx, state); err != nil {
			slog.Warn("[pipeline] failed to handle review issues, proceeding with current plan", "error", err)
			break
		}

		o.assemble(state)
	}

	slog.Info("[pipeline] generation complete",
		"slides", len(state.AssembledPlan.Slides),
		"totalDuration", time.Since(pipelineStart).Round(time.Millisecond),
	)

	return state.AssembledPlan, nil
}

func (o *Orchestrator) runOutliner(ctx context.Context, state *PipelineState) error {
	agent := NewOutlinerAgent(o.client, o.config.OutlinerModel)
	outline, err := agent.Run(ctx, state.UserRequest, state.TemplateInstructions)
	if err != nil {
		return err
	}
	state.Outline = outline
	return nil
}

func (o *Orchestrator) runSelector(ctx context.Context, state *PipelineState, previousErrors ...string) error {
	agent := NewSelectorAgent(o.client, o.config.SelectorModel)
	selections, err := agent.Run(ctx, state.Outline, state.CompactCatalog, state.TemplateInstructions, previousErrors...)
	if err != nil {
		return err
	}
	state.Selections = selections
	state.SlideContents = make([]SlideContent, len(selections.Selections))
	return nil
}

func (o *Orchestrator) runWriters(ctx context.Context, state *PipelineState) error {
	indices := make([]int, len(state.Selections.Selections))
	for i := range indices {
		indices[i] = i
	}
	return o.writeSlides(ctx, state, indices, nil)
}

func (o *Orchestrator) assemble(state *PipelineState) {
	plan := &model.GenerationPlan{
		PresentationTitle: state.Outline.PresentationTitle,
	}

	for _, sc := range state.SlideContents {
		plan.Slides = append(plan.Slides, model.SlideRequest{
			SourceSlide:   sc.SourceSlide,
			Modifications: sc.Modifications,
		})
	}

	state.AssembledPlan = plan
	slog.Info("assembler: plan assembled", "slides", len(plan.Slides))
}

func (o *Orchestrator) runReviewer(ctx context.Context, state *PipelineState) error {
	agent := NewReviewerAgent(o.client, o.config.ReviewerModel)
	result, err := agent.Run(ctx, state.AssembledPlan, state.UserRequest, state.CompactCatalog, state.TemplateInstructions)
	if err != nil {
		return err
	}
	state.ReviewResult = result
	return nil
}

// handleReviewIssues re-runs the Writer for slides that have issues,
// passing reviewer feedback so the Writer can adjust its output.
func (o *Orchestrator) handleReviewIssues(ctx context.Context, state *PipelineState) error {
	feedbackByIndex := make(map[int][]ReviewIssue)
	for _, issue := range state.ReviewResult.Issues {
		if issue.SlideIndex >= 0 && issue.SlideIndex < len(state.Selections.Selections) {
			feedbackByIndex[issue.SlideIndex] = append(feedbackByIndex[issue.SlideIndex], issue)
		}
	}

	indices := make([]int, 0, len(feedbackByIndex))
	for idx := range feedbackByIndex {
		indices = append(indices, idx)
	}

	return o.writeSlides(ctx, state, indices, feedbackByIndex)
}

// writeSlides runs Writers in parallel for the given slide indices.
// If feedbackByIndex is non-nil, matching ReviewIssues are forwarded to the
// Writer so it can adjust its output based on reviewer corrections.
func (o *Orchestrator) writeSlides(ctx context.Context, state *PipelineState, indices []int, feedbackByIndex map[int][]ReviewIssue) error {
	slideNeeds := o.flattenSlideNeeds(state.Outline)

	sem := make(chan struct{}, o.config.MaxParallel)
	var wg sync.WaitGroup
	errs := make([]error, len(state.Selections.Selections))

	for _, idx := range indices {
		selection := state.Selections.Selections[idx]

		templateFields := ParseSlideFields(state.CompactCatalog, selection.SourceSlide)

		writerModel := o.config.WriterModel
		if len(templateFields) <= 2 {
			writerModel = o.config.WriterSimpleModel
		}

		var need SlideNeed
		if selection.OutlineIndex >= 0 && selection.OutlineIndex < len(slideNeeds) {
			need = slideNeeds[selection.OutlineIndex]
		}

		var feedback []ReviewIssue
		if feedbackByIndex != nil {
			feedback = feedbackByIndex[idx]
		}

		wg.Add(1)
		go func(i int, sourceSlide int, sn SlideNeed, fields []TemplateField, mdl string, fb []ReviewIssue) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			writer := NewWriterAgent(o.client, mdl)
			content, err := writer.WriteSlide(ctx, sourceSlide, sn, fields, state.TemplateInstructions, fb...)
			if err != nil {
				errs[i] = err
				return
			}
			enforceMaxChars(content, fields)
			filterValidFields(content, fields)
			state.SetSlideContent(i, *content)
		}(idx, selection.SourceSlide, need, templateFields, writerModel, feedback)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			return fmt.Errorf("writer for slide index %d failed: %w", i, err)
		}
	}

	return nil
}

// flattenSlideNeeds returns all SlideNeeds from the outline in order, matching
// the outlineIndex used by the Selector.
func (o *Orchestrator) flattenSlideNeeds(outline *PresentationOutline) []SlideNeed {
	var needs []SlideNeed
	for _, section := range outline.Sections {
		needs = append(needs, section.SlideNeeds...)
	}
	return needs
}

// filterValidFields removes any writer modifications that target variable names
// not present in the template fields. This catches cases where the writer
// invents field names for templates with few or no editable fields.
func filterValidFields(content *SlideContent, fields []TemplateField) {
	if len(fields) == 0 && len(content.Modifications) > 0 {
		slog.Warn("[filterValidFields] dropping all modifications for template with no fields",
			"sourceSlide", content.SourceSlide,
			"dropped", len(content.Modifications),
		)
		content.Modifications = nil
		return
	}

	validNames := make(map[string]bool, len(fields))
	for _, f := range fields {
		validNames[f.VariableName] = true
	}

	var filtered []model.TextModification
	for _, mod := range content.Modifications {
		if validNames[mod.VariableName] {
			filtered = append(filtered, mod)
		} else {
			slog.Warn("[filterValidFields] dropping modification for unknown field",
				"sourceSlide", content.SourceSlide,
				"field", mod.VariableName,
			)
		}
	}
	content.Modifications = filtered
}

// enforceMaxChars truncates any writer output that exceeds the maxChars
// constraint from the template fields.
func enforceMaxChars(content *SlideContent, fields []TemplateField) {
	maxByField := make(map[string]int, len(fields))
	for _, f := range fields {
		if f.MaxChars > 0 {
			maxByField[f.VariableName] = f.MaxChars
		}
	}

	for i := range content.Modifications {
		mod := &content.Modifications[i]
		limit, ok := maxByField[mod.VariableName]
		if !ok || limit <= 0 {
			continue
		}
		text := []rune(mod.NewText)
		if len(text) <= limit {
			continue
		}
		slog.Warn("[enforceMaxChars] truncating field",
			"sourceSlide", content.SourceSlide,
			"field", mod.VariableName,
			"length", len(text),
			"maxChars", limit,
		)
		truncated := string(text[:limit])
		if idx := strings.LastIndex(truncated, " "); idx > limit*2/3 {
			truncated = truncated[:idx]
		}
		// Avoid breaking inside markdown bold markers
		if open := strings.Count(truncated, "**"); open%2 != 0 {
			if idx := strings.LastIndex(truncated, "**"); idx >= 0 {
				truncated = truncated[:idx]
			}
		}
		mod.NewText = strings.TrimSpace(truncated)
	}
}
