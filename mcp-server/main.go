// Command mcp-server exposes the slidegen presentation generation pipeline
// as an MCP (Model Context Protocol) server. A chatbot or AI agent can call
// the "generate_slides" tool with markdown content describing the desired
// presentation, and receives back the URL of the created Google Slides
// presentation.
//
// Configuration is identical to the slidegen CLI: set SLIDES_*, VERTEX_*,
// SLIDEGEN_*, and FIXFONTS_* environment variables.
//
// Usage:
//
//	go run mcp-server/main.go [--mode stdio|sse] [--addr :8080]
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"strings"

	"github.com/owulveryck/slideAppScripter/internal/auth"
	"github.com/owulveryck/slideAppScripter/internal/config"
	"github.com/owulveryck/slideAppScripter/internal/fixfonts"
	"github.com/owulveryck/slideAppScripter/internal/pipeline"
	"github.com/owulveryck/slideAppScripter/internal/plan"
	"github.com/owulveryck/slideAppScripter/internal/vertex"

	"github.com/kelseyhightower/envconfig"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/slides/v1"
)

type slidegenConfig struct {
	Model string `envconfig:"MODEL" default:"claude-opus-4-6"`
}

type generateSlidesArgs struct {
	Content string `json:"content" jsonschema:"Contenu markdown de la presentation a generer. Fournir le texte complet : titre, sections (# titres), bullet points (- item), texte de contenu. Supporte **gras** et *italique*. Le contenu doit etre en francais. Le systeme selectionne automatiquement les templates adaptes."`
}

const toolDescription = `Generate a professional Google Slides presentation from markdown content using the OCTO Technology slide template library.

Takes markdown-formatted text describing the desired presentation content and produces a fully formatted Google Slides deck. The system automatically selects the best matching templates (title slides, section dividers, content slides, data tables, key figures, etc.) from 50+ professionally designed OCTO templates.

INPUT FORMAT:
- Start with the presentation title (becomes the cover slide)
- Use # headings for major sections (each gets a section divider slide)
- Use ## for subsections
- Use bullet points (- item) for lists, with indentation for sub-items
- Use **bold** and *italic* for emphasis
- Content should be in French (templates use French typography)
- All text must be provided explicitly - the system does NOT invent or hallucinate content
- Do NOT include layout or template instructions - just provide the content

EXAMPLE INPUT:
"Innovation et Transformation Digitale 2026

# Introduction
Notre strategie d'innovation repose sur trois piliers fondamentaux qui guident notre transformation.

## Cloud Native
- Migration vers Kubernetes
- Architecture microservices
- Observabilite et monitoring

## Data & IA
- Pipeline de donnees temps reel
- Modeles de ML en production
- Gouvernance des donnees

# Conclusion
La transformation digitale est un levier strategique majeur pour notre croissance."

CONSTRAINTS:
- Processing takes 30-60 seconds (AI-powered template matching + Google Slides API calls)
- Content must fit template field sizes - the system adapts long text automatically
- The presentation is created in the authenticated user's Google Drive
- Each section and subsection in the input generates at least one dedicated slide

OUTPUT: Returns the URL of the created Google Slides presentation (format: https://docs.google.com/presentation/d/{id}/edit)`

func main() {
	mode := flag.String("mode", "stdio", "Transport mode: stdio, sse, or http")
	addr := flag.String("addr", ":8080", "Listen address for SSE/HTTP mode")
	allowOrigin := flag.String("allow-origin", "", "Trusted origin for cross-origin requests in HTTP mode (e.g. https://example.com)")
	flag.Parse()

	slidesCfg, err := config.LoadSlidesConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
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

	index, err := plan.LoadTemplateIndex(slidesCfg.TemplateIndex)
	if err != nil {
		log.Fatalf("Failed to load template index: %v\nPlease run 'go run buildTemplateIndex/build_template_index.go' first", err)
	}
	compactIndex := plan.BuildCompactIndex(index)

	ctx := context.Background()

	vc, err := vertex.NewClient(ctx, vertexCfg)
	if err != nil {
		log.Fatalf("Failed to create Vertex AI client: %v", err)
	}

	if slidesCfg.Credentials == "" {
		log.Fatal("Set SLIDES_CREDENTIALS to the path of your OAuth2 credentials JSON")
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
			return errResult("Empty content: provide markdown text describing the presentation to generate"), nil, nil
		}

		prompt := pipeline.BuildPrompt(pipeline.DefaultPromptTemplate, compactIndex, content)

		slog.Info("generating slide plan via Claude")
		genPlan, err := pipeline.SendPrompt(ctx, vc, sgCfg.Model, prompt)
		if err != nil {
			return errResult(fmt.Sprintf("Failed to generate slide plan: %v", err)), nil, nil
		}

		presPlan := plan.EnrichPlan(genPlan, index, slidesCfg.TemplateID, content)
		slog.Info("plan generated", "title", presPlan.PresentationTitle, "slides", len(presPlan.Slides))

		if len(presPlan.Slides) == 0 {
			return errResult("The generated plan has no slides. The content may not match available templates."), nil, nil
		}

		presId, err := pipeline.ExecutePlan(ctx, presPlan, slidesSrv, driveSrv)
		if err != nil {
			return errResult(fmt.Sprintf("Failed to create presentation: %v", err)), nil, nil
		}

		slog.Info("running fixfonts on generated presentation")
		if err := fixfonts.Run(ctx, slidesSrv, driveSrv, vc, ffCfg, presId); err != nil {
			slog.Warn("fixfonts failed", "error", err)
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
		handler := corsMiddleware(sseHandler)
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
			httpOpts.CrossOriginProtection = cop
		}
		streamHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
			return server
		}, httpOpts)
		handler := corsMiddleware(streamHandler)
		slog.Info("MCP server listening", "mode", "http", "addr", *addr)
		if err := http.ListenAndServe(*addr, handler); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("Unknown mode: %s (use stdio, sse, or http)", *mode)
	}
}

func errResult(msg string) *mcp.CallToolResult {
	r := &mcp.CallToolResult{}
	r.SetError(fmt.Errorf("%s", msg))
	return r
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
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
