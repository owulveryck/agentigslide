// Command analysis fetches all slides from a Google Slides presentation and
// saves each slide's raw API content as a JSON file and its visual preview as
// a PNG image. It reads the presentation ID from the SLIDES_PREFORMATES_ID
// environment variable and writes output to
// template/{presentationID}/{slideNumber}/.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"google.golang.org/api/option"
	"google.golang.org/api/slides/v1"
)

func main() {
	ctx := context.Background()

	// Récupération de l'ID de la présentation depuis la variable d'environnement
	presentationID := os.Getenv("SLIDES_PREFORMATES_ID")
	if presentationID == "" {
		log.Fatal("La variable d'environnement SLIDES_PREFORMATES_ID doit être définie")
	}

	// Création du service avec les scopes appropriés
	// Utilise les scopes suivants pour avoir accès en lecture aux présentations:
	// - presentations.readonly pour lire les slides
	// - drive.readonly en fallback si nécessaire
	scopes := []string{
		slides.PresentationsReadonlyScope,
		slides.DriveReadonlyScope,
	}

	var opts []option.ClientOption

	if credFile := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); credFile != "" {
		fmt.Printf("Utilisation du fichier de credentials: %s\n", credFile)
	}

	opts = append(opts, option.WithScopes(scopes...))

	srv, err := slides.NewService(ctx, opts...)
	if err != nil {
		log.Fatalf("Impossible de créer le client Slides: %v", err)
	}

	// Récupération de la présentation
	pres, err := srv.Presentations.Get(presentationID).Do()
	if err != nil {
		log.Fatalf("Impossible de récupérer la présentation: %v", err)
	}

	fmt.Printf("Analyse de la présentation : %s (%s)\n", pres.Title, pres.PresentationId)
	fmt.Printf("Nombre de slides : %d\n", len(pres.Slides))
	fmt.Println("==================================================")

	// Création du répertoire de base
	baseDir := fmt.Sprintf("template/%s", pres.PresentationId)

	// Itération sur les slides et sauvegarde dans des fichiers JSON
	for i, slide := range pres.Slides {
		slideNum := i + 1
		fmt.Printf("Traitement de la slide %d/%d (ID: %s)...\n", slideNum, len(pres.Slides), slide.ObjectId)

		// Création du répertoire pour cette slide
		slideDir := fmt.Sprintf("%s/%d", baseDir, slideNum)
		if err := os.MkdirAll(slideDir, 0755); err != nil {
			log.Printf("Erreur lors de la création du répertoire %s: %v", slideDir, err)
			continue
		}

		// Conversion en JSON indenté
		jsonData, err := json.MarshalIndent(slide, "", "  ")
		if err != nil {
			log.Printf("Erreur lors de la conversion JSON pour la slide %d: %v", slideNum, err)
			continue
		}

		// Écriture du fichier content.json
		outputFile := fmt.Sprintf("%s/content.json", slideDir)
		if err := os.WriteFile(outputFile, jsonData, 0644); err != nil {
			log.Printf("Erreur lors de l'écriture du fichier %s: %v", outputFile, err)
			continue
		}

		fmt.Printf("  ✓ Slide %d sauvegardée dans %s\n", slideNum, outputFile)
	}

	fmt.Println("==================================================")
	fmt.Printf("Traitement terminé : %d slides exportées dans %s\n", len(pres.Slides), baseDir)
}
