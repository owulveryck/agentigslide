// Command fixfonts detects and corrects formatting issues in a Google Slides
// presentation using AI vision analysis. It exports the presentation as PDF,
// analyzes it with Claude Vision via Vertex AI, and applies font size, font
// family, and spacing corrections through the Slides BatchUpdate API.
//
// Usage:
//
//	go run fixfonts/main.go --presentation <ID> [--credentials <creds.json>]
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/owulveryck/agentigslide/internal/auth"
	"github.com/owulveryck/agentigslide/internal/config"
	"github.com/owulveryck/agentigslide/internal/fixfonts"
	"github.com/owulveryck/agentigslide/internal/vertex"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/slides/v1"
)

func main() {
	presentationID := flag.String("presentation", "", "Google Slides presentation ID")
	credentials := flag.String("credentials", "", "Path to OAuth2 client credentials JSON (optional; uses ADC if omitted)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: fixfonts --presentation <ID> [--credentials <creds.json>]\n\nFlags:\n")
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
			}{"FIXFONTS", &fixfonts.Config{}},
		)
	}
	flag.Parse()

	if *presentationID == "" {
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

	ffCfg, err := fixfonts.LoadConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	credFile := *credentials
	if credFile == "" {
		credFile = slidesCfg.Credentials
	}
	ctx := context.Background()

	oauthClient, err := auth.GetOAuthClient(ctx, credFile)
	if err != nil {
		log.Fatalf("Failed to get authenticated client: %v", err)
	}

	slidesSrv, err := slides.NewService(ctx, option.WithHTTPClient(oauthClient))
	if err != nil {
		log.Fatalf("Failed to create Slides service: %v", err)
	}

	driveSrv, err := drive.NewService(ctx, option.WithHTTPClient(oauthClient))
	if err != nil {
		log.Fatalf("Failed to create Drive service: %v", err)
	}

	vc, err := vertex.NewClient(ctx, vertexCfg)
	if err != nil {
		log.Fatalf("Failed to create Vertex AI client: %v", err)
	}

	if err := fixfonts.Run(ctx, slidesSrv, driveSrv, vc, ffCfg, *presentationID, nil); err != nil {
		log.Fatalf("fixfonts failed: %v", err)
	}
}
