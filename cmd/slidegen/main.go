// Command slidegen generates Google Slides presentations from a user request
// using a multi-agent pipeline (Outliner/Selector/Writers/Reviewer). By default
// it starts in interactive chat mode where the user refines the outline before
// generation. When a file is provided (--file or piped stdin), the pipeline
// runs directly without interactive refinement.
//
// Usage:
//
//	go run cmd/slidegen/main.go                                    # interactive chat
//	go run cmd/slidegen/main.go --file request.md                  # direct generation
//	go run cmd/slidegen/main.go --plan saved-plan.json             # recovery
//	go run cmd/slidegen/main.go --plan saved-plan.json --file a.md # amend
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/owulveryck/agentigslide/internal/agent"
	"github.com/owulveryck/agentigslide/internal/agent/editorchestrator"
	"github.com/owulveryck/agentigslide/internal/agent/formatter"
	"github.com/owulveryck/agentigslide/internal/auth"
	"github.com/owulveryck/agentigslide/internal/config"
	"github.com/owulveryck/agentigslide/internal/metrics"
	"github.com/owulveryck/agentigslide/internal/model"
	"github.com/owulveryck/agentigslide/internal/monitor"
	"github.com/owulveryck/agentigslide/internal/pipeline"
	"github.com/owulveryck/agentigslide/internal/revision"
	"github.com/owulveryck/agentigslide/internal/vertex"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/slides/v1"
)

type slidegenConfig struct {
	Model        string `envconfig:"MODEL" default:"claude-opus-4-6" desc:"Claude model (used for --plan amend mode)"`
	WebAddr      string `envconfig:"WEB_ADDR" default:":9090" desc:"Address for the web dashboard (used with --web)"`
	SummaryModel string `envconfig:"SUMMARY_MODEL" default:"claude-haiku-4-5@20251001" desc:"Claude model for --summary (fast/cheap)"`
}

var (
	filePath       = flag.String("file", "", "Path to markdown file with the presentation request (reads stdin if omitted and stdin is a pipe)")
	credentials    = flag.String("credentials", "", "Path to OAuth2 client credentials JSON (optional; uses ADC if omitted)")
	dumpPrompt     = flag.Bool("dump", false, "Print the prompt that would be sent to Claude and exit (amend mode only)")
	planPath       = flag.String("plan", "", "Path to a previously saved plan JSON for recovery or amendment (use - for stdin)")
	presentationID = flag.String("presentation", "", "ID of an existing presentation to modify (edit mode)")
	webFlag        = flag.Bool("web", false, "Start a web dashboard to visualize the agent pipeline; file can be uploaded via the UI")
	summaryFlag    = flag.Bool("summary", false, "Generate a human-readable summary of the presentation via LLM after pipeline completion")
	costHistory    = flag.Int("cost-history", 0, "Show the last N runs from the cost history and exit")
)

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage:
  slidegen                                              Interactive chat (default)
  slidegen --file <request.md>                          Generate from file (skips chat)
  cat request.md | slidegen                             Generate from stdin (skips chat)
  slidegen --web                                        Web dashboard (upload file via UI)
  slidegen --plan <plan.json>                           Retry from a saved plan
  slidegen --plan <plan.json> --file <amendments.md>    Amend an existing plan
  slidegen --presentation <ID>                          Edit existing presentation (chat)
  slidegen --presentation <ID> --file <edits.md>        Edit existing presentation from file

Options:
`)
	flag.PrintDefaults()
	config.PrintAllUsage(
		struct {
			Prefix string
			Spec   any
		}{"SLIDES", &config.SlidesConfig{}},
		struct {
			Prefix string
			Spec   any
		}{"VERTEX", &vertex.Config{}},
		struct {
			Prefix string
			Spec   any
		}{"SLIDEGEN", &slidegenConfig{}},
		struct {
			Prefix string
			Spec   any
		}{"AGENT", &agent.Config{}},
		struct {
			Prefix string
			Spec   any
		}{"AGENT (Formatter)", &struct {
			FormatterEnabled     bool `envconfig:"FORMATTER_ENABLED" default:"true" desc:"Enable the Formatter agent"`
			EditFormatterEnabled bool `envconfig:"EDIT_FORMATTER_ENABLED" default:"true" desc:"Enable Formatter on edited slides"`
		}{}},
	)
}

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	config.SetupLogging()
	flag.Usage = printUsage
	flag.Parse()

	if *costHistory > 0 {
		return metrics.PrintHistory(os.Stderr, *costHistory)
	}

	var presPlan *model.PresentationPlan
	var mon *monitor.Monitor
	var collector *metrics.Collector
	var ar *agentResult

	var sgCfg slidegenConfig
	if err := envconfig.Process("SLIDEGEN", &sgCfg); err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}
	useWeb := *webFlag
	useChat := !hasUserRequest(*filePath) && !useWeb

	if *presentationID != "" && *planPath != "" {
		return fmt.Errorf("--presentation and --plan are mutually exclusive")
	}

	if *presentationID != "" {
		return editMode(*presentationID, *filePath, *credentials)
	}

	switch {
	case *planPath != "" && !hasUserRequest(*filePath):
		presPlan = loadPlanFromFile(*planPath)
		slog.Info("plan loaded", "title", presPlan.PresentationTitle, "slides", len(presPlan.Slides))

	case *planPath != "":
		presPlan = amendMode(*planPath, *filePath, *dumpPrompt)

	default:
		ar = agentMode(*filePath, useWeb, useChat, sgCfg.WebAddr)
		presPlan = ar.plan
		mon = ar.monitor
		collector = ar.collector
		defer func() {
			if mon != nil {
				slog.Info("pipeline complete, dashboard remains available - press Ctrl+C to exit")
				sig := make(chan os.Signal, 1)
				signal.Notify(sig, os.Interrupt)
				select {
				case <-sig:
				case <-time.After(5 * time.Minute):
				}
			}
		}()
	}

	presId, revLog, pageIDs, mon, err := executePresentation(presPlan, *credentials, mon)
	if err != nil {
		fatalWithPlanDump(presPlan, mon, "%v", err)
	}

	url := fmt.Sprintf("https://docs.google.com/presentation/d/%s/edit", presId)

	if mon != nil {
		mon.SendURL(url)
	}

	runFormatter(presId, *credentials, revLog)

	runVisualReview(presId, *credentials, pageIDs, presPlan, revLog)

	runFormatter(presId, *credentials, revLog)

	fmt.Println(url)

	if collector != nil {
		summary := collector.Summary()
		metrics.PrintTable(os.Stderr, summary)
		if err := metrics.AppendHistory(summary, "generate"); err != nil {
			slog.Warn("failed to write metrics history", "error", err)
		}
	}

	if ar != nil && ar.agentCfg.MemoryEnabled {
		runMemorySynthesis(ar)
	}

	if *summaryFlag && presPlan != nil {
		runSummary(sgCfg, presPlan)
	}

	return nil
}

func executePresentation(presPlan *model.PresentationPlan, credFlag string, mon *monitor.Monitor) (string, *revision.Log, []string, *monitor.Monitor, error) {
	slidesCfg, err := config.LoadSlidesConfig()
	if err != nil {
		return "", nil, nil, mon, fmt.Errorf("configuration error: %w", err)
	}

	credFile := credFlag
	if credFile == "" {
		credFile = slidesCfg.Credentials
	}

	ctx := context.Background()
	slidesClient, err := auth.GetOAuthClient(ctx, credFile)
	if err != nil {
		return "", nil, nil, mon, fmt.Errorf("failed to get authenticated client: %w", err)
	}

	slidesSrv, err := slides.NewService(ctx, option.WithHTTPClient(slidesClient))
	if err != nil {
		return "", nil, nil, mon, fmt.Errorf("failed to create Slides service: %w", err)
	}

	driveSrv, err := drive.NewService(ctx, option.WithHTTPClient(slidesClient))
	if err != nil {
		return "", nil, nil, mon, fmt.Errorf("failed to create Drive service: %w", err)
	}

	execResult, revLog, err := pipeline.ExecutePlan(ctx, presPlan, pipeline.WrapSlides(slidesSrv), pipeline.WrapDrive(driveSrv))
	if err != nil {
		return "", nil, nil, mon, fmt.Errorf("failed to execute plan: %w", err)
	}

	return execResult.PresentationID, revLog, execResult.PageIDs, mon, nil
}

func runFormatter(presId, credentials string, revLog *revision.Log) {
	agentCfg, err := agent.LoadConfig()
	if err != nil || !agentCfg.FormatterEnabled {
		if err != nil {
			slog.Warn("agent config error, skipping formatter", "error", err)
		}
		return
	}

	ctx := context.Background()
	slidesCfg, err := config.LoadSlidesConfig()
	if err != nil {
		slog.Warn("slides config error, skipping formatter", "error", err)
		return
	}
	credFile := credentials
	if credFile == "" {
		credFile = slidesCfg.Credentials
	}
	slidesClient, err := auth.GetOAuthClient(ctx, credFile)
	if err != nil {
		slog.Warn("auth error, skipping formatter", "error", err)
		return
	}
	slidesSrv, err := slides.NewService(ctx, option.WithHTTPClient(slidesClient))
	if err != nil {
		slog.Warn("slides service error, skipping formatter", "error", err)
		return
	}

	slog.Info("running formatter on generated presentation")
	f := formatter.New(slidesSrv)
	result, fmtErr := f.Run(ctx, presId, revLog)
	if fmtErr != nil {
		slog.Warn("formatter failed", "error", fmtErr)
		return
	}
	slog.Info("formatter completed", "issues", len(result.Issues), "applied", result.AppliedCount)
}

func runVisualReview(presId, credentials string, pageIDs []string, plan *model.PresentationPlan, revLog *revision.Log) {
	if len(pageIDs) == 0 {
		return
	}

	agentCfg, err := agent.LoadConfig()
	if err != nil || !agentCfg.VisualReviewEnabled {
		if err != nil {
			slog.Warn("agent config error, skipping visual review", "error", err)
		}
		return
	}

	vertexCfg, err := vertex.LoadConfig()
	if err != nil {
		slog.Warn("vertex config error, skipping visual review", "error", err)
		return
	}

	ctx := context.Background()
	vc, err := vertex.NewClient(ctx, vertexCfg)
	if err != nil {
		slog.Warn("vertex client error, skipping visual review", "error", err)
		return
	}

	slidesCfg, err := config.LoadSlidesConfig()
	if err != nil {
		slog.Warn("slides config error, skipping visual review", "error", err)
		return
	}
	credFile := credentials
	if credFile == "" {
		credFile = slidesCfg.Credentials
	}
	slidesClient, err := auth.GetOAuthClient(ctx, credFile)
	if err != nil {
		slog.Warn("auth error, skipping visual review", "error", err)
		return
	}
	slidesSrv, err := slides.NewService(ctx, option.WithHTTPClient(slidesClient))
	if err != nil {
		slog.Warn("slides service error, skipping visual review", "error", err)
		return
	}

	slidesAPI := pipeline.WrapSlides(slidesSrv)
	templateInstructions := pipeline.LoadTemplateInstructions(slidesCfg.TemplateDir())

	for attempt := 0; attempt <= agentCfg.MaxVisualRetries; attempt++ {
		slog.Info("[agent:visual-reviewer] starting", "attempt", attempt+1, "slides", len(pageIDs))
		findings := pipeline.VisualReviewEditedSlides(ctx, vc, agentCfg.VisualReviewModel, slidesAPI, presId, pageIDs, agentCfg.MaxParallel)

		for _, f := range findings {
			if !f.Approved {
				for _, issue := range f.Issues {
					slog.Warn("[agent:visual-reviewer] issue", "pageID", f.PageID, "type", issue.IssueType, "description", issue.Description, "suggestion", issue.Suggestion)
				}
			}
		}

		if attempt >= agentCfg.MaxVisualRetries {
			var remaining int
			for _, f := range findings {
				if !f.Approved {
					remaining += len(f.Issues)
				}
			}
			if remaining > 0 {
				slog.Warn("[agent:visual-reviewer] max retries reached, proceeding with visual issues",
					"remainingIssues", remaining,
					"maxRetries", agentCfg.MaxVisualRetries,
				)
			}
			break
		}

		skeleton := buildSkeletonFromPlan(plan, pageIDs, presId)
		pageIDToOpIndex := make(map[string]int, len(pageIDs))
		for i, pid := range pageIDs {
			pageIDToOpIndex[pid] = i
		}

		orch := editorchestrator.New(vc, agentCfg)
		orchFindings := convertFindings(findings)
		correctedOps, fbErr := orch.HandleVisualFeedback(ctx, orchFindings, pageIDToOpIndex, skeleton, templateInstructions)
		if fbErr != nil {
			slog.Warn("visual feedback failed", "error", fbErr)
		}
		if len(correctedOps) == 0 {
			break
		}

		slog.Info("[agent:visual-reviewer] re-applying corrected modifications", "ops", len(correctedOps))
		if reErr := pipeline.ReapplyModifications(ctx, presId, correctedOps, slidesAPI, revLog); reErr != nil {
			slog.Warn("re-apply failed", "error", reErr)
			break
		}
	}
}

func buildSkeletonFromPlan(plan *model.PresentationPlan, pageIDs []string, presId string) *model.EditSkeleton {
	skeleton := &model.EditSkeleton{
		PresentationID: presId,
	}

	dupIdx := 0
	for i, spec := range plan.Slides {
		if spec.Diagram != nil {
			continue
		}
		if dupIdx >= len(pageIDs) {
			break
		}
		pageID := pageIDs[dupIdx]
		dupIdx++

		prefix := strings.TrimSuffix(pageID, spec.SourceSlideID)

		// Group by actual element ID to avoid duplicate VariableNames
		// (template 13 has multiple fields sharing the same ObjectID).
		modTexts := make(map[string][2][]string) // actualId → [texts, intentions]
		var modOrder []string
		for _, obj := range spec.EditableObjects {
			if !obj.Modified || obj.NewValue == nil || obj.ObjectID == "" {
				continue
			}
			actualId := prefix + obj.ObjectID
			entry := modTexts[actualId]
			if entry[0] == nil {
				modOrder = append(modOrder, actualId)
			}
			entry[0] = append(entry[0], *obj.NewValue)
			entry[1] = append(entry[1], obj.Description)
			modTexts[actualId] = entry
		}
		var mods []model.ModificationIntent
		for _, actualId := range modOrder {
			entry := modTexts[actualId]
			mods = append(mods, model.ModificationIntent{
				VariableName: actualId,
				CurrentText:  strings.Join(entry[0], "\n"),
				Intention:    strings.Join(entry[1], "\n"),
			})
		}

		op := model.SkeletonOperation{
			Type:          "modify_content",
			SlideIndex:    i,
			Modifications: mods,
			Intention:     spec.Intention,
			Rationale:     spec.Description,
		}
		skeleton.Operations = append(skeleton.Operations, op)
	}

	return skeleton
}

func runMemorySynthesis(ar *agentResult) {
	if !ar.issueLog.HasIssues() {
		slog.Info("no issues detected during pipeline, skipping memory synthesis")
		return
	}

	ctx := context.Background()
	existingMemories := pipeline.LoadAllAgentMemories(ar.templateDir)

	slog.Info("synthesizing agent memory from pipeline issues")
	proposals, err := agent.SynthesizeMemory(ctx, ar.vc, ar.agentCfg.MemoryModel, ar.issueLog, existingMemories)
	if err != nil {
		slog.Warn("memory synthesis failed", "error", err)
		return
	}

	if len(proposals) == 0 {
		slog.Info("no memory updates proposed")
		return
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprint(os.Stderr, agent.FormatMemoryProposals(proposals))

	fmt.Fprint(os.Stderr, "Écrire ces guidelines dans le répertoire template ? [o/N] ")
	var answer string
	_, _ = fmt.Scanln(&answer)
	if answer != "o" && answer != "O" && answer != "oui" {
		slog.Info("memory update declined by user")
		return
	}

	if err := agent.WriteMemoryFiles(ar.templateDir, proposals); err != nil {
		slog.Warn("failed to write memory files", "error", err)
		return
	}
	slog.Info("agent memory updated successfully")
}

func runSummary(sgCfg slidegenConfig, presPlan *model.PresentationPlan) {
	ctx := context.Background()
	vertexCfg, vErr := vertex.LoadConfig()
	if vErr != nil {
		slog.Warn("summary: failed to load vertex config", "error", vErr)
		return
	}
	vc, vcErr := vertex.NewClient(ctx, vertexCfg)
	if vcErr != nil {
		slog.Warn("summary: failed to create vertex client", "error", vcErr)
		return
	}
	summaryText, sErr := generatePresentationSummary(ctx, vc, sgCfg.SummaryModel, presPlan, string(readUserRequestOrEmpty(*filePath)))
	if sErr != nil {
		slog.Warn("summary generation failed", "error", sErr)
		return
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, summaryText)
}
