package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/owulveryck/agentigslide/internal/agent"
	"github.com/owulveryck/agentigslide/internal/agent/designer"
	"github.com/owulveryck/agentigslide/internal/agent/outliner"
	"github.com/owulveryck/agentigslide/internal/agent/reviewer"
	"github.com/owulveryck/agentigslide/internal/agent/selector"
	"github.com/owulveryck/agentigslide/internal/agent/writer"
	"github.com/owulveryck/agentigslide/internal/metrics"
	"github.com/owulveryck/agentigslide/internal/model"
	"github.com/owulveryck/agentigslide/internal/trace"
	"github.com/owulveryck/agentigslide/internal/vertex"
)

// Orchestrator coordinates the multi-agent pipeline: Outliner -> Selector ->
// Writers (parallel) -> Assembler -> Reviewer (with retry loop).
type Orchestrator struct {
	client    *vertex.Client
	config    agent.Config
	collector *metrics.Collector
	issueLog  agent.IssueLog
	tracer    *trace.Tracer
	// Outline, when set, skips the outliner step and uses this pre-built
	// outline directly. Use this for interactive mode where the outline has
	// already been refined via chat before the pipeline starts.
	Outline *agent.PresentationOutline
	// ClosingSlide, when > 0, is appended as the last slide in the
	// assembled plan. It references a template slide (typically with no
	// editable fields) declared in the template's CLOSING_SLIDE file.
	ClosingSlide int
}

// Option configures optional Orchestrator behavior.
type Option func(*Orchestrator)

// WithTracer attaches a debug tracer to the pipeline.
func WithTracer(t *trace.Tracer) Option {
	return func(o *Orchestrator) { o.tracer = t }
}

// New creates an Orchestrator with the given Vertex AI client and agent
// configuration.
func New(client *vertex.Client, cfg agent.Config, opts ...Option) *Orchestrator {
	o := &Orchestrator{client: client, config: cfg, collector: metrics.NewCollector()}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// Tracer returns the debug tracer, if any.
func (o *Orchestrator) Tracer() *trace.Tracer {
	return o.tracer
}

// IssueLog returns the accumulated issue log from the pipeline run.
func (o *Orchestrator) IssueLog() agent.IssueLog {
	return o.issueLog
}

// Collector returns the metrics collector, allowing callers to record
// usage from steps that run before Generate (e.g. interactive outliner).
func (o *Orchestrator) Collector() *metrics.Collector {
	return o.collector
}

// Generate runs the full agentic pipeline and returns a GenerationPlan
// compatible with the existing plan.EnrichPlan / pipeline.ExecutePlan flow.
func (o *Orchestrator) Generate(ctx context.Context, userRequest, compactCatalog, templateInstructions string, agentMemories map[string]string) (*model.GenerationPlan, *metrics.Collector, error) {
	pipelineStart := time.Now()

	if o.config.PipelineTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.config.PipelineTimeout)
		defer cancel()
		slog.Info("[pipeline] starting multi-agent generation", "timeout", o.config.PipelineTimeout)
	} else {
		slog.Info("[pipeline] starting multi-agent generation")
	}

	if agentMemories == nil {
		agentMemories = make(map[string]string)
	}

	state := &agent.PipelineState{
		UserRequest:          userRequest,
		CompactCatalog:       compactCatalog,
		TemplateInstructions: templateInstructions,
		AgentMemories:        agentMemories,
	}

	if o.Outline != nil {
		slog.Info("[pipeline] step 1/5: outliner (using pre-built outline)")
		state.Outline = o.Outline
	} else {
		slog.Info("[pipeline] step 1/5: outliner")
		var outlinerRetries int
		var outlinerErr error
		for attempt := 0; attempt <= o.config.MaxOutlinerRetries; attempt++ {
			var validationErrStr string
			if outlinerErr != nil {
				validationErrStr = outlinerErr.Error()
			}

			attemptStart := time.Now()
			outlinerUsage, err := o.runOutliner(ctx, state, validationErrStr)
			if err != nil {
				return nil, o.collector, fmt.Errorf("outliner: %w", err)
			}

			outlinerErr = agent.ValidateOutline(state.Outline)

			ta := trace.OutlineAttempt{
				Attempt:    attempt,
				DurationMs: time.Since(attemptStart).Milliseconds(),
				TokensIn:   outlinerUsage.InputTokens,
				TokensOut:  outlinerUsage.OutputTokens,
			}
			if outlinerErr != nil {
				ta.ValidationError = outlinerErr.Error()
			}
			o.tracer.RecordOutlineAttempt(ta)

			if outlinerErr == nil {
				break
			}

			if attempt == o.config.MaxOutlinerRetries {
				slog.Warn("[pipeline] outliner validation failed after max retries, proceeding anyway",
					"issues", outlinerErr,
				)
				break
			}

			outlinerRetries++
			slog.Warn("[pipeline] outliner validation failed, retrying",
				"attempt", attempt+1,
				"error", outlinerErr,
			)
		}
		o.collector.SetOutlinerRetries(outlinerRetries)
	}

	agent.NormalizeOutline(state.Outline, state.CompactCatalog)
	o.traceOutlineResult(state)

	slog.Info("[pipeline] step 2/5: selector")
	var selectorRetries int
	var selectorErr error
	for attempt := 0; attempt <= o.config.MaxSelectorRetries; attempt++ {
		var validationErrStr string
		if selectorErr != nil {
			validationErrStr = selectorErr.Error()
		}

		attemptStart := time.Now()
		selectorUsage, err := o.runSelector(ctx, state, validationErrStr)
		if err != nil {
			return nil, o.collector, fmt.Errorf("selector: %w", err)
		}

		selectorErr = agent.ValidateSelection(state.Selections, state.Outline, state.CompactCatalog)
		if selectorErr == nil {
			selectorErr = agent.ValidateSelectionGlobal(state.Selections, state.Outline)
		}

		sa := trace.SelectionAttempt{
			Attempt:    attempt,
			DurationMs: time.Since(attemptStart).Milliseconds(),
			TokensIn:   selectorUsage.InputTokens,
			TokensOut:  selectorUsage.OutputTokens,
		}
		if selectorErr != nil {
			sa.ValidationError = selectorErr.Error()
		}
		o.tracer.RecordSelectionAttempt(sa)

		if selectorErr == nil {
			if attempt > 0 {
				o.issueLog.MarkResolved("selector", attempt-1)
			}
			break
		}

		o.issueLog.Record("selector", attempt, []agent.ReviewIssue{{
			IssueType:   "validation_error",
			Description: selectorErr.Error(),
		}})

		if attempt == o.config.MaxSelectorRetries {
			dropped := agent.SanitizeSelection(state.Selections, state.CompactCatalog)
			slog.Warn("[pipeline] selector validation failed after max retries, sanitized and proceeding",
				"issues", selectorErr,
				"droppedEntries", dropped,
			)
			break
		}

		selectorRetries++
		slog.Warn("[pipeline] selector validation failed, retrying",
			"attempt", attempt+1,
			"error", selectorErr,
		)
	}
	o.collector.SetSelectorRetries(selectorRetries)
	o.traceSelectionResult(state)

	slog.Info("[pipeline] step 3/5: writers", "count", len(state.Selections.Selections), "maxParallel", o.config.MaxParallel)
	if err := o.runWriters(ctx, state); err != nil {
		return nil, o.collector, fmt.Errorf("writers: %w", err)
	}

	slog.Info("[pipeline] step 4/5: assembling plan")
	o.assemble(state)

	slog.Info("[pipeline] step 5/5: reviewer")
	var reviewerRetries int
	var lastCorrectedIndices []int
	for attempt := 0; attempt <= o.config.MaxReviewRetries; attempt++ {
		attemptStart := time.Now()
		var reviewUsage vertex.Usage
		var reviewErr error
		if attempt == 0 || state.ReviewResult == nil {
			reviewUsage, reviewErr = o.runReviewer(ctx, state)
		} else {
			reviewUsage, reviewErr = o.runReviewerSubset(ctx, state, lastCorrectedIndices)
		}
		if reviewErr != nil {
			slog.Warn("[pipeline] reviewer failed", "error", reviewErr, "attempt", attempt)
			o.tracer.RecordError("reviewer", reviewErr.Error())
			if attempt < o.config.MaxReviewRetries {
				slog.Info("[pipeline] retrying reviewer", "nextAttempt", attempt+1)
				continue
			}
			slog.Warn("[pipeline] reviewer failed after all retries, proceeding without review")
			break
		}

		o.issueLog.Record("reviewer", attempt, state.ReviewResult.Issues)

		ri := trace.ReviewIteration{
			Attempt:    attempt,
			Approved:   state.ReviewResult.Approved,
			IssueCount: len(state.ReviewResult.Issues),
			DurationMs: time.Since(attemptStart).Milliseconds(),
			TokensIn:   reviewUsage.InputTokens,
			TokensOut:  reviewUsage.OutputTokens,
		}
		for _, issue := range state.ReviewResult.Issues {
			ri.Issues = append(ri.Issues, trace.ReviewIssueTrace{
				SlideIndex:  issue.SlideIndex,
				Field:       issue.Field,
				IssueType:   issue.IssueType,
				Description: issue.Description,
				Suggestion:  issue.Suggestion,
			})
		}
		o.tracer.RecordReviewIteration(ri)

		if state.ReviewResult.Approved {
			if attempt > 0 {
				o.issueLog.MarkResolved("reviewer", attempt-1)
			}
			break
		}

		if attempt == o.config.MaxReviewRetries {
			slog.Warn("[pipeline] reviewer did not approve after max retries, proceeding anyway",
				"issues", len(state.ReviewResult.Issues),
			)
			break
		}

		reviewerRetries++
		slog.Info("[pipeline] review iteration: re-running affected writers",
			"issues", len(state.ReviewResult.Issues),
			"attempt", attempt+1,
		)

		corrected, err := o.handleReviewIssuesReturn(ctx, state)
		if err != nil {
			slog.Warn("[pipeline] failed to handle review issues, proceeding with current plan", "error", err)
			break
		}
		if len(corrected) == 0 {
			slog.Warn("[pipeline] all remaining issues are wrong_template (unfixable by writer), proceeding",
				"issues", len(state.ReviewResult.Issues),
			)
			break
		}
		lastCorrectedIndices = corrected

		o.assemble(state)
	}
	o.collector.SetReviewerRetries(reviewerRetries)

	pipelineDuration := time.Since(pipelineStart)
	o.collector.SetSlidesGenerated(len(state.AssembledPlan.Slides))
	o.collector.SetPipelineDuration(pipelineDuration)

	slog.Info("[pipeline] generation complete",
		"slides", len(state.AssembledPlan.Slides),
		"totalDuration", pipelineDuration.Round(time.Millisecond),
	)

	return state.AssembledPlan, o.collector, nil
}

func (o *Orchestrator) runOutliner(ctx context.Context, state *agent.PipelineState, previousErrors ...string) (vertex.Usage, error) {
	ag := outliner.New(o.client, o.config.OutlinerModel, o.config.OutlinerMaxTokens)
	start := time.Now()
	outline, usage, err := ag.Run(ctx, state.UserRequest, state.TemplateInstructions, state.AgentMemories["outliner"], previousErrors...)
	if err != nil {
		return usage, err
	}
	o.collector.Record(metrics.AgentCall{
		Agent: "outliner", Model: o.config.OutlinerModel,
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
		CacheReadInputTokens: usage.CacheReadInputTokens, CacheCreationInputTokens: usage.CacheCreationInputTokens,
		Duration: time.Since(start),
	})
	state.Outline = outline
	return usage, nil
}

func (o *Orchestrator) runSelector(ctx context.Context, state *agent.PipelineState, previousErrors ...string) (vertex.Usage, error) {
	ag := selector.New(o.client, o.config.SelectorModel)
	start := time.Now()
	selections, usage, err := ag.Run(ctx, state.Outline, state.CompactCatalog, state.TemplateInstructions, state.AgentMemories["selector"], previousErrors...)
	if err != nil {
		return usage, err
	}
	o.collector.Record(metrics.AgentCall{
		Agent: "selector", Model: o.config.SelectorModel,
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
		CacheReadInputTokens: usage.CacheReadInputTokens, CacheCreationInputTokens: usage.CacheCreationInputTokens,
		Duration: time.Since(start),
	})
	state.Selections = selections
	state.SlideContents = make([]agent.SlideContent, len(selections.Selections))
	return usage, nil
}

func (o *Orchestrator) runWriters(ctx context.Context, state *agent.PipelineState) error {
	indices := make([]int, len(state.Selections.Selections))
	for i := range indices {
		indices[i] = i
	}
	return o.writeSlides(ctx, state, indices, nil)
}

func (o *Orchestrator) assemble(state *agent.PipelineState) {
	plan := &model.GenerationPlan{
		PresentationTitle: state.Outline.PresentationTitle,
	}

	for i, sc := range state.SlideContents {
		sr := model.SlideRequest{
			SourceSlide:   sc.SourceSlide,
			Modifications: sc.Modifications,
		}
		if state.DiagramSpecs != nil {
			if spec, ok := state.DiagramSpecs[i]; ok && spec != nil {
				sr.Diagram = spec
			}
		}
		plan.Slides = append(plan.Slides, sr)
	}

	if o.ClosingSlide > 0 {
		plan.Slides = append(plan.Slides, model.SlideRequest{
			SourceSlide: o.ClosingSlide,
		})
	}

	state.AssembledPlan = plan
	slog.Info("assembler: plan assembled", "slides", len(plan.Slides))
}

func (o *Orchestrator) runReviewer(ctx context.Context, state *agent.PipelineState) (vertex.Usage, error) {
	ag := reviewer.New(o.client, o.config.ReviewerModel)
	start := time.Now()
	result, usage, err := ag.Run(ctx, state.AssembledPlan, state.UserRequest, state.CompactCatalog, state.TemplateInstructions, o.config.ReviewerThinkingBudget, state.AgentMemories["reviewer"])
	if err != nil {
		return usage, err
	}
	o.collector.Record(metrics.AgentCall{
		Agent: "reviewer", Model: o.config.ReviewerModel,
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
		CacheReadInputTokens: usage.CacheReadInputTokens, CacheCreationInputTokens: usage.CacheCreationInputTokens,
		Duration: time.Since(start),
	})
	state.ReviewResult = result
	return usage, nil
}

func (o *Orchestrator) handleReviewIssuesReturn(ctx context.Context, state *agent.PipelineState) ([]int, error) {
	feedbackByIndex := make(map[int][]agent.ReviewIssue)
	for _, issue := range state.ReviewResult.Issues {
		if issue.SlideIndex < 0 || issue.SlideIndex >= len(state.Selections.Selections) {
			slog.Warn("[pipeline] issue references slide outside plan, skipping (structural)",
				"slide", issue.SlideIndex,
				"issueType", issue.IssueType,
				"description", issue.Description,
			)
			continue
		}
		if issue.IssueType == "wrong_template" {
			slog.Warn("[pipeline] wrong_template issue cannot be fixed by writer, skipping",
				"slide", issue.SlideIndex,
				"sourceSlide", state.Selections.Selections[issue.SlideIndex].SourceSlide,
				"description", issue.Description,
			)
			continue
		}
		if issue.IssueType == "missing_content" && issue.Field == "" {
			slog.Warn("[pipeline] missing_content without specific field is structural, skipping",
				"slide", issue.SlideIndex,
				"description", issue.Description,
			)
			continue
		}
		feedbackByIndex[issue.SlideIndex] = append(feedbackByIndex[issue.SlideIndex], issue)
	}

	indices := make([]int, 0, len(feedbackByIndex))
	for idx := range feedbackByIndex {
		indices = append(indices, idx)
	}

	if len(indices) == 0 {
		return nil, nil
	}

	return indices, o.writeSlides(ctx, state, indices, feedbackByIndex)
}

func (o *Orchestrator) runReviewerSubset(ctx context.Context, state *agent.PipelineState, correctedIndices []int) (vertex.Usage, error) {
	subsetModel := o.config.ReviewerSubsetModel
	ag := reviewer.New(o.client, subsetModel)
	start := time.Now()
	result, usage, err := ag.RunSubset(ctx, state.AssembledPlan, correctedIndices, state.ReviewResult.Issues, state.UserRequest, state.CompactCatalog, state.TemplateInstructions, o.config.ReviewerThinkingBudget, state.AgentMemories["reviewer"])
	if err != nil {
		return usage, err
	}
	o.collector.Record(metrics.AgentCall{
		Agent: "reviewer", Model: subsetModel,
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
		CacheReadInputTokens: usage.CacheReadInputTokens, CacheCreationInputTokens: usage.CacheCreationInputTokens,
		Duration: time.Since(start),
	})
	state.ReviewResult = result
	return usage, nil
}

func (o *Orchestrator) writeSlides(ctx context.Context, state *agent.PipelineState, indices []int, feedbackByIndex map[int][]agent.ReviewIssue) error {
	slideNeeds := agent.FlattenNeeds(state.Outline)

	sectionDividerSeq := make(map[int]int)
	seq := 1
	for i, sel := range state.Selections.Selections {
		oi := sel.OutlineIndex
		if oi >= 0 && oi < len(slideNeeds) && slideNeeds[oi].SlideType == "section_divider" {
			sectionDividerSeq[i] = seq
			seq++
		}
	}

	sem := make(chan struct{}, o.config.MaxParallel)
	var wg sync.WaitGroup
	errs := make([]error, len(state.Selections.Selections))

	for _, idx := range indices {
		selection := state.Selections.Selections[idx]

		var need agent.SlideNeed
		if selection.OutlineIndex >= 0 && selection.OutlineIndex < len(slideNeeds) {
			need = slideNeeds[selection.OutlineIndex]
		}

		if num, ok := sectionDividerSeq[idx]; ok {
			need.ContentItems = append([]string{fmt.Sprintf("section_number=%d", num)}, need.ContentItems...)
		}

		var feedback []agent.ReviewIssue
		if feedbackByIndex != nil {
			feedback = feedbackByIndex[idx]
		}

		if need.SlideType == "diagram" {
			wg.Add(1)
			go func(i int, sn agent.SlideNeed, fb []agent.ReviewIssue) {
				defer wg.Done()
				if ctx.Err() != nil {
					errs[i] = ctx.Err()
					return
				}
				sem <- struct{}{}
				defer func() { <-sem }()

				d := designer.New(o.client, o.config.DesignerModel)
				designerFeedback := append([]agent.ReviewIssue{}, fb...)

				var lastErr error
				for designerAttempt := 0; designerAttempt <= o.config.MaxDesignerRetries; designerAttempt++ {
					start := time.Now()
					spec, usage, err := d.DesignDiagram(ctx, sn, state.TemplateInstructions, state.AgentMemories["designer"], designerFeedback...)
					o.collector.Record(metrics.AgentCall{
						Agent: "designer", Model: o.config.DesignerModel,
						InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
						CacheReadInputTokens: usage.CacheReadInputTokens, CacheCreationInputTokens: usage.CacheCreationInputTokens,
						Duration: time.Since(start),
					})
					if err == nil {
						state.SetDiagramSpec(i, spec)
						state.SetSlideContent(i, agent.SlideContent{SourceSlide: -1})
						lastErr = nil
						break
					}
					lastErr = err
					if designerAttempt == o.config.MaxDesignerRetries {
						break
					}
					slog.Warn("[pipeline] designer validation failed, retrying",
						"slide", i,
						"attempt", designerAttempt+1,
						"error", err,
					)
					designerFeedback = append(designerFeedback, agent.ReviewIssue{
						IssueType:   "validation_error",
						Description: err.Error(),
					})
				}
				if lastErr != nil {
					errs[i] = lastErr
				}
			}(idx, need, feedback)
			continue
		}

		templateFields := agent.ParseSlideFields(state.CompactCatalog, selection.SourceSlide)

		writerModel := o.config.WriterModel
		if len(templateFields) <= 2 {
			writerModel = o.config.WriterSimpleModel
		}

		wg.Add(1)
		go func(i int, sourceSlide int, sn agent.SlideNeed, fields []agent.TemplateField, mdl string, fb []agent.ReviewIssue) {
			defer wg.Done()
			if ctx.Err() != nil {
				errs[i] = ctx.Err()
				return
			}
			sem <- struct{}{}
			defer func() { <-sem }()

			w := writer.New(o.client, mdl)
			start := time.Now()
			content, usage, err := w.WriteSlide(ctx, sourceSlide, sn, fields, state.TemplateInstructions, state.AgentMemories["writer"], fb...)
			if err != nil {
				errs[i] = err
				return
			}
			writerDuration := time.Since(start)
			o.collector.Record(metrics.AgentCall{
				Agent: "writer", Model: mdl,
				InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
				CacheReadInputTokens: usage.CacheReadInputTokens, CacheCreationInputTokens: usage.CacheCreationInputTokens,
				Duration: writerDuration,
			})

			wt := o.buildWriterTrace(i, sourceSlide, sn, fields, mdl, writerDuration, usage, content, fb)
			agent.EnforceMaxChars(content, fields)
			o.recordEnforcement(&wt, content, fields)
			o.tracer.RecordWriter(wt)

			state.SetSlideContent(i, *content)
		}(idx, selection.SourceSlide, need, templateFields, writerModel, feedback)
	}

	wg.Wait()

	var writerErrors []error
	for i, err := range errs {
		if err != nil {
			writerErrors = append(writerErrors, fmt.Errorf("slide index %d: %w", i, err))
		}
	}
	if len(writerErrors) > 0 {
		return fmt.Errorf("writers failed: %w", errors.Join(writerErrors...))
	}

	return nil
}

func (o *Orchestrator) traceOutlineResult(state *agent.PipelineState) {
	if o.tracer == nil || state.Outline == nil {
		return
	}
	var sections []trace.SectionSummary
	for _, sec := range state.Outline.Sections {
		ss := trace.SectionSummary{Title: sec.Title, Purpose: sec.Purpose}
		for _, need := range sec.SlideNeeds {
			ss.SlideNeeds = append(ss.SlideNeeds, trace.SlideNeedSummary{
				Intent:        need.Intent,
				ItemCount:     need.ItemCount,
				MaxItemLength: need.MaxItemLength,
				SlideType:     need.SlideType,
				NeedsTitle:    need.NeedsTitle,
				NeedsSubtitle: need.NeedsSubtitle,
				ContentItems:  need.ContentItems,
			})
		}
		sections = append(sections, ss)
	}
	summary := state.UserRequest
	if len(summary) > 500 {
		summary = summary[:500] + "..."
	}
	o.tracer.SetOutlineResult(summary, sections)
}

func (o *Orchestrator) traceSelectionResult(state *agent.PipelineState) {
	if o.tracer == nil || state.Selections == nil {
		return
	}
	var entries []trace.SelectionEntry
	for i, sel := range state.Selections.Selections {
		entry := trace.SelectionEntry{
			Index:       i,
			SourceSlide: sel.SourceSlide,
			Rationale:   sel.Rationale,
		}
		fields := agent.ParseSlideFields(state.CompactCatalog, sel.SourceSlide)
		for _, f := range fields {
			entry.TemplateFields = append(entry.TemplateFields, trace.FieldSummary{
				VariableName: f.VariableName,
				Role:         f.Role,
				MaxChars:     f.MaxChars,
			})
		}
		entries = append(entries, entry)
	}
	o.tracer.SetSelectionResult(entries)
}

func (o *Orchestrator) buildWriterTrace(slideIndex, sourceSlide int, sn agent.SlideNeed, fields []agent.TemplateField, mdl string, dur time.Duration, usage vertex.Usage, content *agent.SlideContent, fb []agent.ReviewIssue) trace.WriterTrace {
	wt := trace.WriterTrace{
		SlideIndex:  slideIndex,
		SourceSlide: sourceSlide,
		ModelUsed:   mdl,
		SlideType:   sn.SlideType,
		DurationMs:  dur.Milliseconds(),
		TokensIn:    usage.InputTokens,
		TokensOut:   usage.OutputTokens,
	}
	wt.Input.Intent = sn.Intent
	wt.Input.ContentItems = sn.ContentItems
	for _, f := range fields {
		wt.Input.Fields = append(wt.Input.Fields, trace.FieldSummary{
			VariableName: f.VariableName,
			Role:         f.Role,
			MaxChars:     f.MaxChars,
		})
	}
	maxByField := make(map[string]int, len(fields))
	for _, f := range fields {
		if f.MaxChars > 0 {
			maxByField[f.VariableName] = f.MaxChars
		}
	}
	for _, mod := range content.Modifications {
		charCount := len([]rune(mod.NewText))
		mc := maxByField[mod.VariableName]
		wt.Output.Modifications = append(wt.Output.Modifications, trace.ModificationTrace{
			VariableName: mod.VariableName,
			NewText:      mod.NewText,
			CharCount:    charCount,
			MaxChars:     mc,
			OverLimit:    mc > 0 && charCount > mc,
		})
	}
	for _, issue := range fb {
		wt.Feedback = append(wt.Feedback, trace.FeedbackEntry{
			IssueType:   issue.IssueType,
			Description: issue.Description,
			Suggestion:  issue.Suggestion,
			Field:       issue.Field,
		})
	}
	return wt
}

func (o *Orchestrator) recordEnforcement(wt *trace.WriterTrace, content *agent.SlideContent, fields []agent.TemplateField) {
	if o.tracer == nil {
		return
	}
	maxByField := make(map[string]int, len(fields))
	for _, f := range fields {
		if f.MaxChars > 0 {
			maxByField[f.VariableName] = f.MaxChars
		}
	}
	for _, mod := range content.Modifications {
		charCount := len([]rune(mod.NewText))
		for j := range wt.Output.Modifications {
			mt := &wt.Output.Modifications[j]
			if mt.VariableName == mod.VariableName && mt.CharCount != charCount {
				wt.Enforcement = append(wt.Enforcement, trace.EnforcementAction{
					VariableName:   mod.VariableName,
					OriginalLength: mt.CharCount,
					TruncatedTo:    charCount,
					MaxChars:       maxByField[mod.VariableName],
				})
				mt.CharCount = charCount
				mt.NewText = mod.NewText
				mt.OverLimit = false
				break
			}
		}
	}
}
