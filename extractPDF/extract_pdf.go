// Command extractPDF exports a Google Slides presentation as PDF via the
// Drive API and extracts each page as a PNG image using the pdftoppm
// command-line tool. It writes each page's image to
// template/{presentationID}/{pageNumber}/slide.png.
//
// Usage:
//
//	go run extractPDF/extract_pdf.go [--credentials <creds.json>]
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/owulveryck/slideAppScripter/internal/auth"
	"github.com/owulveryck/slideAppScripter/internal/config"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/slides/v1"
)

func main() {
	credentials := flag.String("credentials", "", "Path to OAuth2 client credentials JSON (overrides SLIDES_CREDENTIALS)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: extract_pdf [--credentials <creds.json>]\n\nExports presentation as PDF via Drive API and extracts pages as PNG images.\n\nFlags:\n")
		flag.PrintDefaults()
		config.PrintAllUsage(
			struct {
				Prefix string
				Spec   any
			}{"SLIDES", &config.SlidesConfig{}},
		)
	}
	flag.Parse()

	slidesCfg, err := config.LoadSlidesConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	credFile := *credentials
	if credFile == "" {
		credFile = slidesCfg.Credentials
	}
	if credFile == "" {
		log.Fatal("Provide --credentials <file> or set SLIDES_CREDENTIALS")
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

	pres, err := slidesSrv.Presentations.Get(slidesCfg.TemplateID).Do()
	if err != nil {
		log.Fatalf("Failed to get presentation: %v", err)
	}
	totalPages := len(pres.Slides)

	fmt.Printf("Export du PDF via Drive API pour la présentation %s\n", slidesCfg.TemplateID)
	resp, err := driveSrv.Files.Export(slidesCfg.TemplateID, "application/pdf").Context(ctx).Download()
	if err != nil {
		log.Fatalf("Failed to export PDF: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	tmpFile, err := os.CreateTemp("", "slides-*.pdf")
	if err != nil {
		log.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		log.Fatalf("Failed to write PDF to temp file: %v", err)
	}
	_ = tmpFile.Close()

	pdfFile := tmpFile.Name()
	baseDir := fmt.Sprintf("template/%s", slidesCfg.TemplateID)

	fmt.Printf("Extraction de %d pages du PDF vers %s\n", totalPages, baseDir)
	fmt.Println("==================================================")

	for page := 1; page <= totalPages; page++ {
		slideDir := filepath.Join(baseDir, fmt.Sprintf("%d", page))
		outputFile := filepath.Join(slideDir, "slide.png")

		if err := os.MkdirAll(slideDir, 0755); err != nil {
			log.Printf("Erreur lors de la création du répertoire %s: %v", slideDir, err)
			continue
		}

		cmd := exec.Command("pdftoppm",
			"-f", fmt.Sprintf("%d", page),
			"-l", fmt.Sprintf("%d", page),
			"-png",
			"-singlefile",
			pdfFile,
			filepath.Join(slideDir, "slide"))

		if err := cmd.Run(); err != nil {
			log.Printf("Erreur lors de l'extraction de la page %d: %v", page, err)
			continue
		}

		fmt.Printf("  Page %d/%d extraite vers %s\n", page, totalPages, outputFile)

		if page%25 == 0 {
			fmt.Printf("    [Progression: %d%%]\n", (page*100)/totalPages)
		}
	}

	fmt.Println("==================================================")
	fmt.Printf("Extraction terminée : %d pages extraites\n", totalPages)
}
