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
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"

	"github.com/owulveryck/slideAppScripter/internal/auth"
	"github.com/owulveryck/slideAppScripter/internal/config"
	"github.com/owulveryck/slideAppScripter/internal/fixfonts"
	"github.com/owulveryck/slideAppScripter/internal/pipeline"
	"github.com/owulveryck/slideAppScripter/internal/plan"
	"github.com/owulveryck/slideAppScripter/internal/vertex"

	"github.com/kelseyhightower/envconfig"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/slides/v1"
)

type slidegenConfig struct {
	Model string `envconfig:"MODEL" default:"claude-opus-4-6" desc:"Claude model for slide plan generation"`
}

func main() {
	filePath := flag.String("file", "", "Path to markdown file with the presentation request (reads stdin if omitted and stdin is a pipe)")
	credentials := flag.String("credentials", "", "Path to OAuth2 client credentials JSON (overrides SLIDES_CREDENTIALS)")
	dumpPrompt := flag.Bool("dump", false, "Print the prompt that would be sent to Claude and exit")
	promptFile := flag.String("prompt", "", "Path to a custom prompt template file (must contain two %%s: template index, user request)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: slidegen [--file <request.md>] [--credentials <creds.json>] [--dump] [--prompt <template.txt>]\n\nReads from stdin if --file is omitted and input is piped.\n\nFlags:\n")
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
			}{"FIXFONTS", &fixfonts.Config{}},
		)
	}
	flag.Parse()

	var userRequest []byte
	if *filePath != "" {
		var err error
		userRequest, err = os.ReadFile(*filePath)
		if err != nil {
			log.Fatalf("Failed to read file: %v", err)
		}
	} else {
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) != 0 {
			flag.Usage()
			os.Exit(1)
		}
		var err error
		userRequest, err = io.ReadAll(os.Stdin)
		if err != nil {
			log.Fatalf("Failed to read stdin: %v", err)
		}
	}

	if len(userRequest) == 0 {
		log.Fatal("Empty input")
	}

	slidesCfg, err := config.LoadSlidesConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	index, err := plan.LoadTemplateIndex(slidesCfg.TemplateIndex)
	if err != nil {
		log.Fatalf("Failed to load template index: %v\nPlease run 'go run buildTemplateIndex/build_template_index.go' first", err)
	}

	compactIndex := plan.BuildCompactIndex(index)

	promptTemplate := pipeline.DefaultPromptTemplate
	if *promptFile != "" {
		custom, err := os.ReadFile(*promptFile)
		if err != nil {
			log.Fatalf("Failed to read prompt file: %v", err)
		}
		promptTemplate = string(custom)
	}

	prompt := pipeline.BuildPrompt(promptTemplate, compactIndex, string(userRequest))

	if *dumpPrompt {
		fmt.Print(prompt)
		return
	}

	vertexCfg, err := vertex.LoadConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	var sgCfg slidegenConfig
	if err := envconfig.Process("SLIDEGEN", &sgCfg); err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	ffCfg, err := fixfonts.LoadConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	ctx := context.Background()

	// --- Phase 1: Generate plan via Claude (Vertex AI) ---

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

	// --- Phase 2: Create presentation via Google Slides/Drive APIs ---

	credFile := *credentials
	if credFile == "" {
		credFile = slidesCfg.Credentials
	}
	if credFile == "" {
		log.Fatal("Provide --credentials <file> or set SLIDES_CREDENTIALS")
	}

	slidesClient, err := auth.GetOAuthClient(ctx, credFile)
	if err != nil {
		log.Fatalf("Failed to get authenticated client: %v", err)
	}

	slidesSrv, err := slides.NewService(ctx, option.WithHTTPClient(slidesClient))
	if err != nil {
		log.Fatalf("Failed to create Slides service: %v", err)
	}

	driveSrv, err := drive.NewService(ctx, option.WithHTTPClient(slidesClient))
	if err != nil {
		log.Fatalf("Failed to create Drive service: %v", err)
	}

	presId, err := pipeline.ExecutePlan(ctx, presPlan, slidesSrv, driveSrv)
	if err != nil {
		log.Fatalf("Failed to execute plan: %v", err)
	}

	url := fmt.Sprintf("https://docs.google.com/presentation/d/%s/edit", presId)

	slog.Info("running fixfonts on generated presentation")
	if err := fixfonts.Run(ctx, slidesSrv, driveSrv, vc, ffCfg, presId); err != nil {
		slog.Warn("fixfonts failed", "error", err)
	}

	fmt.Println(url)
}
