// Command outliner exposes the Outliner agent as a standalone A2A server.
//
// It serves two HTTP endpoints:
//   - GET /.well-known/agent-card.json — the public AgentCard for discovery.
//   - POST /*, GET /*, DELETE /* — the A2A REST transport (send message,
//     get task, cancel task, streaming, etc.).
//
// The server uses the Vertex AI backend configured via VERTEX_* environment
// variables (same as slidegen). Use --addr to set the listen address.
//
// Usage:
//
//	go run cmd/outliner/main.go [--addr :8080]
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/owulveryck/agentigslide/internal/agent"
	"github.com/owulveryck/agentigslide/internal/agent/outliner"
	"github.com/owulveryck/agentigslide/internal/vertex"
)

func main() {
	addr := flag.String("addr", ":8080", "Listen address")
	flag.Parse()

	ctx := context.Background()

	agentCfg, err := agent.LoadConfig()
	if err != nil {
		log.Fatalf("Agent configuration error: %v", err)
	}

	vertexCfg, err := vertex.LoadConfig()
	if err != nil {
		log.Fatalf("Vertex configuration error: %v", err)
	}

	vc, err := vertex.NewClient(ctx, vertexCfg)
	if err != nil {
		log.Fatalf("Failed to create Vertex AI client: %v", err)
	}

	ol := outliner.New(vc, agentCfg.OutlinerModel, agentCfg.OutlinerMaxTokens)

	handler := a2asrv.NewHandler(ol)

	card := outliner.Card()
	card.SupportedInterfaces = []*a2a.AgentInterface{
		a2a.NewAgentInterface(fmt.Sprintf("http://localhost%s", *addr), a2a.TransportProtocolHTTPJSON),
	}

	mux := http.NewServeMux()
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(&card))
	mux.Handle("/", a2asrv.NewRESTHandler(handler))

	slog.Info("A2A outliner server listening", "addr", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}
