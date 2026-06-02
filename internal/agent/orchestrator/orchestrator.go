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
	"github.com/owulveryck/agentigslide/internal/vertex"
)

// Orchestrator coordinates the multi-agent pipeline: Outliner -> Selector ->
// Writers (parallel) -> Assembler -> Reviewer (with retry loop).
type Orchestrator struct {
	client    *vertex.Client
	config    agent.Config
	collector *metrics.Collector
	issueLog  agent.IssueLog
	// Outline, when set, skips the outliner step and uses this pre-built
	// outline directly. Use this for interactive mode where the outline has
	// already been refined via chat before the pipeline starts.
	Outline *agent.PresentationOutline
}

// New creates an Orchestrator with the given Vertex AI client and agent
// configuration.
func New(client *vertex.Client, cfg agent.Config) *Orchestrator {
	return &Orchestrator{client: client, config: cfg, collector: metrics.NewCollector()}
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
		if err := o.runOutliner(ctx, state); err != nil {
			return nil, o.collector, fmt.Errorf("outliner: %w", err)
		}
	}
	if err := agent.ValidateOutline(state.Outline); err != nil {
		return nil, o.collector, fmt.Errorf("outline validation: %w", err)
	}

	agent.NormalizeOutline(state.Outline, state.CompactCatalog)

	slog.Info("[pipeline] step 2/5: selector")
	var selectorRetries int
	var selectorErr error
	for attempt := 0; attempt <= o.config.MaxSelectorRetries; attempt++ {
		var validationErrStr string
		if selectorErr != nil {
			validationErrStr = selectorErr.Error()
		}

		if err := o.runSelector(ctx, state, validationErrStr); err != nil {
			return nil, o.collector, fmt.Errorf("selector: %w", err)
		}

		selectorErr = agent.ValidateSelection(state.Selections, state.Outline, state.CompactCatalog)
		if selectorErr == nil {
			selectorErr = agent.ValidateSelectionGlobal(state.Selections, state.Outline)
		}
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
			slog.Warn("[pipeline] selector validation failed after max retries, proceeding anyway",
				"issues", selectorErr,
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
		var reviewErr error
		if attempt == 0 || state.ReviewResult == nil {
			reviewErr = o.runReviewer(ctx, state)
		} else {
			reviewErr = o.runReviewerSubset(ctx, state, lastCorrectedIndices)
		}
		if reviewErr != nil {
			slog.Warn("[pipeline] reviewer failed", "error", reviewErr, "attempt", attempt)
			if attempt < o.config.MaxReviewRetries {
				slog.Info("[pipeline] retrying reviewer", "nextAttempt", attempt+1)
				continue
			}
			slog.Warn("[pipeline] reviewer failed after all retries, proceeding without review")
			break
		}

		o.issueLog.Record("reviewer", attempt, state.ReviewResult.Issues)

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

func (o *Orchestrator) runOutliner(ctx context.Context, state *agent.PipelineState) error {
	ag := outliner.New(o.client, o.config.OutlinerModel, o.config.OutlinerMaxTokens)
	start := time.Now()
	outline, usage, err := ag.Run(ctx, state.UserRequest, state.TemplateInstructions, state.AgentMemories["outliner"])
	if err != nil {
		return err
	}
	o.collector.Record(metrics.AgentCall{
		Agent: "outliner", Model: o.config.OutlinerModel,
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
		CacheReadInputTokens: usage.CacheReadInputTokens, CacheCreationInputTokens: usage.CacheCreationInputTokens,
		Duration: time.Since(start),
	})
	state.Outline = outline
	return nil
}

func (o *Orchestrator) runSelector(ctx context.Context, state *agent.PipelineState, previousErrors ...string) error {
	ag := selector.New(o.client, o.config.SelectorModel)
	start := time.Now()
	selections, usage, err := ag.Run(ctx, state.Outline, state.CompactCatalog, state.TemplateInstructions, state.AgentMemories["selector"], previousErrors...)
	if err != nil {
		return err
	}
	o.collector.Record(metrics.AgentCall{
		Agent: "selector", Model: o.config.SelectorModel,
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
		CacheReadInputTokens: usage.CacheReadInputTokens, CacheCreationInputTokens: usage.CacheCreationInputTokens,
		Duration: time.Since(start),
	})
	state.Selections = selections
	state.SlideContents = make([]agent.SlideContent, len(selections.Selections))
	return nil
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

	state.AssembledPlan = plan
	slog.Info("assembler: plan assembled", "slides", len(plan.Slides))
}

func (o *Orchestrator) runReviewer(ctx context.Context, state *agent.PipelineState) error {
	ag := reviewer.New(o.client, o.config.ReviewerModel)
	start := time.Now()
	result, usage, err := ag.Run(ctx, state.AssembledPlan, state.UserRequest, state.CompactCatalog, state.TemplateInstructions, o.config.ReviewerThinkingBudget, state.AgentMemories["reviewer"])
	if err != nil {
		return err
	}
	o.collector.Record(metrics.AgentCall{
		Agent: "reviewer", Model: o.config.ReviewerModel,
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
		CacheReadInputTokens: usage.CacheReadInputTokens, CacheCreationInputTokens: usage.CacheCreationInputTokens,
		Duration: time.Since(start),
	})
	state.ReviewResult = result
	return nil
}

func (o *Orchestrator) handleReviewIssuesReturn(ctx context.Context, state *agent.PipelineState) ([]int, error) {
	feedbackByIndex := make(map[int][]agent.ReviewIssue)
	for _, issue := range state.ReviewResult.Issues {
		if issue.SlideIndex < 0 || issue.SlideIndex >= len(state.Selections.Selections) {
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

func (o *Orchestrator) runReviewerSubset(ctx context.Context, state *agent.PipelineState, correctedIndices []int) error {
	subsetModel := o.config.ReviewerSubsetModel
	ag := reviewer.New(o.client, subsetModel)
	start := time.Now()
	result, usage, err := ag.RunSubset(ctx, state.AssembledPlan, correctedIndices, state.ReviewResult.Issues, state.UserRequest, state.CompactCatalog, state.TemplateInstructions, o.config.ReviewerThinkingBudget, state.AgentMemories["reviewer"])
	if err != nil {
		return err
	}
	o.collector.Record(metrics.AgentCall{
		Agent: "reviewer", Model: subsetModel,
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
		CacheReadInputTokens: usage.CacheReadInputTokens, CacheCreationInputTokens: usage.CacheCreationInputTokens,
		Duration: time.Since(start),
	})
	state.ReviewResult = result
	return nil
}

func (o *Orchestrator) writeSlides(ctx context.Context, state *agent.PipelineState, indices []int, feedbackByIndex map[int][]agent.ReviewIssue) error {
	slideNeeds := agent.FlattenNeeds(state.Outline)

	sem := make(chan struct{}, o.config.MaxParallel)
	var wg sync.WaitGroup
	errs := make([]error, len(state.Selections.Selections))

	for _, idx := range indices {
		selection := state.Selections.Selections[idx]

		var need agent.SlideNeed
		if selection.OutlineIndex >= 0 && selection.OutlineIndex < len(slideNeeds) {
			need = slideNeeds[selection.OutlineIndex]
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
				start := time.Now()
				spec, usage, err := d.DesignDiagram(ctx, sn, state.TemplateInstructions, state.AgentMemories["designer"], fb...)
				if err != nil {
					errs[i] = err
					return
				}
				o.collector.Record(metrics.AgentCall{
					Agent: "designer", Model: o.config.DesignerModel,
					InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
					CacheReadInputTokens: usage.CacheReadInputTokens, CacheCreationInputTokens: usage.CacheCreationInputTokens,
					Duration: time.Since(start),
				})
				state.SetDiagramSpec(i, spec)
				state.SetSlideContent(i, agent.SlideContent{SourceSlide: -1})
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
			o.collector.Record(metrics.AgentCall{
				Agent: "writer", Model: mdl,
				InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
				CacheReadInputTokens: usage.CacheReadInputTokens, CacheCreationInputTokens: usage.CacheCreationInputTokens,
				Duration: time.Since(start),
			})
			agent.EnforceMaxChars(content, fields)
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
