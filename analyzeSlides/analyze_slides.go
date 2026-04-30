// Command analyzeSlides uses Claude Vision via Vertex AI to analyze specific
// slides from a Google Slides template. For each specified slide number, it
// reads the slide image and content JSON, sends them to Claude for structured
// analysis, and writes results as both JSON and markdown files.
//
// Usage:
//
//	go run analyzeSlides/analyze_slides.go --slides 1,2,5,10
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"example.com/internal/config"
	"example.com/internal/model"
	"example.com/internal/vertex"

	"github.com/kelseyhightower/envconfig"
)

type analyzeConfig struct {
	Model     string `envconfig:"MODEL" default:"claude-opus-4-5@20251101" desc:"Claude model for vision analysis"`
	MaxTokens int    `envconfig:"MAX_TOKENS" default:"8192" desc:"Maximum tokens in Claude response"`
}

func main() {
	slidesFlag := flag.String("slides", "", "Comma-separated list of slide numbers to analyze (e.g., 1,2,5,10)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: analyze_slides --slides 1,2,5,10\n\nFlags:\n")
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
			}{"ANALYZE", &analyzeConfig{}},
		)
	}
	flag.Parse()

	if *slidesFlag == "" {
		flag.Usage()
		os.Exit(1)
	}

	slideNumbers := parseSlideNumbers(*slidesFlag)
	if len(slideNumbers) == 0 {
		log.Fatal("No valid slide numbers provided")
	}

	slidesCfg, err := config.LoadSlidesConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	vertexCfg, err := vertex.LoadConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	var azCfg analyzeConfig
	if err := envconfig.Process("ANALYZE", &azCfg); err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	ctx := context.Background()

	vc, err := vertex.NewClient(ctx, vertexCfg)
	if err != nil {
		log.Fatalf("Failed to create Vertex AI client: %v", err)
	}

	baseDir := fmt.Sprintf("template/%s", slidesCfg.TemplateID)
	for _, slideNum := range slideNumbers {
		fmt.Printf("Analyzing slide %d...\n", slideNum)
		if err := analyzeSlide(ctx, vc, azCfg, baseDir, slideNum); err != nil {
			log.Printf("Error analyzing slide %d: %v", slideNum, err)
			continue
		}
		fmt.Printf("Slide %d analyzed successfully\n", slideNum)
	}

	fmt.Println("Analysis completed!")
}

func parseSlideNumbers(input string) []int {
	parts := strings.Split(input, ",")
	var numbers []int
	for _, part := range parts {
		num, err := strconv.Atoi(strings.TrimSpace(part))
		if err == nil && num > 0 {
			numbers = append(numbers, num)
		}
	}
	return numbers
}

func analyzeSlide(ctx context.Context, vc *vertex.Client, cfg analyzeConfig, baseDir string, slideNum int) error {
	slideDir := fmt.Sprintf("%s/%d", baseDir, slideNum)

	// Read content.json
	contentPath := filepath.Join(slideDir, "content.json")
	contentData, err := os.ReadFile(contentPath)
	if err != nil {
		return fmt.Errorf("failed to read content.json: %w", err)
	}

	var slideContent model.SlideContent
	if err := json.Unmarshal(contentData, &slideContent); err != nil {
		return fmt.Errorf("failed to parse content.json: %w", err)
	}

	// Extract text elements summary from JSON
	jsonSummary := extractJSONSummary(&slideContent)

	// Read slide image
	imagePath := filepath.Join(slideDir, "slide.png")
	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		return fmt.Errorf("failed to read slide.png: %w", err)
	}

	analysis, err := callClaudeVision(ctx, vc, cfg, imageData, jsonSummary, slideContent.ObjectID, slideNum)
	if err != nil {
		return fmt.Errorf("failed to call Claude Vision: %w", err)
	}

	// Save analysis.json
	analysisJSON, err := json.MarshalIndent(analysis, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal analysis: %w", err)
	}

	analysisJSONPath := filepath.Join(slideDir, "analysis.json")
	if err := os.WriteFile(analysisJSONPath, analysisJSON, 0644); err != nil {
		return fmt.Errorf("failed to write analysis.json: %w", err)
	}

	// Generate and save analysis.md
	analysisMD := generateMarkdown(analysis)
	analysisMDPath := filepath.Join(slideDir, "analysis.md")
	if err := os.WriteFile(analysisMDPath, []byte(analysisMD), 0644); err != nil {
		return fmt.Errorf("failed to write analysis.md: %w", err)
	}

	return nil
}

func extractJSONSummary(content *model.SlideContent) string {
	var summary strings.Builder
	summary.WriteString("Available objects in this slide:\n\n")

	for _, elem := range content.PageElements {
		fmt.Fprintf(&summary, "- ObjectID: %s\n", elem.ObjectID)

		// Handle shapes
		if elem.Shape != nil {
			fmt.Fprintf(&summary, "  Type: SHAPE (%s)\n", elem.Shape.ShapeType)

			if elem.Shape.Placeholder != nil {
				fmt.Fprintf(&summary, "  Placeholder: %s\n", elem.Shape.Placeholder.Type)
			}

			if elem.Shape.Text != nil {
				for _, textElem := range elem.Shape.Text.TextElements {
					if textElem.TextRun != nil && textElem.TextRun.Content != "" {
						content := strings.TrimSpace(textElem.TextRun.Content)
						if content != "" {
							fmt.Fprintf(&summary, "  Text: %q\n", content)
						}
					}
				}
			}

			if elem.Transform != nil {
				fmt.Fprintf(&summary, "  Position: (%.0f, %.0f)\n", elem.Transform.TranslateX, elem.Transform.TranslateY)
			}
		}

		// Handle images
		if elem.Image != nil {
			summary.WriteString("  Type: IMAGE\n")
			if elem.Transform != nil {
				fmt.Fprintf(&summary, "  Position: (%.0f, %.0f)\n", elem.Transform.TranslateX, elem.Transform.TranslateY)
			}
			if elem.Size != nil {
				fmt.Fprintf(&summary, "  Size: %.0fx%.0f\n", elem.Size.Width.Magnitude, elem.Size.Height.Magnitude)
			}
		}

		// Handle element groups
		if elem.ElementGroup != nil {
			fmt.Fprintf(&summary, "  Type: GROUP (%d children)\n", len(elem.ElementGroup.Children))
		}

		summary.WriteString("\n")
	}

	return summary.String()
}

func callClaudeVision(ctx context.Context, vc *vertex.Client, cfg analyzeConfig, imageData []byte, jsonSummary string, slideID string, slideNum int) (*model.SlideAnalysis, error) {
	imageBase64 := base64.StdEncoding.EncodeToString(imageData)

	prompt := fmt.Sprintf(`Analyse cette slide de présentation de manière exhaustive et détaillée.

OBJECTIF: Créer un catalogue complet de tous les éléments réutilisables de cette slide.

Voici les objets disponibles dans le JSON de cette slide:
%s

ANALYSE REQUISE:

1. **Intention du slide**: Type de contenu (titre, sommaire, diagramme, template, bibliothèque d'icônes, etc.)

2. **Description complète**: Décris tout ce qui est visible de manière détaillée

3. **Éléments modifiables** (textes): Pour chaque texte visible:
   - Mapper au bon objectId en utilisant le texte exact du JSON
   - Indiquer son type et placeholder si applicable
   - Décrire son rôle et sa position

4. **Éléments visuels réutilisables**: Pour CHAQUE élément visuel (images, icônes, formes décoratives, logos, diagrammes):
   - Décrire PRÉCISÉMENT ce que représente l'élément (ex: "icône de fusée bleue et turquoise", "logo OCTO", "photo d'une personne au bureau")
   - Indiquer son objectId si c'est une IMAGE ou un GROUP du JSON
   - Préciser s'il est réutilisable (true/false)
   - Décrire son usage/objectif (ex: "illustration pour concepts d'innovation", "décoration", "identité visuelle")
   - Position approximative

**IMPORTANT pour les images et icônes**:
- Si tu vois des icônes, décris chacune individuellement avec son objectId
- Si c'est un groupe d'icônes, liste-les toutes
- Pour les images, décris ce qu'elles représentent en détail
- Indique toujours l'objectId quand c'est un élément de type IMAGE ou GROUP

Réponds UNIQUEMENT au format JSON suivant (pas de texte avant ou après):
{
  "intention": "Description courte de l'intention",
  "description": "Description exhaustive de tout ce qui est visible",
  "editableElements": [
    {
      "objectId": "ID de l'objet du JSON",
      "type": "text",
      "placeholder": "TITLE ou BODY ou SUBTITLE ou null",
      "content": "Le texte actuel visible",
      "description": "Description du rôle de ce texte",
      "location": "Position précise"
    }
  ],
  "visualElements": [
    {
      "objectId": "ID si c'est une IMAGE ou GROUP",
      "type": "image, icon, shape, logo, diagram, background_image, etc.",
      "description": "Description DÉTAILLÉE de ce que représente l'élément",
      "purpose": "À quoi sert cet élément, dans quel contexte l'utiliser",
      "reusable": true ou false
    }
  ]
}`, jsonSummary)

	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{
			{
				Type: "image",
				Source: &vertex.DataSource{
					Type:      "base64",
					MediaType: "image/png",
					Data:      imageBase64,
				},
			},
			{
				Type: "text",
				Text: prompt,
			},
		},
	}}

	responseText, err := vc.RawPredict(ctx, cfg.Model, messages, vertex.WithMaxTokens(cfg.MaxTokens))
	if err != nil {
		return nil, fmt.Errorf("claude Vision API call failed: %w", err)
	}

	var visionResp model.VisionResponse
	if err := json.Unmarshal([]byte(responseText), &visionResp); err != nil {
		return nil, fmt.Errorf("failed to parse vision response: %w\nResponse was: %s", err, responseText)
	}

	return &model.SlideAnalysis{
		SlideNumber:      slideNum,
		SlideID:          slideID,
		Intention:        visionResp.Intention,
		Description:      visionResp.Description,
		EditableElements: visionResp.EditableElements,
		VisualElements:   visionResp.VisualElements,
	}, nil
}

func generateMarkdown(analysis *model.SlideAnalysis) string {
	var md strings.Builder

	fmt.Fprintf(&md, "# Slide %d: %s\n\n", analysis.SlideNumber, analysis.Intention)
	md.WriteString("## Intention\n")
	fmt.Fprintf(&md, "%s\n\n", analysis.Intention)

	md.WriteString("## Description complète\n")
	fmt.Fprintf(&md, "%s\n\n", analysis.Description)

	if len(analysis.EditableElements) > 0 {
		md.WriteString("## Éléments modifiables (textes)\n\n")
		for i, elem := range analysis.EditableElements {
			fmt.Fprintf(&md, "### %d. %s\n", i+1, elem.Description)
			fmt.Fprintf(&md, "- **Texte actuel**: %q\n", elem.Content)
			fmt.Fprintf(&md, "- **Object ID**: `%s`\n", elem.ObjectID)
			fmt.Fprintf(&md, "- **Type**: %s\n", elem.Type)
			if elem.Placeholder != nil {
				fmt.Fprintf(&md, "- **Placeholder**: %s\n", *elem.Placeholder)
			}
			fmt.Fprintf(&md, "- **Position**: %s\n\n", elem.Location)
		}
	}

	if len(analysis.VisualElements) > 0 {
		md.WriteString("## Éléments visuels réutilisables\n\n")
		md.WriteString("*Ces éléments peuvent être copiés et réutilisés dans d'autres présentations*\n\n")
		for i, elem := range analysis.VisualElements {
			fmt.Fprintf(&md, "### %d. %s\n", i+1, elem.Type)
			if elem.ObjectID != nil {
				fmt.Fprintf(&md, "- **Object ID**: `%s`\n", *elem.ObjectID)
			}
			fmt.Fprintf(&md, "- **Description**: %s\n", elem.Description)
			if elem.Purpose != "" {
				fmt.Fprintf(&md, "- **Utilisation**: %s\n", elem.Purpose)
			}
			if elem.Reusable {
				md.WriteString("- **Réutilisable**: ✅ Oui\n")
			} else {
				md.WriteString("- **Réutilisable**: ❌ Non (spécifique à ce slide)\n")
			}
			md.WriteString("\n")
		}
	}

	return md.String()
}
