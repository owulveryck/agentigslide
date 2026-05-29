package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/owulveryck/agentigslide/internal/agent"
	"github.com/owulveryck/agentigslide/internal/agent/orchestrator"
	"github.com/owulveryck/agentigslide/internal/agent/outliner"
	"github.com/owulveryck/agentigslide/internal/config"
	"github.com/owulveryck/agentigslide/internal/input"
	"github.com/owulveryck/agentigslide/internal/metrics"
	"github.com/owulveryck/agentigslide/internal/model"
	"github.com/owulveryck/agentigslide/internal/monitor"
	"github.com/owulveryck/agentigslide/internal/pipeline"
	"github.com/owulveryck/agentigslide/internal/plan"
	"github.com/owulveryck/agentigslide/internal/vertex"
)

type agentResult struct {
	plan        *model.PresentationPlan
	monitor     *monitor.Monitor
	collector   *metrics.Collector
	issueLog    agent.IssueLog
	vc          *vertex.Client
	agentCfg    agent.Config
	templateDir string
}

// agentMode runs the multi-agent pipeline: Outliner → Selector → Writers
// (parallel) → Reviewer, then enriches the plan. Returns the monitor if active.
func agentMode(filePath string, useWeb, useChat bool, webAddr string) *agentResult {
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
		printUsage()
		os.Exit(1)
	}

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

	var agentMemories map[string]string
	if agentCfg.MemoryEnabled {
		agentMemories = pipeline.LoadAllAgentMemories(slidesCfg.TemplateDir())
	}

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
		approvedOutline, usages, chatErr := ol.RunInteractive(ctx, string(userRequest), templateInstructions, feedbackFn, agentMemories["outliner"])
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
	genPlan, collector, err := orch.Generate(ctx, string(userRequest), compactIndex, templateInstructions, agentMemories)
	if err != nil {
		log.Fatalf("Agent pipeline failed: %v", err)
	}

	presPlan := plan.EnrichPlan(genPlan, index, slidesCfg.TemplateID, string(userRequest))
	slog.Info("plan generated (agent mode)", "title", presPlan.PresentationTitle, "slides", len(presPlan.Slides))

	if len(presPlan.Slides) == 0 {
		log.Fatal("Plan has no slides")
	}

	fmt.Fprintf(os.Stderr, "\n")
	return &agentResult{
		plan:        presPlan,
		monitor:     mon,
		collector:   collector,
		issueLog:    orch.IssueLog(),
		vc:          vc,
		agentCfg:    agentCfg,
		templateDir: slidesCfg.TemplateDir(),
	}
}
