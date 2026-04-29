package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	pdfFile := "_Slides préformatées OCTO.pdf"
	presentationID := os.Getenv("SLIDES_PREFORMATES_ID")
	if presentationID == "" {
		log.Fatal("La variable d'environnement SLIDES_PREFORMATES_ID doit être définie")
	}

	baseDir := fmt.Sprintf("template/%s", presentationID)
	totalPages := 325

	fmt.Printf("Extraction de %d pages du PDF vers %s\n", totalPages, baseDir)
	fmt.Println("==================================================")

	for page := 1; page <= totalPages; page++ {
		slideDir := filepath.Join(baseDir, fmt.Sprintf("%d", page))
		outputFile := filepath.Join(slideDir, "slide.png")

		// Utilisation de pdftoppm pour extraire la page en PNG
		// -f et -l spécifient la première et dernière page (même numéro = une seule page)
		// -png pour le format PNG
		// -singlefile pour ne pas ajouter de suffixe au nom du fichier
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

		fmt.Printf("  ✓ Page %d/%d extraite vers %s\n", page, totalPages, outputFile)

		// Afficher la progression tous les 25 slides
		if page%25 == 0 {
			fmt.Printf("    [Progression: %d%%]\n", (page*100)/totalPages)
		}
	}

	fmt.Println("==================================================")
	fmt.Printf("Extraction terminée : %d pages extraites\n", totalPages)
}
