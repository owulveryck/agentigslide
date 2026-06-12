package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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
	escalate  func(reason, details string, def bool) bool
	notify    func(reason, details string)
	// Outline, when set, skips the outliner step and uses this pre-built
	// outline directly. Use this for interactive mode where the outline has
	// already been refined via chat before the pipeline starts.
	Outline *agent.PresentationOutline
	// Invariants holds the deck-level structural rules declared by the
	// template configuration (COVER_SLIDE, CLOSING_SLIDE, SUMMARY_SLIDE
	// files). They are applied by construction: the cover template is
	// forced on cover-typed needs (or prefixed when the outline has none)
	// and the closing slide is appended last (ADR 029).
	Invariants agent.DeckInvariants
}

// Option configures optional Orchestrator behavior.
type Option func(*Orchestrator)

// WithTracer attaches a debug tracer to the pipeline.
func WithTracer(t *trace.Tracer) Option {
	return func(o *Orchestrator) { o.tracer = t }
}

// WithEscalation installs the human-in-the-loop callback invoked on
// litigious events (ADR 026). The callback receives the reason, a summary,
// and the default decision; it returns whether to proceed. When nil, the
// default decision is applied silently.
func WithEscalation(fn func(reason, details string, def bool) bool) Option {
	return func(o *Orchestrator) { o.escalate = fn }
}

// WithNotification installs the advisory-constat callback (ADR 032): events
// the pipeline reports but proceeds through regardless (stale issues,
// non-converging loops). Unlike WithEscalation, the notification never blocks
// and has no return value — the typical sink is the end-of-run consolidated
// acknowledgement (escalation.Collector).
func WithNotification(fn func(reason, details string)) Option {
	return func(o *Orchestrator) { o.notify = fn }
}

// decide runs the escalation callback, or applies the default when none is
// installed.
func (o *Orchestrator) decide(reason, details string, def bool) bool {
	if o.escalate == nil {
		return def
	}
	return o.escalate(reason, details, def)
}

// report sends an advisory constat to the notification sink, falling back to
// the blocking escalation callback when none is installed (pre-ADR 032
// behavior).
func (o *Orchestrator) report(reason, details string) {
	if o.notify != nil {
		o.notify(reason, details)
		return
	}
	o.decide(reason, details, true)
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

	phaseDone := func(name string, start time.Time) {
		o.tracer.RecordPhase(name, start)
		o.collector.AddPhaseDuration(name, time.Since(start))
	}

	phaseStart := time.Now()
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
	phaseDone("outline", phaseStart)

	phaseStart = time.Now()
	slog.Info("[pipeline] step 2/5: selector")
	var selectorRetries int
	var selectorErr error
	var selIssues []agent.SelectionIssue
	var selectionSanitized bool
	for attempt := 0; attempt <= o.config.MaxSelectorRetries; attempt++ {
		attemptStart := time.Now()
		var selectorUsage vertex.Usage
		var err error
		// A partial retry repairs only the failed entries; it is possible
		// when the previous plan had the right cardinality (per-entry issues
		// only, no global/count error).
		if attempt > 0 && len(selIssues) > 0 {
			selectorUsage, err = o.runSelectorPartial(ctx, state, selIssues)
			if err != nil {
				slog.Warn("[pipeline] selector partial retry failed, falling back to full retry", "error", err)
				selectorUsage, err = o.runSelector(ctx, state, selectorErr.Error())
			}
		} else {
			var validationErrStr string
			if selectorErr != nil {
				validationErrStr = selectorErr.Error()
			}
			selectorUsage, err = o.runSelector(ctx, state, validationErrStr)
		}
		if err != nil {
			return nil, o.collector, fmt.Errorf("selector: %w", err)
		}

		selIssues, selectorErr = agent.ValidateSelectionDetailed(state.Selections, state.Outline, state.CompactCatalog)
		if selectorErr == nil && len(selIssues) > 0 {
			errs := make([]string, len(selIssues))
			for i, issue := range selIssues {
				errs[i] = fmt.Sprintf("selection %d: %s", issue.SelectionIndex, issue.Reason)
			}
			selectorErr = fmt.Errorf("selection validation failed:\n  %s", strings.Join(errs, "\n  "))
		}
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
			dropped := agent.SanitizeSelection(state.Selections, state.Outline, state.CompactCatalog)
			selectionSanitized = true
			slog.Warn("[pipeline] selector validation failed after max retries, sanitized and proceeding",
				"issues", selectorErr,
				"droppedEntries", dropped,
			)
			// A sanitized selection is a silent degradation of the plan:
			// record it as its own issue type so memory synthesis learns
			// from it and the escalation policy can surface it to a human.
			o.issueLog.Record("selector", attempt, []agent.ReviewIssue{{
				IssueType: "sanitized_selection",
				Description: fmt.Sprintf(
					"selector output still invalid after %d attempts; %d entries dropped or replaced deterministically: %v",
					attempt+1, dropped, selectorErr),
				Suggestion: "Review the generated deck: sanitized slides may not match the outline intent",
			}})
			if !o.decide("sélection sanitizée",
				fmt.Sprintf("%d entrée(s) supprimée(s)/remplacée(s) après %d tentatives invalides :\n%v", dropped, attempt+1, selectorErr),
				true) {
				return nil, o.collector, fmt.Errorf("selector: sanitized selection rejected by user")
			}
			break
		}

		selectorRetries++
		slog.Warn("[pipeline] selector validation failed, retrying",
			"attempt", attempt+1,
			"error", selectorErr,
			"partialRetryPossible", len(selIssues) > 0,
		)
	}
	o.collector.SetSelectorRetries(selectorRetries)
	o.enforceDeckInvariants(state)
	o.traceSelectionResult(state)
	phaseDone("selection", phaseStart)

	phaseStart = time.Now()
	slog.Info("[pipeline] step 3/5: writers", "count", len(state.Selections.Selections), "maxParallel", o.config.MaxParallel)
	if err := o.runWriters(ctx, state); err != nil {
		return nil, o.collector, fmt.Errorf("writers: %w", err)
	}
	phaseDone("writers", phaseStart)

	phaseStart = time.Now()
	slog.Info("[pipeline] step 4/5: assembling plan")
	o.assemble(state)

	preReviewIssues := agent.PreReviewValidation(state.AssembledPlan, state.CompactCatalog, o.Invariants)
	if len(preReviewIssues) > 0 {
		slog.Warn("[pipeline] pre-review gate found deterministic issues", "count", len(preReviewIssues))
		for _, issue := range preReviewIssues {
			slog.Warn("[pipeline] pre-review issue",
				"slide", issue.SlideIndex,
				"type", issue.IssueType,
				"description", issue.Description,
			)
		}
		o.issueLog.Record("pre-review", 0, preReviewIssues)
		state.ReviewResult = &agent.ReviewResult{Issues: preReviewIssues}
		corrected, err := o.handleReviewIssuesReturn(ctx, state)
		if err != nil {
			slog.Warn("[pipeline] pre-review correction failed", "error", err)
		} else if len(corrected) > 0 {
			o.assemble(state)
			slog.Info("[pipeline] re-assembled plan after pre-review corrections", "correctedSlides", len(corrected))
		}
	}

	phaseDone("pre-review", phaseStart)

	// Multi-speed review, gates-governed (ADR 030): when the deterministic
	// gates are clean (no pre-review issues, no sanitized selection), the
	// review is now purely semantic — the cheap model handles it regardless
	// of deck size. The expensive model is reserved for escalation: dirty
	// gates, sanitized selection, force flag, or a correction loop that
	// stops making progress.
	reviewModel := o.config.ReviewerModel
	reviewThinking := o.config.ReviewerThinkingBudget
	gatesClean := len(preReviewIssues) == 0 && !selectionSanitized
	if !o.config.ReviewerForceOpus && gatesClean && o.config.ReviewerSubsetModel != "" {
		reviewModel = o.config.ReviewerSubsetModel
		reviewThinking = 0
		slog.Info("[pipeline] deterministic gates clean, using cheap-tier semantic review",
			"model", reviewModel,
			"deckSize", len(state.AssembledPlan.Slides),
		)
	}

	phaseStart = time.Now()
	slog.Info("[pipeline] step 5/5: reviewer", "model", reviewModel)
	var reviewerRetries int
	var lastCorrectedIndices []int
	type issueKey struct {
		slideIndex int
		field      string
		issueType  string
	}
	staleCount := make(map[issueKey]int)
	// escalateModel flips to true when a correction pass fails to make
	// strict progress: the next review pass then runs on the expensive
	// model with thinking, as a second opinion (ADR 030).
	escalateModel := false
	// reviewTracker implements the convergence contract (ADR 031): the loop
	// stops as soon as a correction pass stops making strict progress.
	reviewTracker := agent.NewConvergenceTracker()
	for attempt := 0; attempt <= o.config.MaxReviewRetries; attempt++ {
		attemptStart := time.Now()
		var reviewUsage vertex.Usage
		var reviewErr error
		if attempt == 0 || state.ReviewResult == nil {
			reviewUsage, reviewErr = o.runReviewer(ctx, state, reviewModel, reviewThinking)
		} else {
			subsetModel := o.config.ReviewerSubsetModel
			subsetThinking := 0
			if escalateModel || o.config.ReviewerForceOpus || !gatesClean {
				subsetModel = o.config.ReviewerModel
				subsetThinking = o.config.ReviewerThinkingBudget
				slog.Info("[pipeline] escalating review pass to expensive model", "model", subsetModel)
			}
			reviewUsage, reviewErr = o.runReviewerSubset(ctx, state, lastCorrectedIndices, subsetModel, subsetThinking)
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

		// Cross-check every computable finding against ground truth before
		// engaging any correction (ADR 030): a judge hallucination must never
		// trigger a rewrite, let alone poison the memory.
		kept, droppedIssues := agent.CrossCheckReviewIssues(
			state.ReviewResult.Issues, state.AssembledPlan,
			agent.ParseCatalog(state.CompactCatalog), o.Invariants)
		if len(droppedIssues) > 0 {
			falsePositives := make([]agent.ReviewIssue, 0, len(droppedIssues))
			for _, d := range droppedIssues {
				slog.Warn("[pipeline] dropping reviewer false positive",
					"slide", d.Issue.SlideIndex,
					"issueType", d.Issue.IssueType,
					"reason", d.Reason,
				)
				falsePositives = append(falsePositives, agent.ReviewIssue{
					SlideIndex:  d.Issue.SlideIndex,
					Field:       d.Issue.Field,
					IssueType:   "reviewer_false_positive",
					Description: fmt.Sprintf("[%s] %s — dropped: %s", d.Issue.IssueType, d.Issue.Description, d.Reason),
				})
			}
			o.issueLog.Record("reviewer", attempt, falsePositives)
			state.ReviewResult.Issues = kept
			if !state.ReviewResult.Approved && len(kept) == 0 {
				slog.Info("[pipeline] all reviewer findings were false positives, treating as approved")
				state.ReviewResult.Approved = true
			}
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
		for _, d := range droppedIssues {
			ri.DroppedIssues = append(ri.DroppedIssues, trace.ReviewIssueTrace{
				SlideIndex:  d.Issue.SlideIndex,
				Field:       d.Issue.Field,
				IssueType:   d.Issue.IssueType,
				Description: d.Issue.Description,
				Suggestion:  d.Reason,
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

		// Convergence contract (ADR 031): compare this pass's fingerprints
		// to the previous one; without strict progress (at least one issue
		// resolved, no more new than resolved), another rewrite is noise —
		// surface and stop instead of degrading content further.
		for _, issue := range state.ReviewResult.Issues {
			reviewTracker.Observe(agent.IssueFingerprint(issue))
		}
		reviewTracker.EndPass()
		if !reviewTracker.StrictProgress() {
			resolved, repeated, fresh := reviewTracker.PassStats()
			var b strings.Builder
			for _, issue := range state.ReviewResult.Issues {
				fmt.Fprintf(&b, "  - slide %d [%s] %s\n", issue.SlideIndex, issue.IssueType, issue.Description)
			}
			slog.Warn("[pipeline] review loop no longer making strict progress, stopping",
				"resolved", resolved, "repeated", repeated, "new", fresh,
			)
			o.report("la boucle de revue ne progresse plus (issues répétées sans résolution)", b.String())
			break
		}

		var freshIssues []agent.ReviewIssue
		var staleIssues []agent.ReviewIssue
		escalateModel = false
		for _, issue := range state.ReviewResult.Issues {
			key := issueKey{slideIndex: issue.SlideIndex, field: issue.Field, issueType: issue.IssueType}
			staleCount[key]++
			if staleCount[key] >= 2 {
				// A repeated fingerprint means the previous correction did
				// not resolve it: get a second opinion from the expensive
				// model on the next pass.
				escalateModel = true
			}
			if staleCount[key] >= 3 {
				slog.Warn("[pipeline] stale issue (seen 3+ times), excluding from rewrite",
					"slide", issue.SlideIndex,
					"field", issue.Field,
					"issueType", issue.IssueType,
				)
				staleIssues = append(staleIssues, issue)
				continue
			}
			freshIssues = append(freshIssues, issue)
		}
		state.ReviewResult.Issues = freshIssues
		if len(staleIssues) > 0 {
			var b strings.Builder
			for _, issue := range staleIssues {
				fmt.Fprintf(&b, "  - slide %d [%s] %s\n", issue.SlideIndex, issue.IssueType, issue.Description)
			}
			// Stale issues are by definition unfixable by another rewrite:
			// surface them in the end-of-run acknowledgement.
			o.report("issues de revue persistantes (3+ itérations)", b.String())
		}

		slog.Info("[pipeline] review iteration: re-running affected writers",
			"issues", len(freshIssues),
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
	phaseDone("review", phaseStart)

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

// runSelectorPartial re-asks the selector only for the entries that failed
// validation and merges the corrections into the current selection plan.
func (o *Orchestrator) runSelectorPartial(ctx context.Context, state *agent.PipelineState, issues []agent.SelectionIssue) (vertex.Usage, error) {
	ag := selector.New(o.client, o.config.SelectorModel)
	start := time.Now()
	usage, err := ag.RunPartial(ctx, state.Outline, state.CompactCatalog, state.TemplateInstructions, state.AgentMemories["selector"], state.Selections, issues)
	if err != nil {
		return usage, err
	}
	o.collector.Record(metrics.AgentCall{
		Agent: "selector", Model: o.config.SelectorModel,
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
		CacheReadInputTokens: usage.CacheReadInputTokens, CacheCreationInputTokens: usage.CacheCreationInputTokens,
		Duration: time.Since(start),
	})
	return usage, nil
}

func (o *Orchestrator) runWriters(ctx context.Context, state *agent.PipelineState) error {
	indices := make([]int, len(state.Selections.Selections))
	for i := range indices {
		indices[i] = i
	}
	return o.writeSlides(ctx, state, indices, nil)
}

// enforceDeckInvariants applies the configured deck structure on the
// validated selection, by construction rather than by review (ADR 029):
// every cover-typed need is forced onto the official cover template, so the
// writer fills its fields with the outline's cover content and the reviewer
// has nothing structural left to judge.
func (o *Orchestrator) enforceDeckInvariants(state *agent.PipelineState) {
	if o.Invariants.CoverSlide <= 0 || state.Selections == nil {
		return
	}
	needs := agent.FlattenNeeds(state.Outline)
	for i := range state.Selections.Selections {
		sel := &state.Selections.Selections[i]
		if sel.OutlineIndex < 0 || sel.OutlineIndex >= len(needs) {
			continue
		}
		if needs[sel.OutlineIndex].SlideType == "cover" && sel.SourceSlide != o.Invariants.CoverSlide {
			slog.Info("[pipeline] enforcing official cover template on cover-typed need",
				"selection", i,
				"was", sel.SourceSlide,
				"now", o.Invariants.CoverSlide,
			)
			sel.SourceSlide = o.Invariants.CoverSlide
		}
	}
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

	// Deck invariants by construction (ADR 029): when the outline produced
	// no cover-typed need, prefix the official cover with the presentation
	// title in its main title field.
	if o.Invariants.CoverSlide > 0 &&
		(len(plan.Slides) == 0 || plan.Slides[0].SourceSlide != o.Invariants.CoverSlide) {
		cover := model.SlideRequest{SourceSlide: o.Invariants.CoverSlide}
		if titleField := mainTitleField(agent.ParseSlideFields(state.CompactCatalog, o.Invariants.CoverSlide)); titleField != "" {
			cover.Modifications = []model.TextModification{{
				VariableName: titleField,
				NewText:      state.Outline.PresentationTitle,
			}}
		} else {
			slog.Warn("assembler: cover template has no identifiable title field in catalog, adding bare cover",
				"coverSlide", o.Invariants.CoverSlide,
			)
		}
		plan.Slides = append([]model.SlideRequest{cover}, plan.Slides...)
		slog.Info("assembler: prefixed official cover slide", "sourceSlide", o.Invariants.CoverSlide)
	}

	if o.Invariants.ClosingSlide > 0 {
		plan.Slides = append(plan.Slides, model.SlideRequest{
			SourceSlide: o.Invariants.ClosingSlide,
		})
	}

	state.AssembledPlan = plan
	slog.Info("assembler: plan assembled", "slides", len(plan.Slides))
}

// mainTitleField returns the variable name of the main title field among the
// given template fields, or "" when none can be identified.
func mainTitleField(fields []agent.TemplateField) string {
	for _, f := range fields {
		vn := strings.ToLower(f.VariableName)
		if strings.Contains(vn, "maintitle") || strings.Contains(vn, "titlemain") {
			return f.VariableName
		}
	}
	for _, f := range fields {
		if strings.HasPrefix(f.Role, "titre") {
			return f.VariableName
		}
	}
	return ""
}

func (o *Orchestrator) runReviewer(ctx context.Context, state *agent.PipelineState, model string, thinkingBudget int) (vertex.Usage, error) {
	ag := reviewer.New(o.client, model)
	start := time.Now()
	result, usage, err := ag.Run(ctx, state.AssembledPlan, state.UserRequest, state.CompactCatalog, state.TemplateInstructions, thinkingBudget, state.AgentMemories["reviewer"])
	if err != nil {
		return usage, err
	}
	o.collector.Record(metrics.AgentCall{
		Agent: "reviewer", Model: model,
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
			oldSource := state.Selections.Selections[issue.SlideIndex].SourceSlide
			if newSlide, ok := agent.ParseTemplateSuggestion(issue.Suggestion); ok {
				catalog := agent.ParseCatalog(state.CompactCatalog)
				if catalog.SlideNumbers[newSlide] {
					slog.Info("[pipeline] swapping template per reviewer suggestion",
						"slide", issue.SlideIndex,
						"oldSource", oldSource,
						"newSource", newSlide,
					)
					state.Selections.Selections[issue.SlideIndex].SourceSlide = newSlide
					feedbackByIndex[issue.SlideIndex] = append(feedbackByIndex[issue.SlideIndex], issue)
					continue
				}
				slog.Warn("[pipeline] reviewer suggested non-existent template, skipping",
					"slide", issue.SlideIndex,
					"suggested", newSlide,
				)
			} else {
				slog.Warn("[pipeline] wrong_template with no parsable suggestion, skipping",
					"slide", issue.SlideIndex,
					"sourceSlide", oldSource,
					"suggestion", issue.Suggestion,
				)
			}
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

func (o *Orchestrator) runReviewerSubset(ctx context.Context, state *agent.PipelineState, correctedIndices []int, subsetModel string, thinkingBudget int) (vertex.Usage, error) {
	ag := reviewer.New(o.client, subsetModel)
	start := time.Now()
	result, usage, err := ag.RunSubset(ctx, state.AssembledPlan, correctedIndices, state.ReviewResult.Issues, state.UserRequest, state.CompactCatalog, state.TemplateInstructions, thinkingBudget, state.AgentMemories["reviewer"])
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
	// Truncations that survive the shorten re-ask are quality defects worth
	// learning from; collected per goroutine, recorded after the barrier
	// (IssueLog is not goroutine-safe).
	truncations := make([][]agent.ReviewIssue, len(state.Selections.Selections))

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

			// Hard truncation with an ellipsis is exactly the defect class
			// the visual review keeps finding downstream. Before resorting
			// to it, give the writer one targeted chance to shorten the
			// over-limit fields itself.
			if overruns := agent.OverLimitFields(content, fields); len(overruns) > 0 {
				shortened, retryUsage, retryDur, retryErr := o.reaskShorter(ctx, w, sourceSlide, sn, fields, state, fb, overruns)
				o.collector.Record(metrics.AgentCall{
					Agent: "writer", Model: mdl,
					InputTokens: retryUsage.InputTokens, OutputTokens: retryUsage.OutputTokens,
					CacheReadInputTokens: retryUsage.CacheReadInputTokens, CacheCreationInputTokens: retryUsage.CacheCreationInputTokens,
					Duration: retryDur,
				})
				if retryErr != nil {
					slog.Warn("[pipeline] shorten re-ask failed, falling back to hard truncation",
						"slide", i, "error", retryErr)
				} else if len(agent.OverLimitFields(shortened, fields)) < len(overruns) {
					content = shortened
					usage = retryUsage
					writerDuration += retryDur
				}
			}

			wt := o.buildWriterTrace(i, sourceSlide, sn, fields, mdl, writerDuration, usage, content, fb)
			if remaining := agent.OverLimitFields(content, fields); len(remaining) > 0 {
				for _, ov := range remaining {
					truncations[i] = append(truncations[i], agent.ReviewIssue{
						SlideIndex: i,
						Field:      ov.VariableName,
						IssueType:  "truncated_text",
						Description: fmt.Sprintf("texte de %d caractères tronqué à %d malgré la relance de raccourcissement (sourceSlide %d)",
							ov.Length, ov.Limit, sourceSlide),
					})
				}
			}
			agent.EnforceMaxChars(content, fields)
			o.recordEnforcement(&wt, content, fields)
			o.tracer.RecordWriter(wt)

			state.SetSlideContent(i, *content)
		}(idx, selection.SourceSlide, need, templateFields, writerModel, feedback)
	}

	wg.Wait()

	for i, issues := range truncations {
		if len(issues) > 0 {
			o.issueLog.Record("writer", i, issues)
		}
	}

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

// reaskShorter asks the writer to shorten the fields that exceed their hard
// character limit, injecting one overflow feedback entry per field.
func (o *Orchestrator) reaskShorter(ctx context.Context, w *writer.Agent, sourceSlide int, sn agent.SlideNeed, fields []agent.TemplateField, state *agent.PipelineState, fb []agent.ReviewIssue, overruns []agent.FieldOverrun) (*agent.SlideContent, vertex.Usage, time.Duration, error) {
	feedback := append([]agent.ReviewIssue{}, fb...)
	for _, ov := range overruns {
		feedback = append(feedback, agent.ReviewIssue{
			IssueType: "overflow",
			Field:     ov.VariableName,
			Description: fmt.Sprintf("le texte fait %d caractères mais la zone n'en accepte que %d — il serait tronqué",
				ov.Length, ov.Limit),
			Suggestion: fmt.Sprintf("Reformule ce champ en %d caractères maximum, en phrase complète (pas d'ellipse)", ov.Limit),
		})
	}
	slog.Info("[pipeline] re-asking writer to shorten over-limit fields",
		"sourceSlide", sourceSlide, "fields", len(overruns))
	start := time.Now()
	content, usage, err := w.WriteSlide(ctx, sourceSlide, sn, fields, state.TemplateInstructions, state.AgentMemories["writer"], feedback...)
	return content, usage, time.Since(start), err
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
