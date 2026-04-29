package main

import (
	"context"
	"flag"
	"log"
	"os"

	"example.com/internal/auth"
	"example.com/internal/fixfonts"
	"example.com/internal/vertex"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/slides/v1"
)

func main() {
	presentationID := flag.String("presentation", "", "Google Slides presentation ID")
	credentials := flag.String("credentials", "", "Path to OAuth2 client credentials JSON")
	flag.Parse()

	if *presentationID == "" {
		log.Fatal("Usage: fixfonts --presentation <ID> [--credentials <creds.json>]")
	}

	credFile := *credentials
	if credFile == "" {
		credFile = os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	}
	if credFile == "" {
		log.Fatal("Provide --credentials <file> or set GOOGLE_APPLICATION_CREDENTIALS")
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

	vc, err := vertex.NewClient(ctx)
	if err != nil {
		log.Fatalf("Failed to create Vertex AI client: %v", err)
	}

	if err := fixfonts.Run(ctx, slidesSrv, driveSrv, vc, *presentationID); err != nil {
		log.Fatalf("fixfonts failed: %v", err)
	}
}
