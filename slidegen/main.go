// Command slidegen generates Google Slides presentations from a user request
// using a multi-agent pipeline (Outliner/Selector/Writers/Reviewer). By default
// it starts in interactive chat mode where the user refines the outline before
// generation. When a file is provided (--file or piped stdin), the pipeline
// runs directly without interactive refinement.
//
// Usage:
//
//	go run slidegen/main.go                                    # interactive chat
//	go run slidegen/main.go --file request.md                  # direct generation
//	go run slidegen/main.go --plan saved-plan.json             # recovery
//	go run slidegen/main.go --plan saved-plan.json --file a.md # amend
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/owulveryck/agentigslide/internal/agent"
	"github.com/owulveryck/agentigslide/internal/agent/editplanner"
	"github.com/owulveryck/agentigslide/internal/agent/orchestrator"
	"github.com/owulveryck/agentigslide/internal/agent/outliner"
	"github.com/owulveryck/agentigslide/internal/auth"
	"github.com/owulveryck/agentigslide/internal/config"
	"github.com/owulveryck/agentigslide/internal/fixfonts"
	"github.com/owulveryck/agentigslide/internal/input"
	"github.com/owulveryck/agentigslide/internal/metrics"
	"github.com/owulveryck/agentigslide/internal/model"
	"github.com/owulveryck/agentigslide/internal/monitor"
	"github.com/owulveryck/agentigslide/internal/pipeline"
	"github.com/owulveryck/agentigslide/internal/plan"
	"github.com/owulveryck/agentigslide/internal/vertex"

	"github.com/kelseyhightower/envconfig"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/slides/v1"
)

type slidegenConfig struct {
	Model        string `envconfig:"MODEL" default:"claude-opus-4-6" desc:"Claude model (used for --plan amend mode)"`
	WebAddr      string `envconfig:"WEB_ADDR" default:":9090" desc:"Address for the web dashboard (used with --web)"`
	SummaryModel string `envconfig:"SUMMARY_MODEL" default:"claude-haiku-4-5@20251001" desc:"Claude model for --summary (fast/cheap)"`
}

func main() {
	filePath := flag.String("file", "", "Path to markdown file with the presentation request (reads stdin if omitted and stdin is a pipe)")
	credentials := flag.String("credentials", "", "Path to OAuth2 client credentials JSON (optional; uses ADC if omitted)")
	dumpPrompt := flag.Bool("dump", false, "Print the prompt that would be sent to Claude and exit (amend mode only)")
	planPath := flag.String("plan", "", "Path to a previously saved plan JSON for recovery or amendment (use - for stdin)")
	presentationID := flag.String("presentation", "", "ID of an existing presentation to modify (edit mode)")
	webFlag := flag.Bool("web", false, "Start a web dashboard to visualize the agent pipeline; file can be uploaded via the UI")
	summaryFlag := flag.Bool("summary", false, "Generate a human-readable summary of the presentation via LLM after pipeline completion")

	flag.Usage = func() {
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
			}{"FIXFONTS", &fixfonts.Config{}},
		)
	}
	flag.Parse()

	var presPlan *model.PresentationPlan
	var mon *monitor.Monitor
	var collector *metrics.Collector

	var sgCfg slidegenConfig
	if err := envconfig.Process("SLIDEGEN", &sgCfg); err != nil {
		log.Fatalf("Configuration error: %v", err)
	}
	useWeb := *webFlag
	useChat := !hasUserRequest(*filePath) && !useWeb

	if *presentationID != "" && *planPath != "" {
		log.Fatal("--presentation and --plan are mutually exclusive")
	}

	if *presentationID != "" {
		editMode(*presentationID, *filePath, *credentials)
		return
	}

	switch {
	case *planPath != "" && !hasUserRequest(*filePath):
		// Recovery mode: load existing plan, skip to Phase 2
		presPlan = loadPlanFromFile(*planPath)
		slog.Info("plan loaded", "title", presPlan.PresentationTitle, "slides", len(presPlan.Slides))

	case *planPath != "":
		// Amend mode: load existing plan + user request, send to Claude for modification
		presPlan = amendMode(*planPath, *filePath, *dumpPrompt)

	default:
		// Multi-agent pipeline (always): interactive chat when no file/stdin
		presPlan, mon, collector = agentMode(*filePath, useWeb, useChat, sgCfg.WebAddr)
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

	// --- Phase 2: Create presentation via Google Slides/Drive APIs ---

	slidesCfg, err := config.LoadSlidesConfig()
	if err != nil {
		fatalWithPlanDump(presPlan, mon, "Configuration error: %v", err)
	}

	credFile := *credentials
	if credFile == "" {
		credFile = slidesCfg.Credentials
	}

	ctx := context.Background()
	slidesClient, err := auth.GetOAuthClient(ctx, credFile)
	if err != nil {
		fatalWithPlanDump(presPlan, mon, "Failed to get authenticated client: %v", err)
	}

	slidesSrv, err := slides.NewService(ctx, option.WithHTTPClient(slidesClient))
	if err != nil {
		fatalWithPlanDump(presPlan, mon, "Failed to create Slides service: %v", err)
	}

	driveSrv, err := drive.NewService(ctx, option.WithHTTPClient(slidesClient))
	if err != nil {
		fatalWithPlanDump(presPlan, mon, "Failed to create Drive service: %v", err)
	}

	presId, err := pipeline.ExecutePlan(ctx, presPlan, slidesSrv, driveSrv)
	if err != nil {
		fatalWithPlanDump(presPlan, mon, "Failed to execute plan: %v", err)
	}

	url := fmt.Sprintf("https://docs.google.com/presentation/d/%s/edit", presId)

	if mon != nil {
		mon.SendURL(url)
	}

	ffCfg, err := fixfonts.LoadConfig()
	if err != nil {
		slog.Warn("fixfonts config error, skipping", "error", err)
	} else {
		vertexCfg, err := vertex.LoadConfig()
		if err != nil {
			slog.Warn("vertex config error, skipping fixfonts", "error", err)
		} else {
			vc, err := vertex.NewClient(ctx, vertexCfg)
			if err != nil {
				slog.Warn("vertex client error, skipping fixfonts", "error", err)
			} else {
				slog.Info("running fixfonts on generated presentation")
				if err := fixfonts.Run(ctx, slidesSrv, driveSrv, vc, ffCfg, presId); err != nil {
					slog.Warn("fixfonts failed", "error", err)
				}
			}
		}
	}

	fmt.Println(url)

	if collector != nil {
		metrics.PrintTable(os.Stderr, collector.Summary())
	}

	if *summaryFlag && presPlan != nil {
		vertexCfg, vErr := vertex.LoadConfig()
		if vErr != nil {
			slog.Warn("summary: failed to load vertex config", "error", vErr)
		} else {
			vc, vcErr := vertex.NewClient(ctx, vertexCfg)
			if vcErr != nil {
				slog.Warn("summary: failed to create vertex client", "error", vcErr)
			} else {
				summaryText, sErr := generatePresentationSummary(ctx, vc, sgCfg.SummaryModel, presPlan, string(readUserRequestOrEmpty(*filePath)))
				if sErr != nil {
					slog.Warn("summary generation failed", "error", sErr)
				} else {
					fmt.Fprintln(os.Stderr)
					fmt.Fprintln(os.Stderr, summaryText)
				}
			}
		}
	}
}

// agentMode runs the multi-agent pipeline: Outliner → Selector → Writers
// (parallel) → Reviewer, then enriches the plan. Returns the monitor if active.
func agentMode(filePath string, useWeb, useChat bool, webAddr string) (*model.PresentationPlan, *monitor.Monitor, *metrics.Collector) {
	vertexCfg, err := vertex.LoadConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	agentCfg, err := agent.LoadConfig()
	if err != nil {
		log.Fatalf("Agent configuration error: %v", err)
	}

	var mon *monitor.Monitor
	if useWeb {
		mon = monitor.New(agentCfg)
		textHandler := slog.NewTextHandler(os.Stderr, nil)
		slog.SetDefault(slog.New(mon.Handler(textHandler)))
		go func() {
			if err := mon.Start(webAddr); err != nil {
				slog.Error("web server failed", "error", err)
			}
		}()
		slog.Info("web dashboard available", "url", "http://localhost"+webAddr)
	}

	var inputReader *input.Reader
	if useChat {
		home, _ := os.UserHomeDir()
		histFile := ""
		if home != "" {
			histFile = filepath.Join(home, ".slidegen_history")
		}
		var initErr error
		inputReader, initErr = input.New(input.Config{HistoryFile: histFile})
		if initErr != nil {
			log.Fatalf("Failed to initialize input: %v", initErr)
		}
		defer inputReader.Close()
	}

	var userRequest []byte
	if hasUserRequest(filePath) {
		userRequest = readUserRequest(filePath)
		if mon != nil {
			mon.MarkStarted()
		}
	} else if useChat {
		text, inputErr := inputReader.ReadMultiLine()
		if inputErr != nil {
			log.Fatalf("Input error: %v", inputErr)
		}
		userRequest = []byte(text)
	} else if mon != nil {
		slog.Info("waiting for file upload via web dashboard")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		data, wErr := mon.WaitForRequest(ctx)
		if wErr != nil {
			log.Fatalf("Failed to get request: %v", wErr)
		}
		userRequest = data
		mon.MarkStarted()
	} else {
		flag.Usage()
		os.Exit(1)
	}

	slidesCfg, err := config.LoadSlidesConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	index, err := plan.LoadTemplateIndex(slidesCfg.EffectiveTemplateIndex())
	if err != nil {
		log.Fatalf("Failed to load template index: %v\nPlease run 'go run buildTemplateIndex/build_template_index.go' first", err)
	}

	exclusions := plan.LoadExclusions(slidesCfg.TemplateDir())
	compactIndex := plan.BuildCompactIndex(index, plan.HashSeed(string(userRequest)), exclusions)
	templateInstructions := pipeline.LoadTemplateInstructions(slidesCfg.TemplateDir())

	ctx := context.Background()

	slog.Info("generating slide plan via multi-agent pipeline")
	vc, err := vertex.NewClient(ctx, vertexCfg)
	if err != nil {
		log.Fatalf("Failed to create Vertex AI client: %v", err)
	}

	orch := orchestrator.New(vc, agentCfg)
	if useChat {
		slog.Info("interactive outline mode: refine the outline before pipeline starts")
		ol := outliner.New(vc, agentCfg.OutlinerModel, agentCfg.OutlinerMaxTokens)
		feedbackFn := func(outline *agent.PresentationOutline) (string, error) {
			return inputReader.ReadFeedback(agent.FormatOutline(outline))
		}
		approvedOutline, usages, chatErr := ol.RunInteractive(ctx, string(userRequest), templateInstructions, feedbackFn)
		if chatErr != nil {
			log.Fatalf("Interactive outline failed: %v", chatErr)
		}
		for _, u := range usages {
			orch.Collector().Record(metrics.AgentCall{
				Agent: "outliner", Model: agentCfg.OutlinerModel,
				InputTokens: u.InputTokens, OutputTokens: u.OutputTokens,
				CacheReadInputTokens: u.CacheReadInputTokens, CacheCreationInputTokens: u.CacheCreationInputTokens,
			})
		}
		orch.Outline = approvedOutline
	}
	genPlan, collector, err := orch.Generate(ctx, string(userRequest), compactIndex, templateInstructions)
	if err != nil {
		log.Fatalf("Agent pipeline failed: %v", err)
	}

	presPlan := plan.EnrichPlan(genPlan, index, slidesCfg.TemplateID, string(userRequest))
	slog.Info("plan generated (agent mode)", "title", presPlan.PresentationTitle, "slides", len(presPlan.Slides))

	if len(presPlan.Slides) == 0 {
		log.Fatal("Plan has no slides")
	}

	return presPlan, mon, collector
}

// amendMode loads an existing plan, reads the user's amendment request, sends
// both to Claude for modification, and returns the enriched amended plan.
func amendMode(planPath, filePath string, dumpPrompt bool) *model.PresentationPlan {
	existingPlan := loadPlanFromFile(planPath)
	slog.Info("base plan loaded", "title", existingPlan.PresentationTitle, "slides", len(existingPlan.Slides))

	userRequest := readUserRequest(filePath)

	slidesCfg, err := config.LoadSlidesConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	index, err := plan.LoadTemplateIndex(slidesCfg.EffectiveTemplateIndex())
	if err != nil {
		log.Fatalf("Failed to load template index: %v\nPlease run 'go run buildTemplateIndex/build_template_index.go' first", err)
	}

	exclusions := plan.LoadExclusions(slidesCfg.TemplateDir())
	compactIndex := plan.BuildCompactIndex(index, plan.HashSeed(string(userRequest)), exclusions)
	templateInstructions := pipeline.LoadTemplateInstructions(slidesCfg.TemplateDir())

	existingPlanJSON := pipeline.PlanToGenerationSummary(existingPlan)
	prompt := pipeline.BuildAmendPrompt(pipeline.AmendPromptData{
		ExistingPlan:      existingPlanJSON,
		TemplateIndex:     compactIndex,
		AmendmentRequest:  string(userRequest),
		ExtraInstructions: templateInstructions,
	})

	if dumpPrompt {
		fmt.Print(prompt)
		os.Exit(0)
	}

	vertexCfg, err := vertex.LoadConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	var sgCfg slidegenConfig
	if err := envconfig.Process("SLIDEGEN", &sgCfg); err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	ctx := context.Background()

	slog.Info("amending plan via Claude")
	vc, err := vertex.NewClient(ctx, vertexCfg)
	if err != nil {
		log.Fatalf("Failed to create Vertex AI client: %v", err)
	}

	genPlan, err := pipeline.SendPrompt(ctx, vc, sgCfg.Model, prompt)
	if err != nil {
		log.Fatalf("Failed to generate amended plan: %v", err)
	}

	presPlan := plan.EnrichPlan(genPlan, index, slidesCfg.TemplateID, string(userRequest))
	slog.Info("amended plan generated", "title", presPlan.PresentationTitle, "slides", len(presPlan.Slides))

	if len(presPlan.Slides) == 0 {
		log.Fatal("Amended plan has no slides")
	}

	return presPlan
}

// readUserRequest reads the user request from a file or stdin.
func readUserRequest(filePath string) []byte {
	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			log.Fatalf("Failed to read file: %v", err)
		}
		if len(data) == 0 {
			log.Fatal("Empty input")
		}
		return data
	}

	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		flag.Usage()
		os.Exit(1)
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		log.Fatalf("Failed to read stdin: %v", err)
	}
	if len(data) == 0 {
		log.Fatal("Empty input")
	}
	return data
}

// hasUserRequest returns true if a user request is available (via --file flag
// or piped stdin).
func hasUserRequest(filePath string) bool {
	if filePath != "" {
		return true
	}
	stat, _ := os.Stdin.Stat()
	return (stat.Mode() & os.ModeCharDevice) == 0
}

// loadPlanFromFile reads and parses a PresentationPlan JSON from a file path,
// or from stdin if path is "-".
func loadPlanFromFile(path string) *model.PresentationPlan {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		log.Fatalf("Failed to read plan: %v", err)
	}

	var p model.PresentationPlan
	if err := json.Unmarshal(data, &p); err != nil {
		log.Fatalf("Failed to parse plan: %v", err)
	}

	if len(p.Slides) == 0 {
		log.Fatal("Plan has no slides")
	}

	return &p
}

// savePlanToTempFile writes the PresentationPlan as indented JSON to a
// temporary file and returns the file path.
func savePlanToTempFile(p *model.PresentationPlan) (string, error) {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal plan: %w", err)
	}
	f, err := os.CreateTemp("", "slidegen-plan-*.json")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	name := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("failed to write plan: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("failed to close plan file: %w", err)
	}
	return name, nil
}

// editMode reads an existing presentation, runs the EditPlanner agent to
// produce an edit plan, then applies the modifications in-place.
func editMode(presID, filePath, credFile string) {
	vertexCfg, err := vertex.LoadConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	agentCfg, err := agent.LoadConfig()
	if err != nil {
		log.Fatalf("Agent configuration error: %v", err)
	}

	slidesCfg, err := config.LoadSlidesConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	ctx := context.Background()

	if credFile == "" {
		credFile = slidesCfg.Credentials
	}

	slidesClient, err := auth.GetOAuthClient(ctx, credFile)
	if err != nil {
		log.Fatalf("Failed to get authenticated client: %v", err)
	}

	slidesSrv, err := slides.NewService(ctx, option.WithHTTPClient(slidesClient))
	if err != nil {
		log.Fatalf("Failed to create Slides service: %v", err)
	}

	slog.Info("reading existing presentation", "id", presID)
	existingSlides, err := pipeline.ReadPresentation(ctx, slidesSrv, presID)
	if err != nil {
		log.Fatalf("Failed to read presentation: %v", err)
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
			log.Fatalf("Failed to initialize input: %v", initErr)
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
			log.Fatalf("Input error: %v", inputErr)
		}
		userRequest = []byte(text)
	}

	index, err := plan.LoadTemplateIndex(slidesCfg.EffectiveTemplateIndex())
	if err != nil {
		log.Fatalf("Failed to load template index: %v\nPlease run 'go run buildTemplateIndex/build_template_index.go' first", err)
	}

	exclusions := plan.LoadExclusions(slidesCfg.TemplateDir())
	compactIndex := plan.BuildCompactIndex(index, plan.HashSeed(string(userRequest)), exclusions)
	templateInstructions := pipeline.LoadTemplateInstructions(slidesCfg.TemplateDir())

	vc, err := vertex.NewClient(ctx, vertexCfg)
	if err != nil {
		log.Fatalf("Failed to create Vertex AI client: %v", err)
	}

	ep := editplanner.New(vc, agentCfg.EditPlannerModel, agentCfg.EditPlannerMaxTokens)

	var editPlan *model.EditPlan
	if useChat {
		feedbackFn := func(plan *model.EditPlan) (string, error) {
			return inputReader.ReadFeedback(editplanner.FormatEditPlan(plan))
		}
		var planErr error
		editPlan, _, planErr = ep.RunInteractive(ctx, presID, existingSlides, string(userRequest), compactIndex, templateInstructions, feedbackFn)
		if planErr != nil {
			log.Fatalf("EditPlanner interactive failed: %v", planErr)
		}
	} else {
		var planErr error
		editPlan, _, planErr = ep.Run(ctx, presID, existingSlides, string(userRequest), compactIndex, templateInstructions)
		if planErr != nil {
			log.Fatalf("EditPlanner failed: %v", planErr)
		}
	}

	slog.Info("edit plan produced", "operations", len(editPlan.Operations))
	for i, op := range editPlan.Operations {
		slog.Info("  operation", "index", i, "type", op.Type, "slideIndex", op.SlideIndex, "rationale", op.Rationale)
	}

	if err := pipeline.ExecuteEditPlan(ctx, editPlan, slidesSrv, slidesCfg.TemplateID, index); err != nil {
		log.Fatalf("Failed to execute edit plan: %v", err)
	}

	url := fmt.Sprintf("https://docs.google.com/presentation/d/%s/edit", presID)
	fmt.Println(url)
}

// fatalWithPlanDump saves the plan to a temp file, prints recovery instructions
// to stderr, then exits with a fatal error.
func fatalWithPlanDump(p *model.PresentationPlan, mon *monitor.Monitor, format string, args ...any) {
	if p != nil {
		path, saveErr := savePlanToTempFile(p)
		if saveErr != nil {
			slog.Error("failed to save plan for recovery", "error", saveErr)
		} else {
			fmt.Fprintf(os.Stderr, "\nPlan saved to: %s\n", path)
			fmt.Fprintf(os.Stderr, "To retry:  slidegen --plan %s\n", path)
			fmt.Fprintf(os.Stderr, "To amend:  slidegen --plan %s --file amendments.md\n\n", path)
		}
	}
	if mon != nil {
		mon.SendError(fmt.Sprintf(format, args...))
		time.Sleep(500 * time.Millisecond)
	}
	log.Fatalf(format, args...)
}
