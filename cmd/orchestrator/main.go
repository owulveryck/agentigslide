// Command orchestrator exposes the full slide-generation pipeline as a
// standalone A2A server.
//
// It serves two HTTP endpoints:
//   - GET /.well-known/agent-card.json — the public AgentCard for discovery.
//   - POST /*, GET /*, DELETE /* — the A2A REST transport (send message,
//     get task, cancel task, streaming, etc.).
//
// The server accepts a plain-text presentation request (markdown), runs the
// multi-agent pipeline (Outliner -> Selector -> Writers -> Reviewer), creates
// the Google Slides presentation, runs fixfonts, and returns the presentation
// URL as a text artifact.
//
// Configuration is identical to the MCP server: set SLIDES_*, VERTEX_*,
// AGENT_*, and FIXFONTS_* environment variables. Use -h to list all
// available variables with their defaults.
//
// Usage:
//
//	go run cmd/orchestrator/main.go [--addr :8084]
package main

import (
	"context"
	"flag"
	"fmt"
	"iter"
	"log"
	"log/slog"
	"net/http"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/slides/v1"

	"github.com/owulveryck/agentigslide/internal/agent"
	"github.com/owulveryck/agentigslide/internal/agent/orchestrator"
	"github.com/owulveryck/agentigslide/internal/auth"
	"github.com/owulveryck/agentigslide/internal/config"
	"github.com/owulveryck/agentigslide/internal/fixfonts"
	"github.com/owulveryck/agentigslide/internal/model"
	"github.com/owulveryck/agentigslide/internal/pipeline"
	"github.com/owulveryck/agentigslide/internal/plan"
	"github.com/owulveryck/agentigslide/internal/vertex"
)

type orchestratorExecutor struct {
	orch                 *orchestrator.Orchestrator
	index                *model.TemplateIndex
	slidesCfg            config.SlidesConfig
	templateInstructions string
	slidesSrv            *slides.Service
	driveSrv             *drive.Service
	vc                   *vertex.Client
	ffCfg                fixfonts.Config
}

var _ a2asrv.AgentExecutor = (*orchestratorExecutor)(nil)

func (oe *orchestratorExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		if execCtx.StoredTask == nil {
			if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
				return
			}
		}

		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, nil), nil) {
			return
		}

		content := extractText(execCtx.Message)
		if content == "" {
			msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("error: empty content — provide markdown text describing the presentation"))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, msg), nil)
			return
		}

		exclusions := plan.LoadExclusions(oe.slidesCfg.TemplateDir())
		compactIndex := plan.BuildCompactIndex(oe.index, plan.HashSeed(content), exclusions)

		slog.Info("generating slide plan via multi-agent pipeline")
		genPlan, _, err := oe.orch.Generate(ctx, content, compactIndex, oe.templateInstructions)
		if err != nil {
			msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("pipeline failed: "+err.Error()))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, msg), nil)
			return
		}

		presPlan := plan.EnrichPlan(genPlan, oe.index, oe.slidesCfg.TemplateID, content)
		if len(presPlan.Slides) == 0 {
			msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("generated plan has no slides — the content may not match available templates"))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, msg), nil)
			return
		}

		presID, err := pipeline.ExecutePlan(ctx, presPlan, oe.slidesSrv, oe.driveSrv)
		if err != nil {
			msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("failed to create presentation: "+err.Error()))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, msg), nil)
			return
		}

		if err := fixfonts.Run(ctx, oe.slidesSrv, oe.driveSrv, oe.vc, oe.ffCfg, presID); err != nil {
			slog.Warn("fixfonts failed", "error", err)
		}

		url := fmt.Sprintf("https://docs.google.com/presentation/d/%s/edit", presID)
		slog.Info("presentation created", "url", url)

		if !yield(a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(url)), nil) {
			return
		}

		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil)
	}
}

func (oe *orchestratorExecutor) Cancel(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
	}
}

func extractText(msg *a2a.Message) string {
	if msg == nil {
		return ""
	}
	var text string
	for _, part := range msg.Parts {
		if t := part.Text(); t != "" {
			if text != "" {
				text += "\n"
			}
			text += t
		}
	}
	return strings.TrimSpace(text)
}

func main() {
	addr := flag.String("addr", ":8084", "Listen address")
	flag.Parse()

	ctx := context.Background()

	slidesCfg, err := config.LoadSlidesConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	vertexCfg, err := vertex.LoadConfig()
	if err != nil {
		log.Fatalf("Vertex configuration error: %v", err)
	}

	agentCfg, err := agent.LoadConfig()
	if err != nil {
		log.Fatalf("Agent configuration error: %v", err)
	}

	ffCfg, err := fixfonts.LoadConfig()
	if err != nil {
		log.Fatalf("Fixfonts configuration error: %v", err)
	}

	index, err := plan.LoadTemplateIndex(slidesCfg.EffectiveTemplateIndex())
	if err != nil {
		log.Fatalf("Failed to load template index: %v\nPlease run 'go run buildTemplateIndex/build_template_index.go' first", err)
	}

	templateInstructions := pipeline.LoadTemplateInstructions(slidesCfg.TemplateDir())

	vc, err := vertex.NewClient(ctx, vertexCfg)
	if err != nil {
		log.Fatalf("Failed to create Vertex AI client: %v", err)
	}

	slidesClient, err := auth.GetOAuthClient(ctx, slidesCfg.Credentials)
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

	orch := orchestrator.New(vc, agentCfg)

	exec := &orchestratorExecutor{
		orch:                 orch,
		index:                index,
		slidesCfg:            slidesCfg,
		templateInstructions: templateInstructions,
		slidesSrv:            slidesSrv,
		driveSrv:             driveSrv,
		vc:                   vc,
		ffCfg:                ffCfg,
	}

	handler := a2asrv.NewHandler(exec)

	card := orchestrator.Card()
	card.SupportedInterfaces = []*a2a.AgentInterface{
		a2a.NewAgentInterface(fmt.Sprintf("http://localhost%s", *addr), a2a.TransportProtocolHTTPJSON),
	}

	mux := http.NewServeMux()
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(&card))
	mux.Handle("/", a2asrv.NewRESTHandler(handler))

	slog.Info("A2A orchestrator server listening", "addr", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}
