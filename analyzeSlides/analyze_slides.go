package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	htransport "google.golang.org/api/transport/http"
)

// Structures pour parser le content.json
type SlideContent struct {
	ObjectID     string        `json:"objectId"`
	PageElements []PageElement `json:"pageElements"`
}

type PageElement struct {
	ObjectID     string        `json:"objectId"`
	Shape        *Shape        `json:"shape,omitempty"`
	Image        *Image        `json:"image,omitempty"`
	ElementGroup *ElementGroup `json:"elementGroup,omitempty"`
	Size         *Size         `json:"size,omitempty"`
	Transform    *Transform    `json:"transform,omitempty"`
}

type Image struct {
	ContentURL string `json:"contentUrl,omitempty"`
}

type ElementGroup struct {
	Children []PageElement `json:"children,omitempty"`
}

type Shape struct {
	ShapeType   string       `json:"shapeType,omitempty"`
	Text        *TextContent `json:"text,omitempty"`
	Placeholder *Placeholder `json:"placeholder,omitempty"`
}

type TextContent struct {
	TextElements []TextElement `json:"textElements"`
}

type TextElement struct {
	TextRun *TextRun `json:"textRun,omitempty"`
}

type TextRun struct {
	Content string `json:"content"`
}

type Placeholder struct {
	Type  string `json:"type"`
	Index int    `json:"index,omitempty"`
}

type Size struct {
	Height Magnitude `json:"height"`
	Width  Magnitude `json:"width"`
}

type Magnitude struct {
	Magnitude float64 `json:"magnitude"`
	Unit      string  `json:"unit"`
}

type Transform struct {
	TranslateX float64 `json:"translateX"`
	TranslateY float64 `json:"translateY"`
	ScaleX     float64 `json:"scaleX,omitempty"`
	ScaleY     float64 `json:"scaleY,omitempty"`
	Unit       string  `json:"unit"`
}

// Structures pour l'analyse
type SlideAnalysis struct {
	SlideNumber      int               `json:"slideNumber"`
	SlideID          string            `json:"slideId"`
	Intention        string            `json:"intention"`
	Description      string            `json:"description"`
	EditableElements []EditableElement `json:"editableElements"`
	VisualElements   []VisualElement   `json:"visualElements"`
}

type EditableElement struct {
	ObjectID    string  `json:"objectId"`
	Type        string  `json:"type"`
	Placeholder *string `json:"placeholder"`
	Content     string  `json:"content"`
	Description string  `json:"description"`
	Location    string  `json:"location"`
}

type VisualElement struct {
	ObjectID    *string `json:"objectId,omitempty"`
	Type        string  `json:"type"`
	Description string  `json:"description"`
	Purpose     string  `json:"purpose,omitempty"`
	Reusable    bool    `json:"reusable,omitempty"`
}

// Structure pour la réponse de Claude Vision
type VisionResponse struct {
	Intention        string            `json:"intention"`
	Description      string            `json:"description"`
	EditableElements []EditableElement `json:"editableElements"`
	VisualElements   []VisualElement   `json:"visualElements"`
}

func createGoogleAuthHTTPClient(ctx context.Context) (*http.Client, error) {
	// Get Google default credentials
	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("failed to find default credentials: %w", err)
	}

	// Create an HTTP client with the credentials
	client, _, err := htransport.NewClient(ctx, option.WithCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	return client, nil
}

func main() {
	// Parse arguments
	slidesFlag := flag.String("slides", "", "Comma-separated list of slide numbers to analyze (e.g., 1,2,5,10)")
	flag.Parse()

	if *slidesFlag == "" {
		log.Fatal("Usage: go run analyze_slides.go --slides 1,2,5,10")
	}

	// Parse slide numbers
	slideNumbers := parseSlideNumbers(*slidesFlag)
	if len(slideNumbers) == 0 {
		log.Fatal("No valid slide numbers provided")
	}

	// Get presentation ID from environment
	presentationID := os.Getenv("SLIDES_PREFORMATES_ID")
	if presentationID == "" {
		log.Fatal("La variable d'environnement SLIDES_PREFORMATES_ID doit être définie")
	}

	// Initialize Anthropic client with Vertex AI support
	ctx := context.Background()

	// The Anthropic SDK will automatically use Vertex AI if ANTHROPIC_VERTEX_PROJECT_ID is set
	// along with CLOUD_ML_REGION environment variables
	projectID := os.Getenv("ANTHROPIC_VERTEX_PROJECT_ID")
	if projectID == "" {
		log.Fatal("La variable d'environnement ANTHROPIC_VERTEX_PROJECT_ID doit être définie")
	}

	region := os.Getenv("CLOUD_ML_REGION")
	if region == "" {
		// Set default region for Vertex AI
		_ = os.Setenv("CLOUD_ML_REGION", "us-east5")
		region = "us-east5"
	}

	// Create an HTTP client with Google credentials for Vertex AI
	httpClient, err := createGoogleAuthHTTPClient(ctx)
	if err != nil {
		log.Fatalf("Failed to create Google auth HTTP client: %v", err)
	}

	// Process each slide
	baseDir := fmt.Sprintf("template/%s", presentationID)
	for _, slideNum := range slideNumbers {
		fmt.Printf("Analyzing slide %d...\n", slideNum)
		if err := analyzeSlide(ctx, httpClient, baseDir, slideNum, projectID, region); err != nil {
			log.Printf("Error analyzing slide %d: %v", slideNum, err)
			continue
		}
		fmt.Printf("✓ Slide %d analyzed successfully\n", slideNum)
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

func analyzeSlide(ctx context.Context, httpClient *http.Client, baseDir string, slideNum int, projectID, region string) error {
	slideDir := fmt.Sprintf("%s/%d", baseDir, slideNum)

	// Read content.json
	contentPath := filepath.Join(slideDir, "content.json")
	contentData, err := os.ReadFile(contentPath)
	if err != nil {
		return fmt.Errorf("failed to read content.json: %w", err)
	}

	var slideContent SlideContent
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

	// Call Claude Vision API via Vertex AI
	analysis, err := callClaudeVision(ctx, httpClient, imageData, jsonSummary, slideContent.ObjectID, slideNum, projectID, region)
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
	analysisMD := generateMarkdown(analysis, &slideContent)
	analysisMDPath := filepath.Join(slideDir, "analysis.md")
	if err := os.WriteFile(analysisMDPath, []byte(analysisMD), 0644); err != nil {
		return fmt.Errorf("failed to write analysis.md: %w", err)
	}

	// Generate and save Apps Script snippet
	appsScriptSnippet := generateAppsScriptSnippet(analysis, &slideContent)
	appsScriptPath := filepath.Join(slideDir, "appscript.js")
	if err := os.WriteFile(appsScriptPath, []byte(appsScriptSnippet), 0644); err != nil {
		return fmt.Errorf("failed to write appscript.js: %w", err)
	}

	return nil
}

func extractJSONSummary(content *SlideContent) string {
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

func callClaudeVision(ctx context.Context, httpClient *http.Client, imageData []byte, jsonSummary string, slideID string, slideNum int, projectID, region string) (*SlideAnalysis, error) {
	// Encode image to base64
	imageBase64 := base64.StdEncoding.EncodeToString(imageData)

	// Create prompt
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

	// Create the request body for Vertex AI
	requestBody := map[string]interface{}{
		"anthropic_version": "vertex-2023-10-16",
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "image",
						"source": map[string]string{
							"type":       "base64",
							"media_type": "image/png",
							"data":       imageBase64,
						},
					},
					{
						"type": "text",
						"text": prompt,
					},
				},
			},
		},
		"max_tokens":  8192, // Increased for Claude Opus 4.5 and detailed analysis
		"temperature": 0.0,
	}

	// Marshal request body
	reqJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Build Vertex AI URL
	// Using Claude Opus 4.5 for better analysis quality
	model := "claude-opus-4-5@20251101"
	url := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/anthropic/models/%s:rawPredict",
		region, projectID, region, model)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var apiResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w\nResponse: %s", err, string(body))
	}

	// Extract text from response
	var responseText string
	for _, block := range apiResp.Content {
		if block.Type == "text" {
			responseText += block.Text
		}
	}

	// Try to extract JSON from response (in case there's surrounding text)
	responseText = strings.TrimSpace(responseText)
	if strings.HasPrefix(responseText, "```json") {
		responseText = strings.TrimPrefix(responseText, "```json")
		responseText = strings.TrimSuffix(responseText, "```")
		responseText = strings.TrimSpace(responseText)
	} else if strings.HasPrefix(responseText, "```") {
		responseText = strings.TrimPrefix(responseText, "```")
		responseText = strings.TrimSuffix(responseText, "```")
		responseText = strings.TrimSpace(responseText)
	}

	// Parse vision response
	var visionResp VisionResponse
	if err := json.Unmarshal([]byte(responseText), &visionResp); err != nil {
		return nil, fmt.Errorf("failed to parse vision response: %w\nResponse was: %s", err, responseText)
	}

	// Create final analysis
	analysis := &SlideAnalysis{
		SlideNumber:      slideNum,
		SlideID:          slideID,
		Intention:        visionResp.Intention,
		Description:      visionResp.Description,
		EditableElements: visionResp.EditableElements,
		VisualElements:   visionResp.VisualElements,
	}

	return analysis, nil
}

func generateMarkdown(analysis *SlideAnalysis, slideContent *SlideContent) string {
	var md strings.Builder

	fmt.Fprintf(&md, "# Slide %d: %s\n\n", analysis.SlideNumber, analysis.Intention)
	md.WriteString("## Intention\n")
	fmt.Fprintf(&md, "%s\n\n", analysis.Intention)

	md.WriteString("## Description complète\n")
	fmt.Fprintf(&md, "%s\n\n", analysis.Description)

	if len(analysis.EditableElements) > 0 {
		md.WriteString("## Éléments modifiables (textes)\n\n")
		for i, elem := range analysis.EditableElements {
			varName := generateVariableName(elem, slideContent, analysis)

			fmt.Fprintf(&md, "### %d. %s\n", i+1, elem.Description)
			fmt.Fprintf(&md, "- **Texte actuel**: %q\n", elem.Content)
			fmt.Fprintf(&md, "- **Variable Apps Script**: `%s`\n", varName)
			fmt.Fprintf(&md, "- **Fonction de mise à jour**: `update%s(newText)`\n", capitalize(varName))
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

// ===== Helper functions for Apps Script generation =====

// findPageElementById trouve un PageElement par objectId
func findPageElementById(content *SlideContent, objectId string) *PageElement {
	for i := range content.PageElements {
		if content.PageElements[i].ObjectID == objectId {
			return &content.PageElements[i]
		}
		// Chercher dans les groupes
		if content.PageElements[i].ElementGroup != nil {
			for j := range content.PageElements[i].ElementGroup.Children {
				if content.PageElements[i].ElementGroup.Children[j].ObjectID == objectId {
					return &content.PageElements[i].ElementGroup.Children[j]
				}
			}
		}
	}
	return nil
}

// extractRoleFromDescription extrait le rôle sémantique depuis la description
func extractRoleFromDescription(desc string) string {
	desc = strings.ToLower(desc)

	// Patterns de détection
	if strings.Contains(desc, "titre principal") {
		return "titleMain"
	}
	if strings.Contains(desc, "sous-titre") || strings.Contains(desc, "sous titre") {
		return "subtitle"
	}
	if strings.Contains(desc, "année") || strings.Contains(desc, "annee") {
		return "year"
	}
	if strings.Contains(desc, "entreprise") || strings.Contains(desc, "company") {
		return "company"
	}
	if strings.Contains(desc, "sommaire") {
		return "summary"
	}
	if strings.Contains(desc, "copyright") {
		return "copyright"
	}
	if strings.Contains(desc, "titre") {
		return "title"
	}
	if strings.Contains(desc, "texte") || strings.Contains(desc, "corps") {
		return "text"
	}

	return ""
}

// getSimplePosition convertit une position EMU en position simple
func getSimplePosition(transform *Transform) string {
	// Convertir EMU en position simple
	// 0 = top, 1 = middle, 2 = bottom
	vPos := "Middle"
	if transform.TranslateY < 1500000 {
		vPos = "Top"
	} else if transform.TranslateY > 4500000 {
		vPos = "Bottom"
	}

	// 0 = left, 1 = center, 2 = right
	hPos := "Center"
	if transform.TranslateX < 2000000 {
		hPos = "Left"
	} else if transform.TranslateX > 7000000 {
		hPos = "Right"
	}

	return vPos + hPos
}

// needsPositionSuffix détermine si un élément a besoin d'un suffixe de position
func needsPositionSuffix(elem EditableElement, analysis *SlideAnalysis) bool {
	// Compter combien d'éléments ont le même rôle
	role := extractRoleFromDescription(elem.Description)
	count := 0
	for _, e := range analysis.EditableElements {
		if extractRoleFromDescription(e.Description) == role {
			count++
		}
	}
	return count > 1
}

// toCamelCase convertit une chaîne en camelCase
func toCamelCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == ' ' || r == '-'
	})

	for i := range parts {
		if i == 0 {
			parts[i] = strings.ToLower(parts[i])
		} else {
			if len(parts[i]) > 0 {
				parts[i] = strings.ToUpper(parts[i][:1]) + strings.ToLower(parts[i][1:])
			}
		}
	}

	return strings.Join(parts, "")
}

// capitalize met en majuscule la première lettre
func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// escapeJS échappe une chaîne pour JavaScript
func escapeJS(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	return s
}

// generateVariableName génère un nom de variable intelligent
func generateVariableName(elem EditableElement, slideContent *SlideContent, analysis *SlideAnalysis) string {
	// 1. Extraire le rôle de elem.Description
	role := extractRoleFromDescription(elem.Description)
	if role == "" {
		role = "text"
	}

	// 2. Trouver l'élément dans slideContent pour sa position
	pageElem := findPageElementById(slideContent, elem.ObjectID)

	// 3. Ajouter la position si nécessaire (pour différencier)
	if pageElem != nil && pageElem.Transform != nil && needsPositionSuffix(elem, analysis) {
		position := getSimplePosition(pageElem.Transform)
		role = role + position
	}

	// 4. Convertir en camelCase et ajouter "Shape"
	return toCamelCase(role) + "Shape"
}

// generateDetectionCode génère le code JavaScript de détection
func generateDetectionCode(elem EditableElement, slideContent *SlideContent) string {
	var criteria []string

	// 1. Critère : Type de placeholder
	if elem.Placeholder != nil && *elem.Placeholder != "" {
		criteria = append(criteria, fmt.Sprintf(
			`    const placeholderType = shape.getPlaceholderType();
    if (placeholderType !== SlidesApp.PlaceholderType.%s) return false;`,
			*elem.Placeholder))
	}

	// 2. Critère : Contenu textuel (approximatif)
	if elem.Content != "" {
		// Utiliser le début du contenu pour identifier
		contentStart := elem.Content
		if len(contentStart) > 20 {
			contentStart = contentStart[:20]
		}
		criteria = append(criteria, fmt.Sprintf(
			`    const text = shape.getText();
    if (!text) return false;
    const content = text.asString().trim();
    if (!content.startsWith('%s')) return false;`,
			escapeJS(contentStart)))
	}

	// 3. Critère : Position approximative
	pageElem := findPageElementById(slideContent, elem.ObjectID)
	if pageElem != nil && pageElem.Transform != nil {
		// Convertir EMU en points (1 EMU = 1/914400 inch, 1 inch = 72 points)
		x := pageElem.Transform.TranslateX / 914400 * 72
		y := pageElem.Transform.TranslateY / 914400 * 72

		criteria = append(criteria, fmt.Sprintf(
			`    const pos = shape.getLeft();
    const posY = shape.getTop();
    // Position approximative (tolérance ±20 points)
    if (Math.abs(pos - %.1f) > 20) return false;
    if (Math.abs(posY - %.1f) > 20) return false;`,
			x, y))
	}

	// 4. Critère : Taille approximative
	if pageElem != nil && pageElem.Size != nil {
		width := pageElem.Size.Width.Magnitude / 914400 * 72
		height := pageElem.Size.Height.Magnitude / 914400 * 72

		criteria = append(criteria, fmt.Sprintf(
			`    const width = shape.getWidth();
    const height = shape.getHeight();
    if (Math.abs(width - %.1f) > 10) return false;
    if (Math.abs(height - %.1f) > 10) return false;`,
			width, height))
	}

	criteria = append(criteria, "    return true;")

	return strings.Join(criteria, "\n")
}

// generateAppsScriptSnippet génère le snippet Apps Script pour une slide
func generateAppsScriptSnippet(analysis *SlideAnalysis, slideContent *SlideContent) string {
	var js strings.Builder

	fmt.Fprintf(&js, `/**
 * Apps Script snippet pour Slide %d
 * %s
 *
 * Ce code analyse la slide APRÈS copie pour créer des variables nommées.
 * Les objectId changent lors de la copie, donc on utilise position/type/contenu.
 */

function createSlide%dAnalyzer(copiedSlide) {
  const shapes = copiedSlide.getShapes();
  const elements = {};

`, analysis.SlideNumber, analysis.Intention, analysis.SlideNumber)

	// Pour chaque élément éditable, générer le code de détection
	for _, elem := range analysis.EditableElements {
		varName := generateVariableName(elem, slideContent, analysis)
		detectionCode := generateDetectionCode(elem, slideContent)

		fmt.Fprintf(&js, `  // %s (%s)
  elements.%s = shapes.find(shape => {
%s
  });

`, elem.Description, elem.Location, varName, detectionCode)
	}

	js.WriteString(`  // Retourner un objet avec les éléments ET les méthodes de modification
  return {
    // Accès direct aux shapes
    shapes: elements,

    // Méthodes de modification (scopées à cette copie)
`)

	// Générer les méthodes de modification dans l'objet retourné
	for _, elem := range analysis.EditableElements {
		varName := generateVariableName(elem, slideContent, analysis)
		fmt.Fprintf(&js, `    update%s: function(newText) {
      if (elements.%s && elements.%s.getText()) {
        elements.%s.getText().setText(newText);
      }
    },
`, capitalize(varName), varName, varName, varName)
	}

	js.WriteString(`  };
}
`)

	return js.String()
}
