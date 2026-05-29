package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"

	"github.com/kelseyhightower/envconfig"
	"github.com/owulveryck/agentigslide/internal/config"
	"github.com/owulveryck/agentigslide/internal/model"
	"github.com/owulveryck/agentigslide/internal/pipeline"
	"github.com/owulveryck/agentigslide/internal/plan"
	"github.com/owulveryck/agentigslide/internal/vertex"
)

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
		log.Fatalf("Failed to load template index: %v\nPlease run 'go run cmd/buildindex/build_template_index.go' first", err)
	}

	exclusions := plan.LoadExclusions(slidesCfg.TemplateDir())
	compactIndex := plan.BuildCompactIndex(index, plan.HashSeed(string(userRequest)), exclusions)
	templateInstructions := pipeline.LoadTemplateInstructions(slidesCfg.TemplateDir())

	existingPlanJSON := pipeline.PlanToGenerationSummary(existingPlan)
	prompt, err := pipeline.BuildAmendPrompt(pipeline.AmendPromptData{
		ExistingPlan:      existingPlanJSON,
		TemplateIndex:     compactIndex,
		AmendmentRequest:  string(userRequest),
		ExtraInstructions: templateInstructions,
	})
	if err != nil {
		log.Fatalf("Failed to build amend prompt: %v", err)
	}

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
