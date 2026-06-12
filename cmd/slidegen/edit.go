package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/owulveryck/agentigslide/internal/agent"
	"github.com/owulveryck/agentigslide/internal/agent/editorchestrator"
	"github.com/owulveryck/agentigslide/internal/agent/editplanner"
	"github.com/owulveryck/agentigslide/internal/agent/formatter"
	"github.com/owulveryck/agentigslide/internal/auth"
	"github.com/owulveryck/agentigslide/internal/config"
	"github.com/owulveryck/agentigslide/internal/input"
	"github.com/owulveryck/agentigslide/internal/metrics"
	"github.com/owulveryck/agentigslide/internal/model"
	"github.com/owulveryck/agentigslide/internal/pipeline"
	"github.com/owulveryck/agentigslide/internal/plan"
	"github.com/owulveryck/agentigslide/internal/revision"
	"github.com/owulveryck/agentigslide/internal/vertex"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/slides/v1"
)

// editMode reads an existing presentation, runs the EditPlanner agent to
// produce an edit plan, then applies the modifications in-place.
func editMode(presID, filePath, credFile string) error {
	vertexCfg, err := vertex.LoadConfig()
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	agentCfg, err := agent.LoadConfig()
	if err != nil {
		return fmt.Errorf("agent configuration error: %w", err)
	}

	slidesCfg, err := config.LoadSlidesConfig()
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	ctx := context.Background()

	if credFile == "" {
		credFile = slidesCfg.Credentials
	}

	slidesClient, err := auth.GetOAuthClient(ctx, credFile)
	if err != nil {
		return fmt.Errorf("failed to get authenticated client: %w", err)
	}

	slidesSrv, err := slides.NewService(ctx, option.WithHTTPClient(slidesClient))
	if err != nil {
		return fmt.Errorf("failed to create Slides service: %w", err)
	}

	driveSrv, err := drive.NewService(ctx, option.WithHTTPClient(slidesClient))
	if err != nil {
		return fmt.Errorf("failed to create Drive service: %w", err)
	}

	slidesAPI := pipeline.WrapSlides(slidesSrv)

	slog.Info("creating backup snapshot before edit")
	snapshot, snapErr := revision.CreateSnapshot(ctx, driveSrv, presID)
	if snapErr != nil {
		slog.Warn("failed to create backup snapshot, proceeding without rollback", "error", snapErr)
	} else {
		slog.Info("backup snapshot available", "url", snapshot.CopyURL)
	}

	slog.Info("reading existing presentation", "id", presID)
	existingSlides, err := pipeline.ReadPresentation(ctx, slidesAPI, presID)
	if err != nil {
		return fmt.Errorf("failed to read presentation: %w", err)
	}
	slog.Info("presentation read", "slides", len(existingSlides))

	useChat := !hasUserRequest(filePath)

	var inputReader *input.Reader
	if useChat {
		home, _ := os.UserHomeDir()
		histFile := ""
		if home != "" {
			histFile = filepath.Join(home, ".slidegen_edit_history")
		}
		var initErr error
		inputReader, initErr = input.New(input.Config{HistoryFile: histFile})
		if initErr != nil {
			return fmt.Errorf("failed to initialize input: %w", initErr)
		}
		defer inputReader.Close()
	}

	var userRequest []byte
	if hasUserRequest(filePath) {
		userRequest = readUserRequest(filePath)
	} else {
		fmt.Fprintf(os.Stderr, "Presentation has %d slides. Describe the modifications:\n", len(existingSlides))
		text, inputErr := inputReader.ReadMultiLine()
		if inputErr != nil {
			return fmt.Errorf("input error: %w", inputErr)
		}
		userRequest = []byte(text)
	}

	index, err := plan.LoadTemplateIndex(slidesCfg.EffectiveTemplateIndex())
	if err != nil {
		return fmt.Errorf("failed to load template index: %w\nPlease run 'go run cmd/buildindex/build_template_index.go' first", err)
	}

	exclusions := plan.LoadExclusions(slidesCfg.TemplateDir())
	compactIndex := plan.BuildCompactIndex(index, plan.HashSeed(string(userRequest)), exclusions)
	templateInstructions := pipeline.LoadTemplateInstructions(slidesCfg.TemplateDir())

	groundTruth := agent.BuildGroundTruth(index, plan.LoadDeckInvariants(slidesCfg.TemplateDir()))

	var agentMemories map[string]string
	if agentCfg.MemoryEnabled {
		agentMemories = pipeline.LoadValidatedAgentMemories(slidesCfg.TemplateDir(), groundTruth)
	}

	vc, err := vertex.NewClient(ctx, vertexCfg)
	if err != nil {
		return fmt.Errorf("failed to create Vertex AI client: %w", err)
	}

	orch := editorchestrator.New(vc, agentCfg)

	if useChat {
		ep := editplanner.New(vc, agentCfg.EditPlannerModel, agentCfg.EditPlannerMaxTokens)
		feedbackFn := func(skeleton *model.EditSkeleton) (string, error) {
			return inputReader.ReadFeedback(editplanner.FormatEditSkeleton(skeleton))
		}
		skeleton, usages, planErr := ep.RunInteractive(ctx, presID, existingSlides, string(userRequest), compactIndex, templateInstructions, feedbackFn, agentMemories["editplanner"])
		if planErr != nil {
			return fmt.Errorf("EditPlanner interactive failed: %w", planErr)
		}
		orch.Skeleton = skeleton
		for _, u := range usages {
			orch.Collector().Record(metrics.AgentCall{
				Agent: "editplanner", Model: agentCfg.EditPlannerModel,
				InputTokens: u.InputTokens, OutputTokens: u.OutputTokens,
				CacheReadInputTokens: u.CacheReadInputTokens, CacheCreationInputTokens: u.CacheCreationInputTokens,
			})
		}
	}

	editPlan, collector, orchErr := orch.Execute(ctx, presID, existingSlides, string(userRequest), compactIndex, templateInstructions, agentMemories)
	if orchErr != nil {
		return fmt.Errorf("edit pipeline failed: %w", orchErr)
	}

	slog.Info("edit plan produced", "operations", len(editPlan.Operations))
	for i, op := range editPlan.Operations {
		args := []any{"index", i, "type", op.Type, "slideIndex", op.SlideIndex, "rationale", op.Rationale}
		if op.Type == "insert_slide" {
			args = append(args, "insertPosition", op.InsertPosition)
		}
		slog.Info("  operation", args...)
	}

	summary := collector.Summary()
	metrics.PrintTable(os.Stderr, summary)
	if histErr := metrics.AppendHistory(summary, "edit"); histErr != nil {
		slog.Warn("failed to write metrics history", "error", histErr)
	}

	editResult, revLog, err := pipeline.ExecuteEditPlan(ctx, editPlan, slidesAPI, slidesCfg.TemplateID, index)
	if err != nil {
		return fmt.Errorf("failed to execute edit plan: %w", err)
	}

	slog.Info("edit result", "affectedSlides", len(editResult.AffectedPageIDs))

	if len(editResult.AffectedPageIDs) > 0 {
		runEditPostProcessing(ctx, vc, agentCfg, slidesAPI, slidesSrv, driveSrv, orch, presID, editResult, revLog, templateInstructions)
	}

	url := fmt.Sprintf("https://docs.google.com/presentation/d/%s/edit", presID)
	fmt.Println(url)
	if snapshot != nil {
		slog.Info("backup snapshot available for rollback", "url", snapshot.CopyURL)
	}

	if agentCfg.MemoryEnabled {
		runMemorySynthesis(&agentResult{
			issueLog:    orch.IssueLog(),
			vc:          vc,
			agentCfg:    agentCfg,
			templateDir: slidesCfg.TemplateDir(),
			groundTruth: groundTruth,
		})
	}

	return nil
}

func runEditPostProcessing(ctx context.Context, vc *vertex.Client, agentCfg agent.Config, slidesAPI pipeline.SlidesAPI, slidesSrv *slides.Service, driveSrv *drive.Service, orch *editorchestrator.EditOrchestrator, presID string, editResult *pipeline.EditResult, revLog *revision.Log, templateInstructions string) {
	if agentCfg.EditFormatterEnabled {
		slog.Info("running formatter on modified slides")
		f := formatter.New(slidesSrv)
		result, fmtErr := f.RunForPages(ctx, presID, editResult.AffectedPageIDs, revLog)
		if fmtErr != nil {
			slog.Warn("formatter failed", "error", fmtErr)
		} else {
			slog.Info("formatter completed", "issues", len(result.Issues), "applied", result.AppliedCount)
		}
	}

	if agentCfg.EditVisualReviewEnabled {
		for attempt := 0; attempt <= agentCfg.MaxEditVisualRetries; attempt++ {
			slog.Info("running visual review on modified slides", "attempt", attempt+1)
			findings := pipeline.VisualReviewEditedSlides(ctx, vc, agentCfg.EditVisualReviewModel, slidesAPI, presID, editResult.AffectedPageIDs, agentCfg.MaxParallel)

			for _, f := range findings {
				if !f.Approved {
					for _, issue := range f.Issues {
						slog.Warn("[agent:visual-reviewer] issue", "pageID", f.PageID, "type", issue.IssueType, "description", issue.Description, "suggestion", issue.Suggestion)
					}
				}
			}

			if attempt >= agentCfg.MaxEditVisualRetries {
				var remaining int
				for _, f := range findings {
					if !f.Approved {
						remaining += len(f.Issues)
					}
				}
				if remaining > 0 {
					slog.Warn("[agent:visual-reviewer] max retries reached, proceeding with visual issues",
						"remainingIssues", remaining,
						"maxRetries", agentCfg.MaxEditVisualRetries,
					)
				}
				break
			}

			orchFindings := convertFindings(findings)
			correctedOps, fbErr := orch.HandleVisualFeedback(ctx, orchFindings, editResult.PageIDToOpIndex, orch.FinalSkeleton, templateInstructions)
			if fbErr != nil {
				slog.Warn("visual feedback failed", "error", fbErr)
			}
			if len(correctedOps) == 0 {
				break
			}

			slog.Info("re-applying corrected modifications", "ops", len(correctedOps))
			if reErr := pipeline.ReapplyModifications(ctx, presID, correctedOps, slidesAPI, revLog); reErr != nil {
				slog.Warn("re-apply failed", "error", reErr)
				break
			}
		}
	}
}

func convertFindings(pf []pipeline.EditVisualFinding) []editorchestrator.EditVisualFinding {
	out := make([]editorchestrator.EditVisualFinding, len(pf))
	for i, f := range pf {
		issues := make([]editorchestrator.EditVisualIssue, len(f.Issues))
		for j, iss := range f.Issues {
			issues[j] = editorchestrator.EditVisualIssue{
				IssueType:   iss.IssueType,
				Description: iss.Description,
				Suggestion:  iss.Suggestion,
			}
		}
		out[i] = editorchestrator.EditVisualFinding{
			PageID:   f.PageID,
			Approved: f.Approved,
			Issues:   issues,
		}
	}
	return out
}
