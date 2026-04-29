package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	htransport "google.golang.org/api/transport/http"
)

// SlideRequest représente une slide à créer
type SlideRequest struct {
	SourceSlide   int                `json:"sourceSlide"`
	Modifications []TextModification `json:"modifications"`
}

// TextModification représente une modification de texte
type TextModification struct {
	VariableName string `json:"variableName"` // Ex: "titleMainShape", "yearBottomLeftShape"
	NewText      string `json:"newText"`      // Le nouveau texte à insérer
}

// GenerationPlan représente le plan de génération
type GenerationPlan struct {
	PresentationTitle string         `json:"presentationTitle"`
	Slides            []SlideRequest `json:"slides"`
}

func main() {
	interactive := flag.Bool("interactive", false, "Interactive mode (read from stdin)")
	request := flag.String("request", "", "User request for slide generation")
	flag.Parse()

	var userRequest string
	if *interactive {
		fmt.Println("Enter your slide generation request:")
		var input bytes.Buffer
		_, _ = io.Copy(&input, os.Stdin)
		userRequest = input.String()
	} else if *request != "" {
		userRequest = *request
	} else {
		log.Fatal("Usage: generate_appscript --request \"your request\" OR generate_appscript --interactive")
	}

	// Get environment variables
	projectID := os.Getenv("ANTHROPIC_VERTEX_PROJECT_ID")
	if projectID == "" {
		log.Fatal("ANTHROPIC_VERTEX_PROJECT_ID environment variable must be set")
	}

	region := os.Getenv("CLOUD_ML_REGION")
	if region == "" {
		region = "us-east5"
	}

	templateID := os.Getenv("SLIDES_PREFORMATES_ID")
	if templateID == "" {
		log.Fatal("SLIDES_PREFORMATES_ID environment variable must be set")
	}

	// Read template index
	indexData, err := os.ReadFile("template_index.json")
	if err != nil {
		log.Fatalf("Failed to read template_index.json: %v\nPlease run 'go run build_template_index.go' first", err)
	}

	// Create HTTP client with Google credentials
	ctx := context.Background()
	httpClient, err := createGoogleAuthHTTPClient(ctx)
	if err != nil {
		log.Fatalf("Failed to create Google auth HTTP client: %v", err)
	}

	// Call Claude to parse the request
	plan, err := parseUserRequest(ctx, httpClient, userRequest, string(indexData), projectID, region)
	if err != nil {
		log.Fatalf("Failed to parse user request: %v", err)
	}

	// Generate Apps Script code
	code := generateAppsScriptCode(plan, templateID)

	// Output the generated code
	fmt.Println(code)
}

func createGoogleAuthHTTPClient(ctx context.Context) (*http.Client, error) {
	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("failed to find default credentials: %w", err)
	}

	client, _, err := htransport.NewClient(ctx, option.WithCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	return client, nil
}

func parseUserRequest(ctx context.Context, httpClient *http.Client, userRequest, templateIndex, projectID, region string) (*GenerationPlan, error) {
	prompt := fmt.Sprintf(`Tu es un assistant qui aide à créer des présentations Google Slides à partir du template OCTO.

SLIDES DISPONIBLES DANS LE TEMPLATE :
%s

L'utilisateur demande : "%s"

Analyse la demande et génère un plan JSON structuré pour créer la présentation.

Pour chaque slide demandée :
1. Identifie la slide du template qui correspond le mieux (utilise slideNumber)
2. Liste les modifications à apporter en utilisant les noms de variables Apps Script disponibles

IMPORTANT - FORMAT DES VARIABLES :
Dans le template_index, chaque élément éditable a maintenant :
- Une "Variable Apps Script" (ex: "titleMainShape", "yearBottomLeftShape")
- Une "Fonction de mise à jour" (ex: "updateTitleMainShape(newText)")

Tu DOIS utiliser le nom de la variable (sans "Shape") pour identifier l'élément à modifier.

Réponds UNIQUEMENT avec un JSON (pas de texte avant ou après) au format suivant:
{
  "presentationTitle": "Titre de la présentation",
  "slides": [
    {
      "sourceSlide": 1,
      "modifications": [
        {
          "variableName": "titleMainShape",
          "newText": "Nouveau titre"
        },
        {
          "variableName": "yearBottomLeftShape",
          "newText": "2027"
        }
      ]
    }
  ]
}

IMPORTANT :
- "variableName" est le nom de la variable Apps Script (depuis le template_index)
- "newText" est le nouveau texte à mettre
- Ne modifie que les champs pertinents
- Si l'utilisateur ne donne pas de contenu spécifique pour un champ, ne l'inclus pas dans modifications
`, templateIndex, userRequest)

	// Create the request body for Vertex AI
	requestBody := map[string]interface{}{
		"anthropic_version": "vertex-2023-10-16",
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": prompt,
					},
				},
			},
		},
		"max_tokens":  4096,
		"temperature": 0.0,
	}

	// Marshal request body
	reqJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Build Vertex AI URL
	model := "claude-sonnet-4-5@20250929"
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

	// Clean response (remove markdown code blocks if present)
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

	// Parse plan
	var plan GenerationPlan
	if err := json.Unmarshal([]byte(responseText), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse plan: %w\nResponse was: %s", err, responseText)
	}

	return &plan, nil
}

// loadAppsScriptSnippet charge le snippet Apps Script pour une slide
func loadAppsScriptSnippet(slideNumber int, templateID string) (string, error) {
	snippetPath := fmt.Sprintf("template/%s/%d/appscript.js", templateID, slideNumber)
	data, err := os.ReadFile(snippetPath)
	if err != nil {
		return "", fmt.Errorf("failed to read appscript.js for slide %d: %w", slideNumber, err)
	}
	return string(data), nil
}

// escapeJS échappe une chaîne pour JavaScript (guillemets simples)
func escapeJS(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	return s
}

// escapeTemplateLiteral échappe une chaîne pour JavaScript template literals (backticks)
func escapeTemplateLiteral(s string) string {
	// Ordre important: échapper backslash d'abord, puis backtick, puis interpolation
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "`", "\\`")
	s = strings.ReplaceAll(s, "${", "\\${")
	return s
}

// capitalize met en majuscule la première lettre
func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func generateAppsScriptCode(plan *GenerationPlan, templateID string) string {
	var code strings.Builder

	fmt.Fprintf(&code, `/**
 * Génération automatique de présentation Google Slides
 * Titre: %s
 * Généré par: Claude Code
 */

const TEMPLATE_ID = '%s';

`, plan.PresentationTitle, templateID)

	// Inclure tous les snippets helper des slides utilisées
	// On garde une trace des slides déjà incluses pour éviter les doublons
	includedSlides := make(map[int]bool)
	for i, slide := range plan.Slides {
		if !includedSlides[slide.SourceSlide] {
			snippet, err := loadAppsScriptSnippet(slide.SourceSlide, templateID)
			if err != nil {
				log.Printf("Warning: %v", err)
				continue
			}
			fmt.Fprintf(&code, "// ===== Helpers pour Slide %d =====\n", slide.SourceSlide)
			code.WriteString(snippet)
			code.WriteString("\n\n")
			includedSlides[slide.SourceSlide] = true
		}
		_ = i // Avoid unused variable warning
	}

	// Fonction principale
	code.WriteString("function createPresentation() {\n")
	code.WriteString("  const template = SlidesApp.openById(TEMPLATE_ID);\n")
	fmt.Fprintf(&code, "  const newPres = SlidesApp.create('%s');\n", escapeJS(plan.PresentationTitle))
	code.WriteString("  const presId = newPres.getId();\n\n")
	code.WriteString("  Logger.log(`Création de la présentation: ${newPres.getUrl()}`);\n\n")

	// Pour chaque slide
	for i, slide := range plan.Slides {
		fmt.Fprintf(&code, "  // ===== Slide %d: Copie et modification =====\n", i+1)
		fmt.Fprintf(&code, "  const templateSlide%d = template.getSlides()[%d];\n", i, slide.SourceSlide-1)
		fmt.Fprintf(&code, "  Logger.log(`Template slide %d: ${templateSlide%d ? 'OK' : 'undefined'}`);\n\n", i+1, i)
		fmt.Fprintf(&code, "  if (!templateSlide%d) {\n", i)
		fmt.Fprintf(&code, "    throw new Error(`Failed to get template slide %d (index %d)`);\n", i+1, slide.SourceSlide-1)
		code.WriteString("  }\n\n")
		fmt.Fprintf(&code, "  const copiedSlide%d = newPres.appendSlide(templateSlide%d);\n", i, i)
		fmt.Fprintf(&code, "  Logger.log(`Copied slide %d: ${copiedSlide%d ? 'OK' : 'undefined'}`);\n\n", i+1, i)
		fmt.Fprintf(&code, "  if (!copiedSlide%d) {\n", i)
		fmt.Fprintf(&code, "    throw new Error(`Failed to append slide %d`);\n", i+1)
		code.WriteString("  }\n\n")

		// Créer un analyzer scopé pour cette copie
		fmt.Fprintf(&code, "  const slide%dAnalyzer = createSlide%dAnalyzer(copiedSlide%d);\n\n", i, slide.SourceSlide, i)

		// Appliquer les modifications avec les méthodes scopées
		if len(slide.Modifications) > 0 {
			code.WriteString("  // Appliquer les modifications\n")
			for _, mod := range slide.Modifications {
				funcName := "update" + capitalize(mod.VariableName)
				text := escapeTemplateLiteral(mod.NewText)

				fmt.Fprintf(&code, "  slide%dAnalyzer.%s(`%s`);\n", i, funcName, text)
			}
		}

		code.WriteString("\n")
	}

	code.WriteString("  Logger.log(`✓ Présentation créée avec succès!`);\n")
	code.WriteString("  Logger.log(`URL: ${newPres.getUrl()}`);\n")
	code.WriteString("  Logger.log(`ID: ${presId}`);\n\n")
	code.WriteString("  return presId;\n}\n\n")
	code.WriteString("/**\n")
	code.WriteString(" * Fonction helper pour obtenir l'URL de la présentation créée\n")
	code.WriteString(" */\n")
	code.WriteString("function getPresentationUrl(presId) {\n")
	code.WriteString("  return `https://docs.google.com/presentation/d/${presId}/edit`;\n")
	code.WriteString("}\n")

	return code.String()
}
