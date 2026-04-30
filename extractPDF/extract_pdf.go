// Command extractPDF extracts each page of a PDF file as a PNG image using
// the pdftoppm command-line tool. It writes each page's image to
// template/{presentationID}/{pageNumber}/slide.png. The pdftoppm tool must
// be installed on the system.
//
// Usage:
//
//	go run extractPDF/extract_pdf.go
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"example.com/internal/config"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: extract_pdf\n\nExtracts PDF pages as PNG images.\n")
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

	pdfFile := "_Slides préformatées OCTO.pdf"
	baseDir := fmt.Sprintf("template/%s", slidesCfg.TemplateID)
	totalPages := 325

	fmt.Printf("Extraction de %d pages du PDF vers %s\n", totalPages, baseDir)
	fmt.Println("==================================================")

	for page := 1; page <= totalPages; page++ {
		slideDir := filepath.Join(baseDir, fmt.Sprintf("%d", page))
		outputFile := filepath.Join(slideDir, "slide.png")

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
