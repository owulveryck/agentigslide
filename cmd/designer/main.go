// Command designer invokes the Designer agent to create a diagram slide in an
// existing Google Slides presentation. It reads a prompt file describing the
// desired diagram, generates a DiagramSpec via Claude, then creates and
// populates a new slide with the rendered diagram shapes.
//
// Usage:
//
//	go run cmd/designer/main.go --presentation <ID> --prompt <file> [--thumb] [--credentials <creds.json>]
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/kelseyhightower/envconfig"
	"github.com/owulveryck/agentigslide/internal/agent"
	"github.com/owulveryck/agentigslide/internal/agent/designer"
	"github.com/owulveryck/agentigslide/internal/auth"
	"github.com/owulveryck/agentigslide/internal/config"
	"github.com/owulveryck/agentigslide/internal/diagram"
	"github.com/owulveryck/agentigslide/internal/model"
	"github.com/owulveryck/agentigslide/internal/pipeline"
	"github.com/owulveryck/agentigslide/internal/vertex"

	"google.golang.org/api/option"
	"google.golang.org/api/slides/v1"
)

type designerConfig struct {
	Model string `envconfig:"MODEL" default:"claude-sonnet-4-6" desc:"Claude model for the Designer agent"`
}

var (
	presentationID = flag.String("presentation", "", "Google Slides presentation ID (required)")
	promptFile     = flag.String("prompt", "", "Path to prompt file describing the diagram (required)")
	thumbFlag      = flag.Bool("thumb", false, "Extract a PNG thumbnail of the created slide to TMPDIR")
	credentials    = flag.String("credentials", "", "Path to OAuth2 client credentials JSON (optional; uses ADC if omitted)")
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: designer --presentation <ID> --prompt <file> [--thumb] [--credentials <creds.json>]\n\nFlags:\n")
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
			}{"DESIGNER", &designerConfig{}},
		)
	}
	flag.Parse()

	if *presentationID == "" || *promptFile == "" {
		flag.Usage()
		os.Exit(1)
	}

	promptData, err := os.ReadFile(*promptFile)
	if err != nil {
		return fmt.Errorf("reading prompt file: %w", err)
	}
	promptText := strings.TrimSpace(string(promptData))
	if promptText == "" {
		return fmt.Errorf("prompt file is empty")
	}

	var dCfg designerConfig
	if err := envconfig.Process("DESIGNER", &dCfg); err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	slidesCfg, err := config.LoadSlidesConfig()
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	vertexCfg, err := vertex.LoadConfig()
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	ctx := context.Background()

	vc, err := vertex.NewClient(ctx, vertexCfg)
	if err != nil {
		return fmt.Errorf("creating Vertex AI client: %w", err)
	}

	credFile := *credentials
	if credFile == "" {
		credFile = slidesCfg.Credentials
	}
	oauthClient, err := auth.GetOAuthClient(ctx, credFile)
	if err != nil {
		return fmt.Errorf("authentication: %w", err)
	}

	slidesSrv, err := slides.NewService(ctx, option.WithHTTPClient(oauthClient))
	if err != nil {
		return fmt.Errorf("creating Slides service: %w", err)
	}
	slidesAPI := pipeline.WrapSlides(slidesSrv)

	templateInstructions := pipeline.LoadTemplateInstructions(slidesCfg.TemplateDir())

	slideNeed := agent.SlideNeed{
		Intent:    promptText,
		SlideType: "diagram",
	}

	slog.Info("calling designer agent", "model", dCfg.Model)
	agentSpec, usage, err := designer.New(vc, dCfg.Model).DesignDiagram(ctx, slideNeed, templateInstructions)
	if err != nil {
		return fmt.Errorf("designer agent: %w", err)
	}
	slog.Info("designer agent done", "nodes", len(agentSpec.Nodes), "edges", len(agentSpec.Edges), "inputTokens", usage.InputTokens, "outputTokens", usage.OutputTokens)

	modelSpec := agentToModelSpec(agentSpec)

	pageID := fmt.Sprintf("diag_designer_%d", rand.Intn(100000))

	_, err = slidesAPI.BatchUpdate(*presentationID, &slides.BatchUpdatePresentationRequest{
		Requests: []*slides.Request{{
			CreateSlide: &slides.CreateSlideRequest{ObjectId: pageID},
		}},
	})
	if err != nil {
		return fmt.Errorf("creating slide: %w", err)
	}

	pres, err := slidesAPI.GetPresentation(*presentationID)
	if err != nil {
		return fmt.Errorf("reading presentation: %w", err)
	}
	var cleanupReqs []*slides.Request
	for _, page := range pres.Slides {
		if page.ObjectId != pageID {
			continue
		}
		for _, el := range page.PageElements {
			cleanupReqs = append(cleanupReqs, &slides.Request{
				DeleteObject: &slides.DeleteObjectRequest{ObjectId: el.ObjectId},
			})
		}
	}
	if len(cleanupReqs) > 0 {
		_, err = slidesAPI.BatchUpdate(*presentationID, &slides.BatchUpdatePresentationRequest{
			Requests: cleanupReqs,
		})
		if err != nil {
			return fmt.Errorf("cleaning placeholders: %w", err)
		}
	}

	positioned, err := diagram.Layout(modelSpec, pageID)
	if err != nil {
		return fmt.Errorf("diagram layout: %w", err)
	}

	shapeReqs := diagram.Render(positioned)
	if len(shapeReqs) > 0 {
		_, err = slidesAPI.BatchUpdate(*presentationID, &slides.BatchUpdatePresentationRequest{
			Requests: shapeReqs,
		})
		if err != nil {
			return fmt.Errorf("creating diagram shapes: %w", err)
		}
	}

	slog.Info("diagram slide created", "pageID", pageID)

	if *thumbFlag {
		thumbPath, err := exportThumbnail(slidesAPI, *presentationID, pageID)
		if err != nil {
			return fmt.Errorf("exporting thumbnail: %w", err)
		}
		fmt.Println(thumbPath)
	}

	fmt.Printf("https://docs.google.com/presentation/d/%s/edit\n", *presentationID)
	return nil
}

func agentToModelSpec(spec *agent.DiagramSpec) *model.DiagramSpec {
	ms := &model.DiagramSpec{
		Title:      spec.Title,
		LayoutHint: spec.LayoutHint,
	}
	for _, n := range spec.Nodes {
		ms.Nodes = append(ms.Nodes, model.DiagramNode{
			ID: n.ID, Label: n.Label, Shape: n.Shape, Style: n.Style, Size: n.Size,
		})
	}
	for _, e := range spec.Edges {
		ms.Edges = append(ms.Edges, model.DiagramEdge{
			From: e.From, To: e.To, Label: e.Label, LineStyle: e.LineStyle,
		})
	}
	for _, g := range spec.Groups {
		ms.Groups = append(ms.Groups, model.DiagramGroup{
			ID: g.ID, Label: g.Label, Nodes: g.Nodes, Style: g.Style, LayoutHint: g.LayoutHint,
		})
	}
	return ms
}

func exportThumbnail(slidesAPI pipeline.SlidesAPI, presID, pageID string) (string, error) {
	thumb, err := slidesAPI.GetPageThumbnail(presID, pageID)
	if err != nil {
		return "", fmt.Errorf("getting thumbnail: %w", err)
	}

	resp, err := http.Get(thumb.ContentUrl) //nolint:gosec // URL from trusted Google API
	if err != nil {
		return "", fmt.Errorf("downloading thumbnail: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("thumbnail download returned %d", resp.StatusCode)
	}

	tmpDir := os.TempDir()
	outPath := filepath.Join(tmpDir, fmt.Sprintf("designer_%s.png", pageID))

	f, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("creating file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", fmt.Errorf("writing thumbnail: %w", err)
	}

	return outPath, nil
}
