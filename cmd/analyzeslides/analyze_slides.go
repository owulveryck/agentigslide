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
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/owulveryck/agentigslide/internal/config"
	"github.com/owulveryck/agentigslide/internal/model"
	"github.com/owulveryck/agentigslide/internal/vertex"

	"github.com/kelseyhightower/envconfig"
)

//go:embed prompt_analyze.txt
var analyzePromptTemplate string

type analyzeConfig struct {
	Model      string `envconfig:"MODEL" default:"claude-opus-4-6" desc:"Claude model for vision analysis"`
	MaxTokens  int    `envconfig:"MAX_TOKENS" default:"8192" desc:"Maximum tokens in Claude response"`
	MaxRetries int    `envconfig:"MAX_RETRIES" default:"3" desc:"Maximum retries on malformed JSON response"`
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

	baseDir := slidesCfg.TemplateDir()
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
		writeElementSummary(&summary, &elem, "")
	}

	return summary.String()
}

func writeElementSummary(summary *strings.Builder, elem *model.PageElement, indent string) {
	fmt.Fprintf(summary, "%s- ObjectID: %s\n", indent, elem.ObjectID)

	if elem.Shape != nil {
		fmt.Fprintf(summary, "%s  Type: SHAPE (%s)\n", indent, elem.Shape.ShapeType)

		if elem.Shape.Placeholder != nil {
			fmt.Fprintf(summary, "%s  Placeholder: %s\n", indent, elem.Shape.Placeholder.Type)
		}

		if elem.Shape.Text != nil {
			for _, textElem := range elem.Shape.Text.TextElements {
				if textElem.TextRun != nil && textElem.TextRun.Content != "" {
					c := strings.TrimSpace(textElem.TextRun.Content)
					if c != "" {
						fmt.Fprintf(summary, "%s  Text: %q\n", indent, c)
					}
				}
			}
		}

		if elem.Transform != nil {
			fmt.Fprintf(summary, "%s  Position: (%.0f, %.0f)\n", indent, elem.Transform.TranslateX, elem.Transform.TranslateY)
		}
	}

	if elem.Image != nil {
		fmt.Fprintf(summary, "%s  Type: IMAGE\n", indent)
		if elem.Transform != nil {
			fmt.Fprintf(summary, "%s  Position: (%.0f, %.0f)\n", indent, elem.Transform.TranslateX, elem.Transform.TranslateY)
		}
		if elem.Size != nil {
			fmt.Fprintf(summary, "%s  Size: %.0fx%.0f\n", indent, elem.Size.Width.Magnitude, elem.Size.Height.Magnitude)
		}
	}

	if elem.ElementGroup != nil {
		fmt.Fprintf(summary, "%s  Type: GROUP (%d children)\n", indent, len(elem.ElementGroup.Children))
		for i := range elem.ElementGroup.Children {
			writeElementSummary(summary, &elem.ElementGroup.Children[i], indent+"  ")
		}
	}

	fmt.Fprintf(summary, "%s\n", indent)
}

func callClaudeVision(ctx context.Context, vc *vertex.Client, cfg analyzeConfig, imageData []byte, jsonSummary string, slideID string, slideNum int) (*model.SlideAnalysis, error) {
	imageBase64 := base64.StdEncoding.EncodeToString(imageData)

	prompt := fmt.Sprintf(analyzePromptTemplate, jsonSummary)

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

	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		responseText, err := vc.RawPredict(ctx, cfg.Model, messages, vertex.WithMaxTokens(cfg.MaxTokens))
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err
			if attempt == cfg.MaxRetries {
				return nil, fmt.Errorf("claude Vision API call failed after %d retries: %w", cfg.MaxRetries, err)
			}
			delay := time.Duration(3<<attempt) * time.Second
			log.Printf("Slide %d: retry %d/%d — API error: %v (waiting %v)", slideNum, attempt+1, cfg.MaxRetries, err, delay)
			time.Sleep(delay)
			continue
		}

		var visionResp model.VisionResponse
		if err := json.Unmarshal([]byte(responseText), &visionResp); err != nil {
			lastErr = err
			if attempt == cfg.MaxRetries {
				return nil, fmt.Errorf("failed to parse vision response after %d retries: %w\nResponse was: %s", cfg.MaxRetries, err, responseText)
			}

			log.Printf("Slide %d: retry %d/%d — JSON parse error: %v", slideNum, attempt+1, cfg.MaxRetries, err)

			messages = append(messages,
				vertex.Message{
					Role:    "assistant",
					Content: []vertex.ContentBlock{{Type: "text", Text: responseText}},
				},
				vertex.Message{
					Role:    "user",
					Content: []vertex.ContentBlock{{Type: "text", Text: buildRetryFeedback(responseText, err)}},
				},
			)
			continue
		}

		if attempt > 0 {
			log.Printf("Slide %d: retry succeeded on attempt %d", slideNum, attempt+1)
		}
		return &model.SlideAnalysis{
			SlideNumber:      slideNum,
			SlideID:          slideID,
			Intention:        visionResp.Intention,
			Description:      visionResp.Description,
			Category:         visionResp.Category,
			UseCaseTags:      visionResp.UseCaseTags,
			VisualStyle:      visionResp.VisualStyle,
			VisualCaveats:    visionResp.VisualCaveats,
			EditableElements: visionResp.EditableElements,
			VisualElements:   visionResp.VisualElements,
		}, nil
	}

	return nil, fmt.Errorf("failed to parse vision response: %w", lastErr)
}

func buildRetryFeedback(responseText string, parseErr error) string {
	trimmed := strings.TrimSpace(responseText)
	isTruncated := len(trimmed) > 0 &&
		trimmed[len(trimmed)-1] != '}' &&
		trimmed[len(trimmed)-1] != ']'

	var feedback strings.Builder
	feedback.WriteString("Ta réponse précédente n'est pas du JSON valide.\n\n")
	fmt.Fprintf(&feedback, "Erreur de parsing: %s\n\n", parseErr.Error())

	if isTruncated {
		feedback.WriteString("Il semble que ta réponse a été tronquée (elle ne se termine pas par } ou ]).\n")
		feedback.WriteString("Essaie d'être plus concis dans tes descriptions pour que la réponse complète tienne dans la limite de tokens.\n\n")
	}

	feedback.WriteString("Corrige ta réponse et renvoie UNIQUEMENT du JSON valide, sans texte avant ou après. ")
	feedback.WriteString("Le JSON doit respecter exactement le format demandé dans ma question initiale.")

	return feedback.String()
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
			fmt.Fprintf(&md, "- **Position**: %s\n", elem.Location)
			if elem.VariableName != "" {
				fmt.Fprintf(&md, "- **Variable**: `%s`\n", elem.VariableName)
			}
			md.WriteString("\n")
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
