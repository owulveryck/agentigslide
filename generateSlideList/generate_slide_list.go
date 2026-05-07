// Command generateSlideList generates a structured presentation plan JSON from
// a user request. It loads the template index, sends the user request along
// with a compact template description to Claude via Vertex AI, and outputs an
// enriched PresentationPlan to stdout.
//
// Usage:
//
//	go run generateSlideList/generate_slide_list.go --request "Create a deck about innovation"
//	go run generateSlideList/generate_slide_list.go --interactive
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/owulveryck/agentigslide/internal/config"
	"github.com/owulveryck/agentigslide/internal/model"
	"github.com/owulveryck/agentigslide/internal/pipeline"
	"github.com/owulveryck/agentigslide/internal/plan"
	"github.com/owulveryck/agentigslide/internal/vertex"

	"github.com/kelseyhightower/envconfig"
)

type genslidesConfig struct {
	Model string `envconfig:"MODEL" default:"claude-sonnet-4-5@20250929" desc:"Claude model for plan generation"`
}

func main() {
	interactive := flag.Bool("interactive", false, "Interactive mode (read from stdin)")
	request := flag.String("request", "", "User request for slide generation")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: generate_slide_list --request \"your request\" OR --interactive\n\nFlags:\n")
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
			}{"GENSLIDES", &genslidesConfig{}},
		)
	}
	flag.Parse()

	var userRequest string
	if *interactive {
		fmt.Fprintln(os.Stderr, "Enter your slide generation request:")
		var input bytes.Buffer
		_, _ = io.Copy(&input, os.Stdin)
		userRequest = input.String()
	} else if *request != "" {
		userRequest = *request
	} else {
		flag.Usage()
		os.Exit(1)
	}

	slidesCfg, err := config.LoadSlidesConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	vertexCfg, err := vertex.LoadConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	var gsCfg genslidesConfig
	if err := envconfig.Process("GENSLIDES", &gsCfg); err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	index, err := plan.LoadTemplateIndex(slidesCfg.EffectiveTemplateIndex())
	if err != nil {
		log.Fatalf("Failed to load template index: %v\nPlease run 'go run buildTemplateIndex/build_template_index.go' first", err)
	}

	ctx := context.Background()
	vc, err := vertex.NewClient(ctx, vertexCfg)
	if err != nil {
		log.Fatalf("Failed to create Vertex AI client: %v", err)
	}

	exclusions := plan.LoadExclusions(slidesCfg.TemplateDir())
	compactIndex := plan.BuildCompactIndex(index, plan.HashSeed(userRequest), exclusions)
	templateInstructions := pipeline.LoadTemplateInstructions(slidesCfg.TemplateDir())

	genPlan, err := parseUserRequest(ctx, vc, gsCfg.Model, userRequest, compactIndex, templateInstructions)
	if err != nil {
		log.Fatalf("Failed to parse user request: %v", err)
	}

	output := plan.EnrichPlan(genPlan, index, slidesCfg.TemplateID, userRequest)

	result, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal output: %v", err)
	}
	fmt.Println(string(result))
}

func parseUserRequest(ctx context.Context, vc *vertex.Client, modelName, userRequest, templateIndexJSON, extraInstructions string) (*model.GenerationPlan, error) {
	prompt := pipeline.BuildPrompt(pipeline.PromptData{
		TemplateIndex:     templateIndexJSON,
		UserRequest:       userRequest,
		ExtraInstructions: extraInstructions,
	})
	return pipeline.SendPrompt(ctx, vc, modelName, prompt)
}
