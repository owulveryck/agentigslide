// Command extract fetches a Google Slides presentation and exports its PDF,
// raw slide structure (JSON), and thumbnail images to a local directory.
// It accepts a Google Slides URL or a bare presentation ID as argument.
//
// Usage:
//
//	go run ./cmd/extract/ [--credentials FILE] [--output DIR] [--no-pdf] [--no-thumbnails] [--parallel N] URL_OR_ID
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/owulveryck/agentigslide/internal/auth"
	"github.com/owulveryck/agentigslide/internal/retry"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/slides/v1"
)

func main() {
	var (
		credentials  string
		output       string
		noPDF        bool
		noThumbnails bool
		parallel     int
		urlFlag      string
	)

	flag.StringVar(&credentials, "credentials", "", "path to OAuth2 credentials JSON (fallback: SLIDES_CREDENTIALS env, then ADC)")
	flag.StringVar(&output, "output", "", "output directory (default: /tmp/agentigslide/{ID})")
	flag.StringVar(&output, "o", "", "output directory (shorthand)")
	flag.BoolVar(&noPDF, "no-pdf", false, "skip PDF export")
	flag.BoolVar(&noThumbnails, "no-thumbnails", false, "skip thumbnail downloads")
	flag.IntVar(&parallel, "parallel", 5, "max concurrent goroutines for slide extraction")
	flag.StringVar(&urlFlag, "url", "", "Google Slides URL or presentation ID")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: extract [flags] <google-slides-url-or-id>\n\n")
		fmt.Fprintf(os.Stderr, "Extracts PDF, slide structure, and thumbnails from a Google Slides presentation.\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	input := urlFlag
	if input == "" {
		input = flag.Arg(0)
	}
	if input == "" {
		flag.Usage()
		os.Exit(1)
	}

	presID, err := parsePresentationID(input)
	if err != nil {
		log.Fatalf("Invalid input: %v", err)
	}

	if output == "" {
		output = fmt.Sprintf("/tmp/agentigslide/%s", presID)
	}

	if err := os.MkdirAll(output, 0755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	if credentials == "" {
		credentials = os.Getenv("SLIDES_CREDENTIALS")
	}

	ctx := context.Background()

	httpClient, err := auth.GetOAuthClient(ctx, credentials)
	if err != nil {
		log.Fatalf("Authentication failed: %v", err)
	}

	slidesSrv, err := slides.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		log.Fatalf("Failed to create Slides service: %v", err)
	}

	driveSrv, err := drive.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		log.Fatalf("Failed to create Drive service: %v", err)
	}

	pres, err := retry.DoWithResult(ctx, "GetPresentation", func() (*slides.Presentation, error) {
		return slidesSrv.Presentations.Get(presID).Do()
	})
	if err != nil {
		log.Fatalf("Failed to fetch presentation: %v", err)
	}

	fmt.Fprintf(os.Stderr, "Presentation: %s (%s)\n", pres.Title, pres.PresentationId)
	fmt.Fprintf(os.Stderr, "Slides: %d\n", len(pres.Slides))

	meta := map[string]any{
		"presentationId": pres.PresentationId,
		"title":          pres.Title,
		"locale":         pres.Locale,
		"pageSize":       pres.PageSize,
		"slideCount":     len(pres.Slides),
	}
	metaJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal metadata: %v", err)
	}
	if err := os.WriteFile(fmt.Sprintf("%s/presentation.json", output), metaJSON, 0644); err != nil {
		log.Fatalf("Failed to write presentation.json: %v", err)
	}

	if !noPDF {
		fmt.Fprintf(os.Stderr, "Exporting PDF...\n")
		resp, err := retry.DoWithResult(ctx, "ExportPDF", func() (*http.Response, error) {
			return driveSrv.Files.Export(presID, "application/pdf").Download()
		})
		if err != nil {
			log.Fatalf("Failed to export PDF: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		pdfPath := fmt.Sprintf("%s/presentation.pdf", output)
		f, err := os.Create(pdfPath)
		if err != nil {
			log.Fatalf("Failed to create PDF file: %v", err)
		}
		if _, err := io.Copy(f, resp.Body); err != nil {
			_ = f.Close()
			log.Fatalf("Failed to write PDF: %v", err)
		}
		_ = f.Close()
		fmt.Fprintf(os.Stderr, "PDF saved: %s\n", pdfPath)
	}

	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup

	for i, slide := range pres.Slides {
		wg.Add(1)
		go func(slideNum int, slide *slides.Page) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			slideDir := fmt.Sprintf("%s/%d", output, slideNum)
			if err := os.MkdirAll(slideDir, 0755); err != nil {
				log.Printf("Warning: failed to create directory %s: %v", slideDir, err)
				return
			}

			jsonData, err := json.MarshalIndent(slide, "", "  ")
			if err != nil {
				log.Printf("Warning: failed to marshal slide %d: %v", slideNum, err)
				return
			}
			if err := os.WriteFile(fmt.Sprintf("%s/content.json", slideDir), jsonData, 0644); err != nil {
				log.Printf("Warning: failed to write content.json for slide %d: %v", slideNum, err)
				return
			}

			if !noThumbnails {
				thumbnail, err := retry.DoWithResult(ctx, "GetThumbnail", func() (*slides.Thumbnail, error) {
					return slidesSrv.Presentations.Pages.GetThumbnail(pres.PresentationId, slide.ObjectId).
						ThumbnailPropertiesThumbnailSize("LARGE").
						ThumbnailPropertiesMimeType("PNG").
						Do()
				})
				if err != nil {
					log.Printf("Warning: failed to fetch thumbnail for slide %d: %v", slideNum, err)
					return
				}
				pngPath := fmt.Sprintf("%s/slide.png", slideDir)
				if err := downloadFile(thumbnail.ContentUrl, pngPath); err != nil {
					log.Printf("Warning: failed to download thumbnail for slide %d: %v", slideNum, err)
					return
				}
			}

			fmt.Fprintf(os.Stderr, "  Slide %d/%d extracted\n", slideNum, len(pres.Slides))
		}(i+1, slide)
	}

	wg.Wait()

	fmt.Fprintf(os.Stderr, "Done: %d slides extracted to %s\n", len(pres.Slides), output)
	fmt.Println(output)
}

func parsePresentationID(input string) (string, error) {
	if !strings.Contains(input, "/") {
		if input == "" {
			return "", fmt.Errorf("empty presentation ID")
		}
		return input, nil
	}

	u, err := url.Parse(input)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, p := range parts {
		if p == "d" && i+1 < len(parts) {
			id := parts[i+1]
			if id == "" {
				return "", fmt.Errorf("empty presentation ID in URL")
			}
			return id, nil
		}
	}

	return "", fmt.Errorf("could not find presentation ID in URL path: %s", u.Path)
}

func downloadFile(fileURL, destPath string) error {
	resp, err := http.Get(fileURL) //nolint:gosec // URL comes from Google API response
	if err != nil {
		return fmt.Errorf("HTTP GET failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		return fmt.Errorf("write file: %w", err)
	}

	return f.Close()
}
