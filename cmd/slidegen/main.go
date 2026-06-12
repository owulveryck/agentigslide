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
	"github.com/owulveryck/agentigslide/internal/escalation"
	"github.com/owulveryck/agentigslide/internal/metrics"
	"github.com/owulveryck/agentigslide/internal/model"
	"github.com/owulveryck/agentigslide/internal/monitor"
	"github.com/owulveryck/agentigslide/internal/pipeline"
	"github.com/owulveryck/agentigslide/internal/plan"
	"github.com/owulveryck/agentigslide/internal/revision"
	"github.com/owulveryck/agentigslide/internal/trace"
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
	traceFile      = flag.String("trace", "", "Path for debug trace JSON output (captures full pipeline data flow for diagnostics)")
)

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage:
  slidegen                                              Interactive chat (default)
  slidegen --file <request.md>                          Generate from file (skips chat)
  slidegen --file <request.md> --trace trace.json       Generate with debug trace
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

	tracer := trace.New(*traceFile)
	defer func() {
		if err := tracer.Flush(); err != nil {
			slog.Warn("failed to write trace file", "error", err)
		} else if tracer != nil {
			slog.Info("trace file written", "path", *traceFile)
		}
	}()

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
		ar = agentMode(*filePath, useWeb, useChat, sgCfg.WebAddr, tracer)
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

	phaseDone := func(name string, start time.Time) {
		tracer.RecordPhase(name, start)
		if collector != nil {
			collector.AddPhaseDuration(name, time.Since(start))
		}
	}

	execStart := time.Now()
	presId, revLog, pageIDs, mon, err := executePresentation(presPlan, *credentials, mon, tracer)
	phaseDone("execution", execStart)
	if err != nil {
		fatalWithPlanDump(presPlan, mon, "%v", err)
	}

	url := fmt.Sprintf("https://docs.google.com/presentation/d/%s/edit", presId)

	if mon != nil {
		mon.SendURL(url)
	}

	var issueLog *agent.IssueLog
	if ar != nil {
		issueLog = &ar.issueLog
	}

	fmtStart := time.Now()
	runFormatter(presId, *credentials, revLog, tracer, 1, issueLog)
	phaseDone("formatter-1", fmtStart)

	var escCol *escalation.Collector
	if ar != nil {
		escCol = ar.escCollector
	}

	vrStart := time.Now()
	runVisualReview(presId, *credentials, pageIDs, presPlan, revLog, tracer, issueLog, collector, escCol)
	phaseDone("visual-review", vrStart)

	fmtStart = time.Now()
	runFormatter(presId, *credentials, revLog, tracer, 2, issueLog)
	phaseDone("formatter-2", fmtStart)

	fmt.Println(url)

	// Memory synthesis (an LLM call worth ~1 min on the v3 trace) overlaps
	// with the consolidated human acknowledgement below instead of blocking
	// the end of the run (ADR 032). The application step — which may need a
	// human decision — only happens after the acknowledgement, so the two
	// never compete for stdin.
	var memCh chan map[string]string
	var memStart time.Time
	if ar != nil && ar.agentCfg.MemoryEnabled {
		memStart = time.Now()
		memCh = make(chan map[string]string, 1)
		go func() { memCh <- synthesizeMemoryProposals(ar) }()
	}

	// Single end-of-run acknowledgement of every advisory constat collected
	// along the pipeline (unconverged loops, unresolved visual findings…).
	escCol.Flush()

	if memCh != nil {
		select {
		case proposals := <-memCh:
			applyMemoryProposals(ar, proposals)
		case <-time.After(2 * time.Minute):
			slog.Warn("memory synthesis still running after 2 minutes, abandoning")
		}
		tracer.RecordPhase("memory-synthesis", memStart)
		if collector != nil {
			collector.AddPhaseDuration("memory-synthesis", time.Since(memStart))
		}
	}

	if collector != nil {
		summary := collector.Summary()
		metrics.PrintTable(os.Stderr, summary)
		if err := metrics.AppendHistory(summary, "generate"); err != nil {
			slog.Warn("failed to write metrics history", "error", err)
		}
		// Dump the complete per-call LLM ledger into the trace: this is the
		// authoritative data for offline cost computation (traceeval), and it
		// covers the agents whose tokens are not in the per-phase trace
		// sections (visual review, memory synthesis, designer).
		tracer.SetAgentCalls(convertAgentCalls(collector.Calls()))
	}

	if *summaryFlag && presPlan != nil {
		runSummary(sgCfg, presPlan)
	}

	return nil
}

func convertAgentCalls(calls []metrics.AgentCall) []trace.AgentCallTrace {
	out := make([]trace.AgentCallTrace, 0, len(calls))
	for _, c := range calls {
		out = append(out, trace.AgentCallTrace{
			Agent:            c.Agent,
			Model:            c.Model,
			InputTokens:      c.InputTokens,
			OutputTokens:     c.OutputTokens,
			CacheReadTokens:  c.CacheReadInputTokens,
			CacheWriteTokens: c.CacheCreationInputTokens,
			DurationMs:       c.Duration.Milliseconds(),
		})
	}
	return out
}

func executePresentation(presPlan *model.PresentationPlan, credFlag string, mon *monitor.Monitor, tracer *trace.Tracer) (string, *revision.Log, []string, *monitor.Monitor, error) {
	slidesCfg, err := config.LoadSlidesConfig()
	if err != nil {
		return "", nil, nil, mon, fmt.Errorf("configuration error: %w", err)
	}

	credFile := credFlag
	if credFile == "" {
		credFile = slidesCfg.Credentials
	}

	ctx := context.Background()
	if agentCfg, cfgErr := agent.LoadConfig(); cfgErr == nil {
		var cancel context.CancelFunc
		ctx, cancel = agent.PhaseContext(ctx, agentCfg.ExecutionTimeout)
		defer cancel()
	}
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

	execResult, revLog, err := pipeline.ExecutePlan(ctx, presPlan, pipeline.WrapSlides(slidesSrv), pipeline.WrapDrive(driveSrv), pipeline.WithExecTracer(tracer))
	if err != nil {
		return "", nil, nil, mon, fmt.Errorf("failed to execute plan: %w", err)
	}

	return execResult.PresentationID, revLog, execResult.PageIDs, mon, nil
}

func runFormatter(presId, credentials string, revLog *revision.Log, tracer *trace.Tracer, pass int, issueLog *agent.IssueLog) {
	agentCfg, err := agent.LoadConfig()
	if err != nil || !agentCfg.FormatterEnabled {
		if err != nil {
			slog.Warn("agent config error, skipping formatter", "error", err)
		}
		return
	}

	ctx, cancel := agent.PhaseContext(context.Background(), agentCfg.FormatterTimeout)
	defer cancel()
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
	runStart := time.Now()
	result, fmtErr := f.Run(ctx, presId, revLog)
	if fmtErr != nil {
		slog.Warn("formatter failed", "error", fmtErr)
		tracer.RecordError("formatter", fmtErr.Error())
		return
	}
	slog.Info("formatter completed", "issues", len(result.Issues), "applied", result.AppliedCount)
	ft := convertFormatterResult(result, pass)
	ft.DurationMs = time.Since(runStart).Milliseconds()
	tracer.RecordFormatter(ft)

	// Formatter corrections are symptoms of upstream style drift: feed them
	// to the issue log so memory synthesis can teach the writers to avoid
	// the recurring ones.
	if issueLog != nil && len(result.Issues) > 0 {
		issues := make([]agent.ReviewIssue, 0, len(result.Issues))
		for _, fi := range result.Issues {
			issues = append(issues, agent.ReviewIssue{
				SlideIndex:  fi.SlideIndex,
				Field:       fi.ObjectID,
				IssueType:   "formatting_" + fi.Rule,
				Description: fmt.Sprintf("attendu %s, trouvé %s (sévérité %s)", fi.Expected, fi.Actual, fi.Severity),
			})
		}
		issueLog.Record("formatter", pass, issues)
	}
}

func runVisualReview(presId, credentials string, pageIDs []string, plan *model.PresentationPlan, revLog *revision.Log, tracer *trace.Tracer, issueLog *agent.IssueLog, collector *metrics.Collector, escCol *escalation.Collector) {
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

	ctx, cancel := agent.PhaseContext(context.Background(), agentCfg.VisualReviewTimeout)
	defer cancel()
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

	reviewablePageIDs := filterReviewablePageIDs(pageIDs, plan)
	if len(reviewablePageIDs) == 0 {
		slog.Info("[agent:visual-reviewer] no reviewable slides (all low-field), skipping")
		return
	}
	if skipped := len(pageIDs) - len(reviewablePageIDs); skipped > 0 {
		slog.Info("[agent:visual-reviewer] skipping low-field slides", "skipped", skipped, "reviewing", len(reviewablePageIDs))
	}

	currentReviewIDs := reviewablePageIDs

	// pageID → template slide number, for routing template-geometry findings
	// into the learned caveats of the template index (ADR 031).
	sourceByPageID := make(map[string]int)
	if plan != nil {
		dupIdx := 0
		for _, spec := range plan.Slides {
			if spec.Diagram != nil {
				continue
			}
			if dupIdx >= len(pageIDs) {
				break
			}
			sourceByPageID[pageIDs[dupIdx]] = spec.SourceSlideNumber
			dupIdx++
		}
	}

	tracker := agent.NewConvergenceTracker()

	for attempt := 0; attempt <= agentCfg.MaxVisualRetries; attempt++ {
		slog.Info("[agent:visual-reviewer] starting", "attempt", attempt+1, "slides", len(currentReviewIDs))
		attemptStart := time.Now()
		findings := pipeline.VisualReviewEditedSlides(ctx, vc, agentCfg.VisualReviewModel, slidesAPI, presId, currentReviewIDs, agentCfg.MaxParallel)
		recordVisualReviewUsage(collector, findings)

		for _, f := range findings {
			if !f.Approved {
				for _, issue := range f.Issues {
					slog.Warn("[agent:visual-reviewer] issue", "pageID", f.PageID, "type", issue.IssueType, "description", issue.Description, "suggestion", issue.Suggestion)
				}
			}
		}

		vt := convertVisualFindings(findings, attempt)
		vt.DurationMs = time.Since(attemptStart).Milliseconds()
		tracer.RecordVisualReview(vt)

		// Classification by actuator (ADR 031): only content-fixable issues
		// go back to the correction loop. Template-geometry findings are
		// knowledge about the template, not about this deck: they are
		// persisted as learned caveats so the selector avoids (and the
		// reviewer anticipates) these zones on future runs.
		fixableFindings, geometryCount := splitVisualFindings(findings, sourceByPageID, slidesCfg.TemplateDir())
		if geometryCount > 0 {
			slog.Info("[agent:visual-reviewer] template-geometry findings persisted as learned caveats", "count", geometryCount)
		}

		// Convergence contract: stop as soon as a correction pass stops
		// making strict progress — re-reviewing the same defects only burns
		// vision tokens (passes 13→11→10 on the v3 trace).
		for _, f := range findings {
			if f.Approved {
				continue
			}
			for _, issue := range f.Issues {
				tracker.Observe(agent.VisualFingerprint(f.PageID, issue.IssueType))
			}
		}
		tracker.EndPass()
		if !tracker.StrictProgress() {
			resolved, repeated, fresh := tracker.PassStats()
			slog.Warn("[agent:visual-reviewer] no strict progress, stopping the visual loop",
				"resolved", resolved, "repeated", repeated, "new", fresh,
			)
			reportUnresolvedVisualFindings(findings, agentCfg.MaxVisualRetries, issueLog, escCol)
			break
		}

		if attempt >= agentCfg.MaxVisualRetries {
			reportUnresolvedVisualFindings(findings, agentCfg.MaxVisualRetries, issueLog, escCol)
			break
		}

		skeleton := buildSkeletonFromPlan(plan, pageIDs, presId)
		pageIDToOpIndex := make(map[string]int, len(pageIDs))
		for i, pid := range pageIDs {
			pageIDToOpIndex[pid] = i
		}

		orch := editorchestrator.New(vc, agentCfg)
		orchFindings := convertFindings(fixableFindings)
		correctedOps, fbErr := orch.HandleVisualFeedback(ctx, orchFindings, pageIDToOpIndex, skeleton, templateInstructions)
		if fbErr != nil {
			slog.Warn("visual feedback failed", "error", fbErr)
		}
		if len(correctedOps) == 0 {
			// Nothing the writers can fix (e.g. only misalignment/font
			// issues): surface what remains instead of dropping it silently.
			reportUnresolvedVisualFindings(findings, agentCfg.MaxVisualRetries, issueLog, escCol)
			break
		}

		// Re-review exactly the slides whose modifications were corrected,
		// derived from the corrected operations themselves rather than from
		// a duplicated issue-type filter.
		pageIDByPlanIndex := buildPlanIndexToPageID(plan, pageIDs)
		correctedSet := make(map[string]bool, len(correctedOps))
		for _, op := range correctedOps {
			if pid, ok := pageIDByPlanIndex[op.SlideIndex]; ok {
				correctedSet[pid] = true
			}
		}

		slog.Info("[agent:visual-reviewer] re-applying corrected modifications", "ops", len(correctedOps))
		if reErr := pipeline.ReapplyModifications(ctx, presId, correctedOps, slidesAPI, revLog); reErr != nil {
			slog.Warn("re-apply failed", "error", reErr)
			break
		}

		currentReviewIDs = make([]string, 0, len(correctedSet))
		for pid := range correctedSet {
			currentReviewIDs = append(currentReviewIDs, pid)
		}
		if len(currentReviewIDs) == 0 {
			break
		}
		slog.Info("[agent:visual-reviewer] scoping retry to corrected slides only", "slides", len(currentReviewIDs))
	}
}

// splitVisualFindings classifies each finding's issues by actuator
// (ADR 031): content-fixable issues are returned for the correction loop;
// template-geometry issues are persisted as learned caveats on the source
// template slide; subjective issues are dropped from the correction loop
// (they remain in the findings used for the final acknowledgement report).
// Returns the findings reduced to their fixable issues, and the number of
// geometry caveats persisted.
func splitVisualFindings(findings []pipeline.EditVisualFinding, sourceByPageID map[string]int, templateDir string) (fixable []pipeline.EditVisualFinding, geometryCount int) {
	for _, f := range findings {
		if f.Approved {
			fixable = append(fixable, f)
			continue
		}
		reduced := f
		reduced.Issues = nil
		for _, issue := range f.Issues {
			switch agent.ClassifyVisualIssue(issue.IssueType, issue.Description) {
			case agent.VisualFixable:
				reduced.Issues = append(reduced.Issues, issue)
			case agent.VisualTemplateGeometry:
				geometryCount++
				caveat := "Géométrie contraignante observée en visual review : " + truncateForCaveat(issue.Description)
				if err := plan.AppendLearnedCaveat(templateDir, sourceByPageID[f.PageID], caveat); err != nil {
					slog.Warn("failed to persist learned caveat", "pageID", f.PageID, "error", err)
				}
			case agent.VisualSubjective:
				slog.Info("[agent:visual-reviewer] subjective finding acknowledged, not retried",
					"pageID", f.PageID, "type", issue.IssueType)
			}
		}
		if len(reduced.Issues) == 0 {
			// No fixable content issue left on this slide.
			reduced.Approved = true
		}
		fixable = append(fixable, reduced)
	}
	return fixable, geometryCount
}

func truncateForCaveat(s string) string {
	const maxLen = 140
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen]) + "…"
}

// recordVisualReviewUsage feeds the per-slide visual review API usage into
// the metrics collector so the vision calls (the costliest blind spot of the
// trace) appear in the per-call ledger like any other agent call.
func recordVisualReviewUsage(collector *metrics.Collector, findings []pipeline.EditVisualFinding) {
	if collector == nil {
		return
	}
	for _, f := range findings {
		if f.Model == "" {
			continue // thumbnail or API failure: no LLM call happened
		}
		collector.Record(metrics.AgentCall{
			Agent:                    "visual-reviewer",
			Model:                    f.Model,
			InputTokens:              f.Usage.InputTokens,
			OutputTokens:             f.Usage.OutputTokens,
			CacheReadInputTokens:     f.Usage.CacheReadInputTokens,
			CacheCreationInputTokens: f.Usage.CacheCreationInputTokens,
			Duration:                 time.Duration(f.ReviewMs) * time.Millisecond,
		})
	}
}

// buildPlanIndexToPageID maps each non-diagram plan slide index to its
// generated pageID, mirroring the iteration order of buildSkeletonFromPlan.
func buildPlanIndexToPageID(plan *model.PresentationPlan, pageIDs []string) map[int]string {
	m := make(map[int]string)
	if plan == nil {
		return m
	}
	dupIdx := 0
	for i, spec := range plan.Slides {
		if spec.Diagram != nil {
			continue
		}
		if dupIdx >= len(pageIDs) {
			break
		}
		m[i] = pageIDs[dupIdx]
		dupIdx++
	}
	return m
}

// reportUnresolvedVisualFindings prints a one-screen summary of the visual
// defects still present when the review loop exits, and records them in the
// issue log so memory synthesis and the escalation policy can act on them.
// Nothing is ever shipped silently degraded.
func reportUnresolvedVisualFindings(findings []pipeline.EditVisualFinding, maxRetries int, issueLog *agent.IssueLog, escCol *escalation.Collector) {
	var unresolved []agent.ReviewIssue
	var b strings.Builder
	for _, f := range findings {
		if f.Approved {
			continue
		}
		for _, issue := range f.Issues {
			unresolved = append(unresolved, agent.ReviewIssue{
				IssueType:   issue.IssueType,
				Field:       f.PageID,
				Description: issue.Description,
				Suggestion:  issue.Suggestion,
			})
			fmt.Fprintf(&b, "  - [%s] slide %s : %s\n", issue.IssueType, f.PageID, issue.Description)
			if issue.Suggestion != "" {
				fmt.Fprintf(&b, "      → %s\n", issue.Suggestion)
			}
		}
	}
	if len(unresolved) == 0 {
		return
	}
	slog.Warn("[agent:visual-reviewer] unresolved visual issues after final pass",
		"remainingIssues", len(unresolved),
		"maxRetries", maxRetries,
	)
	if issueLog != nil {
		issueLog.Record("visual-reviewer", maxRetries, unresolved)
	}
	req := escalation.Request{
		Reason:   fmt.Sprintf("%d défaut(s) visuel(s) non résolu(s) après la passe finale", len(unresolved)),
		Details:  b.String(),
		Question: "Acquitter ces défauts (la présentation est déjà créée) ?",
		Default:  true,
	}
	if escCol != nil {
		// Consolidated end-of-run acknowledgement (ADR 032).
		escCol.Add(req)
		return
	}
	escalation.Ask(req)
}

// filterReviewablePageIDs excludes pageIDs for slides with very few editable
// fields (e.g. section dividers, covers) that produce false-positive visual
// review findings (empty_field on decorative zones).
func filterReviewablePageIDs(pageIDs []string, plan *model.PresentationPlan) []string {
	if plan == nil {
		return pageIDs
	}
	const minFields = 3
	var result []string
	dupIdx := 0
	for _, spec := range plan.Slides {
		if spec.Diagram != nil {
			continue
		}
		if dupIdx >= len(pageIDs) {
			break
		}
		editableCount := 0
		for _, obj := range spec.EditableObjects {
			if obj.Modified {
				editableCount++
			}
		}
		if editableCount >= minFields {
			result = append(result, pageIDs[dupIdx])
		} else {
			slog.Debug("[visual-review] skipping low-field slide",
				"pageID", pageIDs[dupIdx],
				"editableFields", editableCount,
			)
		}
		dupIdx++
	}
	return result
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

// runMemorySynthesis is the synchronous path (edit mode): synthesize then
// apply. Generation mode runs the two halves separately so the LLM call
// overlaps with the end-of-run acknowledgement.
func runMemorySynthesis(ar *agentResult) {
	applyMemoryProposals(ar, synthesizeMemoryProposals(ar))
}

// synthesizeMemoryProposals runs the memory synthesis LLM call and returns
// the raw proposals (nil when there is nothing to learn or the call failed).
// Safe to run concurrently with the rest of the shutdown sequence: it only
// reads the issue log and records into the (goroutine-safe) collector.
func synthesizeMemoryProposals(ar *agentResult) map[string]string {
	if !ar.issueLog.HasIssues() {
		slog.Info("no issues detected during pipeline, skipping memory synthesis")
		return nil
	}

	ctx := context.Background()
	existingMemories := pipeline.LoadAllAgentMemories(ar.templateDir)

	slog.Info("synthesizing agent memory from pipeline issues")
	synthStart := time.Now()
	proposals, usage, err := agent.SynthesizeMemory(ctx, ar.vc, ar.agentCfg.MemoryModel, ar.issueLog, existingMemories)
	if ar.collector != nil && (usage.InputTokens > 0 || usage.OutputTokens > 0) {
		ar.collector.Record(metrics.AgentCall{
			Agent:                    "memory-synthesis",
			Model:                    ar.agentCfg.MemoryModel,
			InputTokens:              usage.InputTokens,
			OutputTokens:             usage.OutputTokens,
			CacheReadInputTokens:     usage.CacheReadInputTokens,
			CacheCreationInputTokens: usage.CacheCreationInputTokens,
			Duration:                 time.Since(synthStart),
		})
	}
	if err != nil {
		slog.Warn("memory synthesis failed", "error", err)
		return nil
	}
	return proposals
}

// applyMemoryProposals validates, classifies and writes the synthesized
// guidelines. May solicit the human for litigious updates — call it only
// after any other stdin interaction is done.
func applyMemoryProposals(ar *agentResult, proposals map[string]string) {
	if len(proposals) == 0 {
		slog.Info("no memory updates proposed")
		return
	}

	// Ground-truth validation (ADR 028): a synthesized rule referencing
	// slides that do not exist, or asserting deck structure, is rejected
	// before any classification — learned memory never outranks the catalog
	// or the configuration. This is the gate that would have blocked the
	// poisoned "SLIDE 325 est un fantôme" rule of edito-trace-v3.
	validated, rejected := agent.ValidateMemories(proposals, ar.groundTruth)
	for _, r := range rejected {
		slog.Warn("[memory-validation] rejecting synthesized rule contradicting ground truth",
			"agent", r.Agent,
			"reason", r.Reason,
			"rule", r.Line,
		)
	}
	proposals = validated
	if len(proposals) == 0 {
		slog.Info("all memory proposals rejected by ground-truth validation")
		return
	}

	// Litigiousness classification: additive proposals derived from ordinary
	// pipeline events are applied automatically (the MEMORY.md files are
	// versioned in git, so any bad guideline is revertable); only deletions,
	// rewrites of existing guidelines, or proposals tied to a sanitized run
	// require a human decision.
	autoApply := make(map[string]string)
	litigious := make(map[string]string)
	existing := pipeline.LoadAllAgentMemories(ar.templateDir)
	for agentName, proposed := range proposals {
		if agent.IsAdditiveUpdate(existing[agentName], proposed) && !ar.issueLog.HasLitigiousIssues(agentName) {
			autoApply[agentName] = proposed
		} else {
			litigious[agentName] = proposed
		}
	}

	if len(autoApply) > 0 {
		if err := agent.WriteMemoryFiles(ar.templateDir, autoApply); err != nil {
			slog.Warn("failed to write memory files", "error", err)
		} else {
			for agentName := range autoApply {
				slog.Info("agent memory updated automatically (additive)", "agent", agentName)
			}
		}
	}

	if len(litigious) == 0 {
		return
	}

	approved := escalation.Ask(escalation.Request{
		Reason:   "mise à jour mémoire litigieuse (suppression/réécriture de guidelines, ou run dégradé)",
		Details:  agent.FormatMemoryProposals(litigious),
		Question: "Écrire ces guidelines dans le répertoire template ?",
		Default:  false,
		Timeout:  2 * time.Minute,
	})
	if !approved {
		slog.Info("litigious memory update declined by user")
		return
	}

	if err := agent.WriteMemoryFiles(ar.templateDir, litigious); err != nil {
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

func convertFormatterResult(result *formatter.FormatterResult, pass int) trace.FormatterTrace {
	ft := trace.FormatterTrace{
		Pass:         pass,
		IssueCount:   len(result.Issues),
		AppliedCount: result.AppliedCount,
	}
	for _, issue := range result.Issues {
		ft.Issues = append(ft.Issues, trace.FormatterIssueTrace{
			Rule:       issue.Rule,
			SlideIndex: issue.SlideIndex,
			ObjectID:   issue.ObjectID,
			Expected:   issue.Expected,
			Actual:     issue.Actual,
			Severity:   issue.Severity,
		})
	}
	for _, c := range result.Corrections {
		ft.Corrections = append(ft.Corrections, trace.FormatterCorrectionTrace{
			ObjectID:   c.ObjectID,
			SlideIndex: c.SlideIndex,
			Type:       c.Type,
			Reason:     c.Reason,
		})
	}
	return ft
}

func convertVisualFindings(findings []pipeline.EditVisualFinding, attempt int) trace.VisualReviewTrace {
	vt := trace.VisualReviewTrace{Attempt: attempt}
	for _, f := range findings {
		finding := trace.VisualFindingTrace{
			PageID:           f.PageID,
			Approved:         f.Approved,
			ThumbnailFetchMs: f.ThumbnailFetchMs,
			ReviewMs:         f.ReviewMs,
		}
		for _, issue := range f.Issues {
			finding.Issues = append(finding.Issues, trace.VisualIssueTrace{
				IssueType:   issue.IssueType,
				Description: issue.Description,
				Suggestion:  issue.Suggestion,
			})
		}
		vt.Findings = append(vt.Findings, finding)
	}
	var corrections int
	for _, f := range findings {
		if !f.Approved {
			corrections += len(f.Issues)
		}
	}
	vt.Corrections = corrections
	return vt
}
