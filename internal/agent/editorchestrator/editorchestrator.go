package editorchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/owulveryck/agentigslide/internal/agent"
	"github.com/owulveryck/agentigslide/internal/agent/editplanner"
	"github.com/owulveryck/agentigslide/internal/agent/editreviewer"
	"github.com/owulveryck/agentigslide/internal/agent/editwriter"
	"github.com/owulveryck/agentigslide/internal/agent/writer"
	"github.com/owulveryck/agentigslide/internal/metrics"
	"github.com/owulveryck/agentigslide/internal/model"
	"github.com/owulveryck/agentigslide/internal/vertex"
)

// EditOrchestrator coordinates the agentic edit pipeline:
// EditPlanner -> EditWriters/Writers (parallel) -> Assembler -> EditReviewer.
type EditOrchestrator struct {
	client    *vertex.Client
	config    agent.Config
	collector *metrics.Collector
	// Skeleton, when set, skips the planner step and uses this pre-built
	// skeleton directly. Use this for interactive mode where the skeleton
	// has already been refined via chat before the pipeline starts.
	Skeleton *model.EditSkeleton
	// FinalSkeleton is the enriched skeleton after Execute completes.
	// Available for the visual feedback loop to map issues to operations.
	FinalSkeleton *model.EditSkeleton
}

// New creates an EditOrchestrator with the given Vertex AI client and
// agent configuration.
func New(client *vertex.Client, cfg agent.Config) *EditOrchestrator {
	return &EditOrchestrator{client: client, config: cfg, collector: metrics.NewCollector()}
}

// Collector returns the metrics collector.
func (o *EditOrchestrator) Collector() *metrics.Collector {
	return o.collector
}

// Execute runs the full edit pipeline and returns an EditPlan ready for
// pipeline.ExecuteEditPlan().
func (o *EditOrchestrator) Execute(
	ctx context.Context,
	presentationID string,
	existingSlides []model.ExistingSlideInfo,
	userRequest string,
	compactCatalog string,
	templateInstructions string,
) (*model.EditPlan, *metrics.Collector, error) {
	pipelineStart := time.Now()
	slog.Info("[edit-pipeline] starting agentic edit")

	state := &editPipelineState{
		presentationID:       presentationID,
		existingSlides:       existingSlides,
		userRequest:          userRequest,
		compactCatalog:       compactCatalog,
		templateInstructions: templateInstructions,
	}

	// Step 1: EditPlanner
	if o.Skeleton != nil {
		slog.Info("[edit-pipeline] step 1/4: planner (using pre-built skeleton)")
		state.skeleton = o.Skeleton
	} else {
		slog.Info("[edit-pipeline] step 1/4: planner")
		if err := o.runEditPlanner(ctx, state); err != nil {
			return nil, o.collector, fmt.Errorf("editplanner: %w", err)
		}
	}

	// Enrich skeleton with current text from existing slides
	o.enrichSkeleton(state)

	// Step 2: Writers (parallel)
	slog.Info("[edit-pipeline] step 2/4: writers", "operations", len(state.skeleton.Operations), "maxParallel", o.config.MaxParallel)
	if err := o.runWriters(ctx, state); err != nil {
		return nil, o.collector, fmt.Errorf("writers: %w", err)
	}

	// Step 3: Assemble
	slog.Info("[edit-pipeline] step 3/4: assembling edit plan")
	o.assemble(state)

	// Step 4: EditReviewer (optional)
	if o.config.EditReviewEnabled {
		slog.Info("[edit-pipeline] step 4/4: reviewer")
		for attempt := 0; attempt <= o.config.MaxEditReviewRetries; attempt++ {
			if err := o.runEditReviewer(ctx, state); err != nil {
				slog.Warn("[edit-pipeline] reviewer failed", "error", err, "attempt", attempt)
				if attempt < o.config.MaxEditReviewRetries {
					continue
				}
				slog.Warn("[edit-pipeline] reviewer failed after all retries, proceeding without review")
				break
			}

			if state.reviewResult.Approved {
				break
			}

			if attempt == o.config.MaxEditReviewRetries {
				slog.Warn("[edit-pipeline] reviewer did not approve after max retries, proceeding anyway",
					"issues", len(state.reviewResult.Issues),
				)
				break
			}

			slog.Info("[edit-pipeline] review iteration: re-running affected writers",
				"issues", len(state.reviewResult.Issues),
				"attempt", attempt+1,
			)

			if err := o.handleReviewIssues(ctx, state); err != nil {
				slog.Warn("[edit-pipeline] failed to handle review issues, proceeding", "error", err)
				break
			}
			o.assemble(state)
		}
	} else {
		slog.Info("[edit-pipeline] step 4/4: reviewer (disabled)")
	}

	o.FinalSkeleton = state.skeleton

	pipelineDuration := time.Since(pipelineStart)
	o.collector.SetSlidesGenerated(len(state.editPlan.Operations))
	o.collector.SetPipelineDuration(pipelineDuration)

	slog.Info("[edit-pipeline] edit complete",
		"operations", len(state.editPlan.Operations),
		"totalDuration", pipelineDuration.Round(time.Millisecond),
	)

	return state.editPlan, o.collector, nil
}

type editPipelineState struct {
	mu sync.Mutex

	presentationID       string
	existingSlides       []model.ExistingSlideInfo
	userRequest          string
	compactCatalog       string
	templateInstructions string

	skeleton         *model.EditSkeleton
	filledOperations []model.EditOperation
	editPlan         *model.EditPlan
	reviewResult     *agent.ReviewResult
}

func (s *editPipelineState) setOperation(index int, op model.EditOperation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.filledOperations[index] = op
}

func (o *EditOrchestrator) runEditPlanner(ctx context.Context, state *editPipelineState) error {
	ep := editplanner.New(o.client, o.config.EditPlannerModel, o.config.EditPlannerMaxTokens)
	skeleton, usage, err := ep.Run(ctx, state.presentationID, state.existingSlides, state.userRequest, state.compactCatalog, state.templateInstructions)
	if err != nil {
		return err
	}
	o.collector.Record(metrics.AgentCall{
		Agent: "editplanner", Model: o.config.EditPlannerModel,
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
		CacheReadInputTokens: usage.CacheReadInputTokens, CacheCreationInputTokens: usage.CacheCreationInputTokens,
	})
	state.skeleton = skeleton
	return nil
}

// enrichSkeleton populates CurrentText in ModificationIntent from the
// existing presentation data and filters out modifications with ObjectIDs
// that don't exist in the presentation (LLM hallucinations).
func (o *EditOrchestrator) enrichSkeleton(state *editPipelineState) {
	textByObjectID := make(map[string]string)
	for _, slide := range state.existingSlides {
		for _, el := range slide.TextElements {
			textByObjectID[el.ObjectID] = el.Content
		}
	}

	for i, op := range state.skeleton.Operations {
		if op.Type != "modify_content" {
			continue
		}
		valid := state.skeleton.Operations[i].Modifications[:0]
		for j, mod := range op.Modifications {
			if _, ok := textByObjectID[mod.VariableName]; !ok {
				slog.Warn("[enrichSkeleton] dropping modification with unknown ObjectID",
					"variableName", mod.VariableName,
					"slideIndex", op.SlideIndex)
				continue
			}
			state.skeleton.Operations[i].Modifications[j].CurrentText = textByObjectID[mod.VariableName]
			valid = append(valid, state.skeleton.Operations[i].Modifications[j])
		}
		state.skeleton.Operations[i].Modifications = valid
	}
}

func (o *EditOrchestrator) runWriters(ctx context.Context, state *editPipelineState) error {
	state.filledOperations = make([]model.EditOperation, len(state.skeleton.Operations))

	sem := make(chan struct{}, o.config.MaxParallel)
	var wg sync.WaitGroup
	errs := make([]error, len(state.skeleton.Operations))

	for i, skelOp := range state.skeleton.Operations {
		switch skelOp.Type {
		case "modify_content":
			writerModel := o.config.EditWriterModel
			if len(skelOp.Modifications) <= 2 {
				writerModel = o.config.EditWriterSimpleModel
			}

			wg.Add(1)
			go func(idx int, op model.SkeletonOperation, mdl string) {
				defer wg.Done()
				if ctx.Err() != nil {
					errs[idx] = ctx.Err()
					return
				}
				sem <- struct{}{}
				defer func() { <-sem }()

				w := editwriter.New(o.client, mdl)
				mods, usage, err := w.WriteBatch(ctx, op.Modifications, state.templateInstructions)
				if err != nil {
					errs[idx] = err
					return
				}
				o.collector.Record(metrics.AgentCall{
					Agent: "editwriter", Model: mdl,
					InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
					CacheReadInputTokens: usage.CacheReadInputTokens, CacheCreationInputTokens: usage.CacheCreationInputTokens,
				})
				state.setOperation(idx, model.EditOperation{
					Type:          "modify_content",
					SlideIndex:    op.SlideIndex,
					Modifications: mods,
					Intention:     op.Intention,
					Rationale:     op.Rationale,
				})
			}(i, skelOp, writerModel)

		case "replace_slide", "insert_slide":
			templateFields := agent.ParseSlideFields(state.compactCatalog, skelOp.NewSourceSlide)
			writerModel := o.config.WriterModel
			if len(templateFields) <= 2 {
				writerModel = o.config.WriterSimpleModel
			}

			wg.Add(1)
			go func(idx int, op model.SkeletonOperation, fields []agent.TemplateField, mdl string) {
				defer wg.Done()
				if ctx.Err() != nil {
					errs[idx] = ctx.Err()
					return
				}
				sem <- struct{}{}
				defer func() { <-sem }()

				slideNeed := buildSlideNeedFromSkeleton(op)
				w := writer.New(o.client, mdl)
				content, usage, err := w.WriteSlide(ctx, op.NewSourceSlide, slideNeed, fields, state.templateInstructions)
				if err != nil {
					errs[idx] = err
					return
				}
				o.collector.Record(metrics.AgentCall{
					Agent: "writer", Model: mdl,
					InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
					CacheReadInputTokens: usage.CacheReadInputTokens, CacheCreationInputTokens: usage.CacheCreationInputTokens,
				})
				agent.EnforceMaxChars(content, fields)

				editOp := model.EditOperation{
					Type:           op.Type,
					SlideIndex:     op.SlideIndex,
					NewSourceSlide: op.NewSourceSlide,
					InsertPosition: op.InsertPosition,
					SlideContent:   content.Modifications,
					Intention:      op.Intention,
					Rationale:      op.Rationale,
				}
				state.setOperation(idx, editOp)
			}(i, skelOp, templateFields, writerModel)

		case "delete_slide":
			state.filledOperations[i] = model.EditOperation{
				Type:       "delete_slide",
				SlideIndex: skelOp.SlideIndex,
				Rationale:  skelOp.Rationale,
			}
		}
	}

	wg.Wait()

	var writerErrors []error
	for i, err := range errs {
		if err != nil {
			writerErrors = append(writerErrors, fmt.Errorf("operation %d: %w", i, err))
		}
	}
	if len(writerErrors) > 0 {
		return fmt.Errorf("writers failed: %w", errors.Join(writerErrors...))
	}

	return nil
}

func (o *EditOrchestrator) assemble(state *editPipelineState) {
	state.editPlan = &model.EditPlan{
		PresentationID: state.presentationID,
		Operations:     state.filledOperations,
	}
	slog.Info("[edit-pipeline] plan assembled", "operations", len(state.editPlan.Operations))
}

func (o *EditOrchestrator) runEditReviewer(ctx context.Context, state *editPipelineState) error {
	r := editreviewer.New(o.client, o.config.EditReviewerModel)
	result, usage, err := r.Run(ctx, state.editPlan, state.skeleton, state.existingSlides, state.userRequest, state.templateInstructions, o.config.EditReviewerThinkingBudget)
	if err != nil {
		return err
	}
	o.collector.Record(metrics.AgentCall{
		Agent: "editreviewer", Model: o.config.EditReviewerModel,
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
		CacheReadInputTokens: usage.CacheReadInputTokens, CacheCreationInputTokens: usage.CacheCreationInputTokens,
	})
	state.reviewResult = result
	return nil
}

func (o *EditOrchestrator) handleReviewIssues(ctx context.Context, state *editPipelineState) error {
	feedbackByIndex := make(map[int][]agent.ReviewIssue)
	for _, issue := range state.reviewResult.Issues {
		if issue.SlideIndex >= 0 && issue.SlideIndex < len(state.skeleton.Operations) {
			feedbackByIndex[issue.SlideIndex] = append(feedbackByIndex[issue.SlideIndex], issue)
		}
	}

	sem := make(chan struct{}, o.config.MaxParallel)
	var wg sync.WaitGroup
	errs := make([]error, len(state.skeleton.Operations))

	for idx, issues := range feedbackByIndex {
		skelOp := state.skeleton.Operations[idx]
		if skelOp.Type != "modify_content" {
			continue
		}

		writerModel := o.config.EditWriterModel
		if len(skelOp.Modifications) <= 2 {
			writerModel = o.config.EditWriterSimpleModel
		}

		enrichedMods := enrichModificationsWithFeedback(skelOp.Modifications, issues)

		wg.Add(1)
		go func(i int, mods []model.ModificationIntent, mdl string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			w := editwriter.New(o.client, mdl)
			newMods, usage, err := w.WriteBatch(ctx, mods, state.templateInstructions)
			if err != nil {
				errs[i] = err
				return
			}
			o.collector.Record(metrics.AgentCall{
				Agent: "editwriter", Model: mdl,
				InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
				CacheReadInputTokens: usage.CacheReadInputTokens, CacheCreationInputTokens: usage.CacheCreationInputTokens,
			})
			state.setOperation(i, model.EditOperation{
				Type:          "modify_content",
				SlideIndex:    state.skeleton.Operations[i].SlideIndex,
				Modifications: newMods,
				Intention:     state.skeleton.Operations[i].Intention,
				Rationale:     state.skeleton.Operations[i].Rationale,
			})
		}(idx, enrichedMods, writerModel)
	}

	wg.Wait()

	var writerErrors []error
	for i, err := range errs {
		if err != nil {
			writerErrors = append(writerErrors, fmt.Errorf("operation %d: %w", i, err))
		}
	}
	if len(writerErrors) > 0 {
		return errors.Join(writerErrors...)
	}
	return nil
}

// enrichModificationsWithFeedback appends review feedback to modification
// intentions so the writer can correct its output.
func enrichModificationsWithFeedback(mods []model.ModificationIntent, issues []agent.ReviewIssue) []model.ModificationIntent {
	result := make([]model.ModificationIntent, len(mods))
	copy(result, mods)
	for i := range result {
		var feedback []string
		for _, issue := range issues {
			if issue.Field == "" || issue.Field == result[i].VariableName {
				feedback = append(feedback, fmt.Sprintf("[%s] %s → %s", issue.IssueType, issue.Description, issue.Suggestion))
			}
		}
		if len(feedback) > 0 {
			result[i].Intention += "\n\nCORRECTIONS DEMANDÉES :\n" + joinStrings(feedback)
		}
	}
	return result
}

func joinStrings(ss []string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += "\n"
		}
		result += "- " + s
	}
	return result
}

// HandleVisualFeedback converts visual findings into corrections, re-runs
// EditWriters on affected modify_content operations, and returns the corrected
// operations ready for ReapplyModifications. Only text_overflow and
// text_truncated issues on modify_content slides are actionable.
func (o *EditOrchestrator) HandleVisualFeedback(
	ctx context.Context,
	findings []EditVisualFinding,
	pageIDToOpIndex map[string]int,
	skeleton *model.EditSkeleton,
	templateInstructions string,
) ([]model.EditOperation, error) {
	feedbackByIndex := make(map[int][]agent.ReviewIssue)
	for _, f := range findings {
		if f.Approved {
			continue
		}
		opIndex, ok := pageIDToOpIndex[f.PageID]
		if !ok || opIndex < 0 || opIndex >= len(skeleton.Operations) {
			continue
		}
		if skeleton.Operations[opIndex].Type != "modify_content" {
			continue
		}
		for _, issue := range f.Issues {
			if issue.IssueType != "text_overflow" && issue.IssueType != "text_truncated" {
				continue
			}
			feedbackByIndex[opIndex] = append(feedbackByIndex[opIndex], agent.ReviewIssue{
				SlideIndex:  skeleton.Operations[opIndex].SlideIndex,
				IssueType:   issue.IssueType,
				Description: issue.Description,
				Suggestion:  issue.Suggestion,
			})
		}
	}

	if len(feedbackByIndex) == 0 {
		return nil, nil
	}

	slog.Info("[edit-visual-feedback] re-running writers for visual issues", "affectedOps", len(feedbackByIndex))

	corrected := make([]model.EditOperation, 0, len(feedbackByIndex))
	var mu sync.Mutex
	sem := make(chan struct{}, o.config.MaxParallel)
	var wg sync.WaitGroup
	var errSlice []error

	for idx, issues := range feedbackByIndex {
		skelOp := skeleton.Operations[idx]

		writerModel := o.config.EditWriterModel
		if len(skelOp.Modifications) <= 2 {
			writerModel = o.config.EditWriterSimpleModel
		}

		enrichedMods := enrichModificationsWithFeedback(skelOp.Modifications, issues)

		wg.Add(1)
		go func(i int, mods []model.ModificationIntent, mdl string, skelOp model.SkeletonOperation) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			w := editwriter.New(o.client, mdl)
			newMods, usage, err := w.WriteBatch(ctx, mods, templateInstructions)
			if err != nil {
				mu.Lock()
				errSlice = append(errSlice, fmt.Errorf("operation %d: %w", i, err))
				mu.Unlock()
				return
			}
			o.collector.Record(metrics.AgentCall{
				Agent: "editwriter", Model: mdl,
				InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
				CacheReadInputTokens: usage.CacheReadInputTokens, CacheCreationInputTokens: usage.CacheCreationInputTokens,
			})
			op := model.EditOperation{
				Type:          "modify_content",
				SlideIndex:    skelOp.SlideIndex,
				Modifications: newMods,
				Intention:     skelOp.Intention,
				Rationale:     skelOp.Rationale,
			}
			mu.Lock()
			corrected = append(corrected, op)
			mu.Unlock()
		}(idx, enrichedMods, writerModel, skelOp)
	}

	wg.Wait()

	if len(errSlice) > 0 {
		return corrected, errors.Join(errSlice...)
	}

	return corrected, nil
}

// EditVisualFinding mirrors pipeline.EditVisualFinding to avoid a circular
// import. The caller converts between the two.
type EditVisualFinding struct {
	PageID   string
	Approved bool
	Issues   []EditVisualIssue
}

// EditVisualIssue mirrors pipeline.EditVisualIssue.
type EditVisualIssue struct {
	IssueType   string
	Description string
	Suggestion  string
}

// buildSlideNeedFromSkeleton converts a skeleton operation into a SlideNeed
// compatible with the existing Writer agent.
func buildSlideNeedFromSkeleton(op model.SkeletonOperation) agent.SlideNeed {
	items := make([]string, len(op.ContentIntents))
	for i, ci := range op.ContentIntents {
		items[i] = ci.Intention
	}
	return agent.SlideNeed{
		Intent:       op.Intention,
		ContentItems: items,
		ItemCount:    len(items),
		NeedsTitle:   true,
	}
}
