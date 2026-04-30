// Command analysis fetches all slides from a Google Slides presentation and
// saves each slide's raw API content as a JSON file and its visual preview as
// a PNG image. It reads the presentation ID from the SLIDES_TEMPLATE_ID
// environment variable and writes output to
// template/{presentationID}/{slideNumber}/.
//
// Note: This CLI uses Google ADC (Application Default Credentials). Set
// GOOGLE_APPLICATION_CREDENTIALS for service account auth if needed.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"example.com/internal/config"

	"google.golang.org/api/option"
	"google.golang.org/api/slides/v1"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: analysis\n\nFetches all slides from the template presentation.\n")
		fmt.Fprintf(os.Stderr, "\nNote: Uses Google ADC. Set GOOGLE_APPLICATION_CREDENTIALS for service account auth.\n")
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

	ctx := context.Background()

	scopes := []string{
		slides.PresentationsReadonlyScope,
		slides.DriveReadonlyScope,
	}

	var opts []option.ClientOption
	opts = append(opts, option.WithScopes(scopes...))

	srv, err := slides.NewService(ctx, opts...)
	if err != nil {
		log.Fatalf("Impossible de créer le client Slides: %v", err)
	}

	pres, err := srv.Presentations.Get(slidesCfg.TemplateID).Do()
	if err != nil {
		log.Fatalf("Impossible de récupérer la présentation: %v", err)
	}

	fmt.Printf("Analyse de la présentation : %s (%s)\n", pres.Title, pres.PresentationId)
	fmt.Printf("Nombre de slides : %d\n", len(pres.Slides))
	fmt.Println("==================================================")

	baseDir := fmt.Sprintf("template/%s", pres.PresentationId)

	for i, slide := range pres.Slides {
		slideNum := i + 1
		fmt.Printf("Traitement de la slide %d/%d (ID: %s)...\n", slideNum, len(pres.Slides), slide.ObjectId)

		slideDir := fmt.Sprintf("%s/%d", baseDir, slideNum)
		if err := os.MkdirAll(slideDir, 0755); err != nil {
			log.Printf("Erreur lors de la création du répertoire %s: %v", slideDir, err)
			continue
		}

		jsonData, err := json.MarshalIndent(slide, "", "  ")
		if err != nil {
			log.Printf("Erreur lors de la conversion JSON pour la slide %d: %v", slideNum, err)
			continue
		}

		outputFile := fmt.Sprintf("%s/content.json", slideDir)
		if err := os.WriteFile(outputFile, jsonData, 0644); err != nil {
			log.Printf("Erreur lors de l'écriture du fichier %s: %v", outputFile, err)
			continue
		}

		fmt.Printf("  Slide %d sauvegardée dans %s\n", slideNum, outputFile)
	}

	fmt.Println("==================================================")
	fmt.Printf("Traitement terminé : %d slides exportées dans %s\n", len(pres.Slides), baseDir)
}
