// Command formatter detects and corrects formatting inconsistencies in a
// Google Slides presentation using deterministic structural analysis.
// It reads the presentation structure via the Slides API, checks consistency
// of fonts, colors, sizes, spacing, and alignment across all slides, and
// applies corrections through the BatchUpdate API. No LLM is involved.
//
// Usage:
//
//	go run cmd/formatter/main.go --presentation <ID> [--credentials <creds.json>]
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/owulveryck/agentigslide/internal/agent/formatter"
	"github.com/owulveryck/agentigslide/internal/auth"
	"github.com/owulveryck/agentigslide/internal/config"

	"google.golang.org/api/option"
	"google.golang.org/api/slides/v1"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	presentationID := flag.String("presentation", "", "Google Slides presentation ID")
	credentials := flag.String("credentials", "", "Path to OAuth2 client credentials JSON (optional; uses ADC if omitted)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: formatter --presentation <ID> [--credentials <creds.json>]\n\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *presentationID == "" {
		flag.Usage()
		os.Exit(1)
	}

	slidesCfg, err := config.LoadSlidesConfig()
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	credFile := *credentials
	if credFile == "" {
		credFile = slidesCfg.Credentials
	}
	ctx := context.Background()

	oauthClient, err := auth.GetOAuthClient(ctx, credFile)
	if err != nil {
		return fmt.Errorf("failed to get authenticated client: %w", err)
	}

	slidesSrv, err := slides.NewService(ctx, option.WithHTTPClient(oauthClient))
	if err != nil {
		return fmt.Errorf("failed to create Slides service: %w", err)
	}

	f := formatter.New(slidesSrv)
	result, err := f.Run(ctx, *presentationID, nil)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Issues found: %d, Corrections applied: %d\n", len(result.Issues), result.AppliedCount)
	return nil
}
