// Command slidegen is the full pipeline for generating a Google Slides
// presentation from a markdown file. It reads a user request from the specified
// file, generates a slide plan via Claude (Vertex AI), creates the presentation
// by duplicating template slides via the Google Slides and Drive APIs, applies
// text content with markdown formatting, and optionally runs the fixfonts
// post-processing step to correct formatting issues.
//
// Usage:
//
//	go run slidegen/main.go --file request.md [--credentials creds.json]
//	go run slidegen/main.go --plan saved-plan.json
//	go run slidegen/main.go --plan saved-plan.json --file amendments.md
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

	"github.com/owulveryck/slideAppScripter/internal/agent"
	"github.com/owulveryck/slideAppScripter/internal/auth"
	"github.com/owulveryck/slideAppScripter/internal/config"
	"github.com/owulveryck/slideAppScripter/internal/fixfonts"
	"github.com/owulveryck/slideAppScripter/internal/model"
	"github.com/owulveryck/slideAppScripter/internal/pipeline"
	"github.com/owulveryck/slideAppScripter/internal/plan"
	"github.com/owulveryck/slideAppScripter/internal/vertex"

	"github.com/kelseyhightower/envconfig"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/slides/v1"
)

type slidegenConfig struct {
	Model     string `envconfig:"MODEL" default:"claude-opus-4-6" desc:"Claude model for slide plan generation (monolithic mode)"`
	AgentMode bool   `envconfig:"AGENT_MODE" default:"false" desc:"Enable multi-agent pipeline (Outliner/Selector/Writers/Reviewer)"`
}

func main() {
	filePath := flag.String("file", "", "Path to markdown file with the presentation request (reads stdin if omitted and stdin is a pipe)")
	credentials := flag.String("credentials", "", "Path to OAuth2 client credentials JSON (optional; uses ADC if omitted)")
	dumpPrompt := flag.Bool("dump", false, "Print the prompt that would be sent to Claude and exit")
	promptFile := flag.String("prompt", "", "Path to a custom prompt template file (must contain two %%s: template index, user request)")
	planPath := flag.String("plan", "", "Path to a previously saved plan JSON for recovery or amendment (use - for stdin)")
	agentFlag := flag.Bool("agent", false, "Use multi-agent pipeline (Outliner/Selector/Writers/Reviewer) instead of monolithic mode")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage:
  slidegen --file <request.md>                          Generate from scratch
  slidegen --agent --file <request.md>                  Generate using multi-agent pipeline
  slidegen --plan <plan.json>                           Retry from a saved plan
  slidegen --plan <plan.json> --file <amendments.md>    Amend an existing plan

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

	var sgCfg slidegenConfig
	if err := envconfig.Process("SLIDEGEN", &sgCfg); err != nil {
		log.Fatalf("Configuration error: %v", err)
	}
	useAgent := *agentFlag || sgCfg.AgentMode

	switch {
	case *planPath != "" && !hasUserRequest(*filePath):
		// Recovery mode: load existing plan, skip to Phase 2
		presPlan = loadPlanFromFile(*planPath)
		slog.Info("plan loaded", "title", presPlan.PresentationTitle, "slides", len(presPlan.Slides))

	case *planPath != "":
		// Amend mode: load existing plan + user request, send to Claude for modification
		presPlan = amendMode(*planPath, *filePath, *dumpPrompt)

	case useAgent:
		// Multi-agent mode: Outliner → Selector → Writers (parallel) → Reviewer
		presPlan = agentMode(*filePath)

	default:
		// Generate from scratch (original monolithic flow)
		presPlan = generateMode(*filePath, *dumpPrompt, *promptFile)
	}

	// --- Phase 2: Create presentation via Google Slides/Drive APIs ---

	slidesCfg, err := config.LoadSlidesConfig()
	if err != nil {
		fatalWithPlanDump(presPlan, "Configuration error: %v", err)
	}

	credFile := *credentials
	if credFile == "" {
		credFile = slidesCfg.Credentials
	}

	ctx := context.Background()
	slidesClient, err := auth.GetOAuthClient(ctx, credFile)
	if err != nil {
		fatalWithPlanDump(presPlan, "Failed to get authenticated client: %v", err)
	}

	slidesSrv, err := slides.NewService(ctx, option.WithHTTPClient(slidesClient))
	if err != nil {
		fatalWithPlanDump(presPlan, "Failed to create Slides service: %v", err)
	}

	driveSrv, err := drive.NewService(ctx, option.WithHTTPClient(slidesClient))
	if err != nil {
		fatalWithPlanDump(presPlan, "Failed to create Drive service: %v", err)
	}

	presId, err := pipeline.ExecutePlan(ctx, presPlan, slidesSrv, driveSrv)
	if err != nil {
		fatalWithPlanDump(presPlan, "Failed to execute plan: %v", err)
	}

	url := fmt.Sprintf("https://docs.google.com/presentation/d/%s/edit", presId)

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
}

// agentMode runs the multi-agent pipeline: Outliner → Selector → Writers
// (parallel) → Reviewer, then enriches the plan.
func agentMode(filePath string) *model.PresentationPlan {
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

	vertexCfg, err := vertex.LoadConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	agentCfg, err := agent.LoadConfig()
	if err != nil {
		log.Fatalf("Agent configuration error: %v", err)
	}

	ctx := context.Background()

	slog.Info("generating slide plan via multi-agent pipeline")
	vc, err := vertex.NewClient(ctx, vertexCfg)
	if err != nil {
		log.Fatalf("Failed to create Vertex AI client: %v", err)
	}

	orchestrator := agent.NewOrchestrator(vc, agentCfg)
	genPlan, err := orchestrator.Generate(ctx, string(userRequest), compactIndex, templateInstructions)
	if err != nil {
		log.Fatalf("Agent pipeline failed: %v", err)
	}

	presPlan := plan.EnrichPlan(genPlan, index, slidesCfg.TemplateID, string(userRequest))
	slog.Info("plan generated (agent mode)", "title", presPlan.PresentationTitle, "slides", len(presPlan.Slides))

	if len(presPlan.Slides) == 0 {
		log.Fatal("Plan has no slides")
	}

	return presPlan
}

// generateMode runs the full Phase 1: read user request, build prompt, call
// Claude, enrich the plan. Returns the enriched PresentationPlan.
func generateMode(filePath string, dumpPrompt bool, promptFile string) *model.PresentationPlan {
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

	promptTemplate := pipeline.DefaultPromptTemplate
	if promptFile != "" {
		custom, err := os.ReadFile(promptFile)
		if err != nil {
			log.Fatalf("Failed to read prompt file: %v", err)
		}
		promptTemplate = string(custom)
	}
	templateInstructions := pipeline.LoadTemplateInstructions(slidesCfg.TemplateDir())

	prompt := pipeline.BuildPrompt(promptTemplate, compactIndex, string(userRequest), templateInstructions)

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

	slog.Info("generating slide plan via Claude")
	vc, err := vertex.NewClient(ctx, vertexCfg)
	if err != nil {
		log.Fatalf("Failed to create Vertex AI client: %v", err)
	}

	genPlan, err := pipeline.SendPrompt(ctx, vc, sgCfg.Model, prompt)
	if err != nil {
		log.Fatalf("Failed to generate plan: %v", err)
	}

	presPlan := plan.EnrichPlan(genPlan, index, slidesCfg.TemplateID, string(userRequest))
	slog.Info("plan generated", "title", presPlan.PresentationTitle, "slides", len(presPlan.Slides))

	if len(presPlan.Slides) == 0 {
		log.Fatal("Plan has no slides")
	}

	return presPlan
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
	prompt := pipeline.BuildAmendPrompt(compactIndex, existingPlanJSON, string(userRequest), templateInstructions)

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

// fatalWithPlanDump saves the plan to a temp file, prints recovery instructions
// to stderr, then exits with a fatal error.
func fatalWithPlanDump(p *model.PresentationPlan, format string, args ...any) {
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
	log.Fatalf(format, args...)
}
