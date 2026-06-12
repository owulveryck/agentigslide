// Command mcp-server exposes the slidegen presentation generation pipeline
// as an MCP (Model Context Protocol) server. A chatbot or AI agent can call
// the "generate_slides" tool with markdown content describing the desired
// presentation, and receives back the URL of the created Google Slides
// presentation.
//
// Three transport modes are supported:
//   - stdio (default): direct process communication via stdin/stdout,
//     suited for Claude Code and local MCP agents.
//   - sse: Server-Sent Events over HTTP, for web-based clients.
//     Use --addr to set the listen address and --allow-origin for CORS.
//   - http: bidirectional streamable HTTP with built-in cross-origin
//     protection via --allow-origin.
//
// Errors returned by the generate_slides tool are structured with a category
// prefix ([validation], [transient], [business]) and a Retryable indicator
// so that calling agents can implement differentiated recovery strategies.
// See [ADR 008] for details.
//
// Internally the server delegates to the multi-agent orchestrator
// (Outliner → Selector → Writers → Reviewer) and then calls the
// Google Slides/Drive APIs to produce the final presentation.
//
// Configuration is identical to the slidegen CLI: set SLIDES_*, VERTEX_*,
// AGENT_* environment variables. Use -h to list all
// available variables with their defaults.
//
// Usage:
//
//	go run exp/mcp-server/main.go [--mode stdio|sse|http] [--addr :8080] [--allow-origin https://example.com]
//
// [ADR 008]: ../../docs/adr/008-structured-mcp-errors.md
package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"strings"

	"github.com/owulveryck/agentigslide/internal/agent"
	"github.com/owulveryck/agentigslide/internal/agent/formatter"
	"github.com/owulveryck/agentigslide/internal/agent/orchestrator"
	"github.com/owulveryck/agentigslide/internal/auth"
	"github.com/owulveryck/agentigslide/internal/config"
	"github.com/owulveryck/agentigslide/internal/pipeline"
	"github.com/owulveryck/agentigslide/internal/plan"
	"github.com/owulveryck/agentigslide/internal/vertex"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/slides/v1"
)

// generateSlidesArgs holds the input parameters for the generate_slides MCP tool.
type generateSlidesArgs struct {
	Content string `json:"content" jsonschema:"Contenu markdown de la presentation a generer. Fournir le texte complet : titre, sections (# titres), bullet points (- item), texte de contenu. Supporte **gras**, *italique* et backticks pour police monospace. Le contenu doit etre en francais. Le systeme selectionne automatiquement les templates adaptes."`
}

//go:embed tool_description.txt
var toolDescription string

func main() {
	mode := flag.String("mode", "stdio", "Transport mode: stdio, sse, or http")
	addr := flag.String("addr", ":8080", "Listen address for SSE/HTTP mode")
	allowOrigin := flag.String("allow-origin", "", "Trusted origin for cross-origin requests in HTTP mode (e.g. https://example.com)")
	flag.Parse()

	config.SetupLogging()

	slidesCfg, err := config.LoadSlidesConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	vertexCfg, err := vertex.LoadConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	index, err := plan.LoadTemplateIndex(slidesCfg.EffectiveTemplateIndex())
	if err != nil {
		log.Fatalf("Failed to load template index: %v\nPlease run 'go run cmd/buildindex/build_template_index.go' first", err)
	}

	templateInstructions := pipeline.LoadTemplateInstructions(slidesCfg.TemplateDir())

	ctx := context.Background()

	vc, err := vertex.NewClient(ctx, vertexCfg)
	if err != nil {
		log.Fatalf("Failed to create Vertex AI client: %v", err)
	}

	agentCfg, err := agent.LoadConfig()
	if err != nil {
		log.Fatalf("Agent configuration error: %v", err)
	}
	orch := orchestrator.New(vc, agentCfg)
	orch.Invariants = plan.LoadDeckInvariants(slidesCfg.TemplateDir())

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

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "slidegen",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "generate_slides",
		Description: toolDescription,
	}, func(ctx context.Context, req *mcp.CallToolRequest, args generateSlidesArgs) (*mcp.CallToolResult, any, error) {
		content := strings.TrimSpace(args.Content)
		if content == "" {
			return structuredError(errValidation, false, "Empty content: provide markdown text describing the presentation to generate"), nil, nil
		}

		exclusions := plan.LoadExclusions(slidesCfg.TemplateDir())
		compactIndex := plan.BuildCompactIndex(index, plan.HashSeed(content), exclusions)

		slog.Info("generating slide plan via multi-agent pipeline")
		genPlan, _, err := orch.Generate(ctx, content, compactIndex, templateInstructions, nil)
		if err != nil {
			msg := fmt.Sprintf("Agent pipeline failed: %v", err)
			if isTransientPipelineError(err) {
				return structuredError(errTransient, true, msg), nil, nil
			}
			return structuredError(errBusiness, false, msg), nil, nil
		}

		presPlan := plan.EnrichPlan(genPlan, index, slidesCfg.TemplateID, content)
		slog.Info("plan generated", "title", presPlan.PresentationTitle, "slides", len(presPlan.Slides))

		if len(presPlan.Slides) == 0 {
			return structuredError(errBusiness, false, "The generated plan has no slides. The content may not match available templates."), nil, nil
		}

		execResult, revLog, err := pipeline.ExecutePlan(ctx, presPlan, pipeline.WrapSlides(slidesSrv), pipeline.WrapDrive(driveSrv))
		if err != nil {
			return structuredError(errTransient, true, fmt.Sprintf("Failed to create presentation: %v", err)), nil, nil
		}
		presId := execResult.PresentationID

		slog.Info("running formatter on generated presentation")
		f := formatter.New(slidesSrv)
		if _, fmtErr := f.Run(ctx, presId, revLog); fmtErr != nil {
			slog.Warn("formatter failed", "error", fmtErr)
		}

		url := fmt.Sprintf("https://docs.google.com/presentation/d/%s/edit", presId)
		slog.Info("presentation created", "url", url)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: url}},
		}, nil, nil
	})

	switch *mode {
	case "stdio":
		if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
			log.Fatal(err)
		}
	case "sse":
		sseHandler := mcp.NewSSEHandler(func(r *http.Request) *mcp.Server {
			return server
		}, nil)
		handler := corsMiddleware(*allowOrigin, sseHandler)
		slog.Info("MCP server listening", "mode", "sse", "addr", *addr)
		if err := http.ListenAndServe(*addr, handler); err != nil {
			log.Fatal(err)
		}
	case "http":
		httpOpts := &mcp.StreamableHTTPOptions{}
		if *allowOrigin != "" {
			cop := http.NewCrossOriginProtection()
			if err := cop.AddTrustedOrigin(*allowOrigin); err != nil {
				log.Fatalf("Invalid --allow-origin %q: %v", *allowOrigin, err)
			}
			//nolint:staticcheck // SA1019: planned refactor to middleware
			httpOpts.CrossOriginProtection = cop
		}
		streamHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
			return server
		}, httpOpts)
		handler := corsMiddleware(*allowOrigin, streamHandler)
		slog.Info("MCP server listening", "mode", "http", "addr", *addr)
		if err := http.ListenAndServe(*addr, handler); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("Unknown mode: %s (use stdio, sse, or http)", *mode)
	}
}

// errorCategory classifies MCP tool errors into three buckets so that calling
// agents can implement differentiated recovery strategies. See ADR 008.
type errorCategory string

const (
	errValidation errorCategory = "validation"
	errTransient  errorCategory = "transient"
	errBusiness   errorCategory = "business"
)

// structuredError builds a CallToolResult with IsError=true and a text-encoded
// category prefix plus retryable indicator. The format is:
//
//	[category] message
//	Retryable: true|false
//
// This is parsable by calling agents while remaining human-readable.
func structuredError(cat errorCategory, retryable bool, msg string) *mcp.CallToolResult {
	text := fmt.Sprintf("[%s] %s\nRetryable: %v", cat, msg, retryable)
	r := &mcp.CallToolResult{}
	r.SetError(fmt.Errorf("%s", text))
	return r
}

// isTransientPipelineError inspects an error message for indicators of
// temporary failures (API rate limits, timeouts, deadline exceeded) that
// are likely to succeed on retry. Unknown errors default to non-transient.
func isTransientPipelineError(err error) bool {
	msg := strings.ToLower(err.Error())
	transientIndicators := []string{"429", "529", "timeout", "context deadline", "temporarily unavailable"}
	for _, indicator := range transientIndicators {
		if strings.Contains(msg, indicator) {
			return true
		}
	}
	return false
}

// corsMiddleware wraps an http.Handler to inject CORS headers when allowOrigin
// is non-empty. It handles OPTIONS preflight requests and exposes the
// Mcp-Session-Id header required by the MCP HTTP transport.
func corsMiddleware(allowOrigin string, next http.Handler) http.Handler {
	if allowOrigin == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Mcp-Session-Id")
		w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
