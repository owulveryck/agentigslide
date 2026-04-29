package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// TemplateIndex représente l'index des slides du template
type TemplateIndex struct {
	TemplateID string          `json:"templateId"`
	Slides     []TemplateSlide `json:"slides"`
}

type TemplateSlide struct {
	SlideNumber    int                    `json:"slideNumber"`
	SlideID        string                 `json:"slideId"`
	Intention      string                 `json:"intention"`
	Keywords       []string               `json:"keywords"`
	EditableFields []EditableFieldSummary `json:"editableFields"`
}

type EditableFieldSummary struct {
	ObjectID    string  `json:"objectId"`
	Role        string  `json:"role"`
	Placeholder *string `json:"placeholder"`
	Content     string  `json:"content,omitempty"`
}

// Global template index
var templateIndex *TemplateIndex

func main() {
	// Load template index
	if err := loadTemplateIndex(); err != nil {
		log.Fatalf("Failed to load template index: %v", err)
	}

	// Create MCP server
	s := server.NewMCPServer(
		"Google Slides Generator",
		"1.0.0",
		server.WithToolCapabilities(true),
		server.WithPromptCapabilities(true),
	)

	// Register tools
	registerTools(s)

	// Register prompts
	registerPrompts(s)

	// Start stdio server
	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func loadTemplateIndex() error {
	// Look for template_index.json in parent directory
	indexPath := "../template_index.json"
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return fmt.Errorf("failed to read template_index.json: %w", err)
	}

	if err := json.Unmarshal(data, &templateIndex); err != nil {
		return fmt.Errorf("failed to parse template_index.json: %w", err)
	}

	log.Printf("Loaded template index with %d slides", len(templateIndex.Slides))
	return nil
}

func registerTools(s *server.MCPServer) {
	// Tool 1: find_template_slide
	s.AddTool(
		mcp.NewTool("find_template_slide",
			mcp.WithDescription("Recherche une slide du template par description/intention"),
			mcp.WithString("query",
				mcp.Required(),
				mcp.Description("Description de la slide recherchée (ex: 'slide de titre', 'sommaire', 'pictos business')"),
			),
		),
		findTemplateSlideHandler,
	)

	// Tool 2: create_presentation
	s.AddTool(
		mcp.NewTool("create_presentation",
			mcp.WithDescription("Crée une nouvelle présentation Google Slides"),
			mcp.WithString("title",
				mcp.Required(),
				mcp.Description("Titre de la présentation"),
			),
		),
		createPresentationHandler,
	)

	// Tool 3: copy_slide_from_template
	s.AddTool(
		mcp.NewTool("copy_slide_from_template",
			mcp.WithDescription("Copie une slide du template vers une présentation"),
			mcp.WithNumber("sourceSlideNumber",
				mcp.Required(),
				mcp.Description("Numéro de la slide dans le template (commence à 1)"),
			),
			mcp.WithString("targetPresentationId",
				mcp.Required(),
				mcp.Description("ID de la présentation cible"),
			),
		),
		copySlideHandler,
	)

	// Tool 4: update_slide_text
	s.AddTool(
		mcp.NewTool("update_slide_text",
			mcp.WithDescription("Modifie le texte d'une slide"),
			mcp.WithString("presentationId",
				mcp.Required(),
				mcp.Description("ID de la présentation"),
			),
			mcp.WithNumber("slideIndex",
				mcp.Required(),
				mcp.Description("Index de la slide (commence à 0)"),
			),
			mcp.WithObject("updates",
				mcp.Required(),
				mcp.Description("Map des modifications: objectId -> nouveau texte"),
			),
		),
		updateSlideTextHandler,
	)
}

func registerPrompts(s *server.MCPServer) {
	s.AddPrompt(
		mcp.NewPrompt("create_deck",
			mcp.WithPromptDescription("Guide pour créer un deck de slides"),
			mcp.WithArgument("user_request",
				mcp.Required(),
				mcp.Description("Description de la présentation souhaitée"),
			),
		),
		createDeckPromptHandler,
	)
}

// Handler for find_template_slide
func findTemplateSlideHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, ok := request.Params.Arguments["query"].(string)
	if !ok {
		return mcp.NewToolResultError("query parameter must be a string"), nil
	}

	// Simple keyword matching (can be improved with embeddings later)
	bestMatch := findBestMatch(query)
	if bestMatch == nil {
		return mcp.NewToolResultError("No matching slide found"), nil
	}

	result := map[string]interface{}{
		"slideNumber": bestMatch.SlideNumber,
		"slideId":     bestMatch.SlideID,
		"intention":   bestMatch.Intention,
		"keywords":    bestMatch.Keywords,
		"fields":      bestMatch.EditableFields,
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return mcp.NewToolResultText(string(resultJSON)), nil
}

// Handler for create_presentation
func createPresentationHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	title, ok := request.Params.Arguments["title"].(string)
	if !ok {
		return mcp.NewToolResultError("title parameter must be a string"), nil
	}

	// TODO: Implement actual Google Slides API call
	// For now, return a mock response
	result := map[string]interface{}{
		"presentationId":  "MOCK_PRESENTATION_ID",
		"presentationUrl": fmt.Sprintf("https://docs.google.com/presentation/d/MOCK_ID/edit"),
		"title":           title,
		"status":          "created (mock)",
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return mcp.NewToolResultText(string(resultJSON)), nil
}

// Handler for copy_slide
func copySlideHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sourceSlideNumber, ok := request.Params.Arguments["sourceSlideNumber"].(float64)
	if !ok {
		return mcp.NewToolResultError("sourceSlideNumber must be a number"), nil
	}

	targetPresentationId, ok := request.Params.Arguments["targetPresentationId"].(string)
	if !ok {
		return mcp.NewToolResultError("targetPresentationId must be a string"), nil
	}

	// TODO: Implement actual Google Slides API call
	result := map[string]interface{}{
		"sourceSlideNumber": int(sourceSlideNumber),
		"targetSlideIndex":  0,
		"targetPresId":      targetPresentationId,
		"status":            "copied (mock)",
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return mcp.NewToolResultText(string(resultJSON)), nil
}

// Handler for update_slide_text
func updateSlideTextHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	presentationId, ok := request.Params.Arguments["presentationId"].(string)
	if !ok {
		return mcp.NewToolResultError("presentationId must be a string"), nil
	}

	slideIndex, ok := request.Params.Arguments["slideIndex"].(float64)
	if !ok {
		return mcp.NewToolResultError("slideIndex must be a number"), nil
	}

	updates, ok := request.Params.Arguments["updates"].(map[string]interface{})
	if !ok {
		return mcp.NewToolResultError("updates must be an object"), nil
	}

	// TODO: Implement actual Google Slides API call
	result := map[string]interface{}{
		"presentationId": presentationId,
		"slideIndex":     int(slideIndex),
		"updatesCount":   len(updates),
		"status":         "updated (mock)",
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return mcp.NewToolResultText(string(resultJSON)), nil
}

// Handler for create_deck prompt
func createDeckPromptHandler(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	userRequest, ok := request.Params.Arguments["user_request"].(string)
	if !ok {
		return nil, fmt.Errorf("user_request parameter is required")
	}

	// Build index summary
	indexSummary := buildIndexSummary()

	promptText := fmt.Sprintf(`Tu es un assistant qui aide à créer des présentations Google Slides à partir du template OCTO.

SLIDES DISPONIBLES DANS LE TEMPLATE:
%s

L'utilisateur demande: "%s"

WORKFLOW RECOMMANDÉ:

1. Analyse la demande et identifie les slides nécessaires avec find_template_slide
2. Crée une nouvelle présentation avec create_presentation
3. Pour chaque slide identifiée:
   a. Copie la slide du template avec copy_slide_from_template
   b. Modifie les textes avec update_slide_text si nécessaire

EXEMPLE:

Pour "Créer un deck 'Innovation' avec une slide de titre":

1. find_template_slide(query="slide de titre")
   → Slide #1 trouvée

2. create_presentation(title="Innovation")
   → Présentation créée, ID: ABC123

3. copy_slide_from_template(sourceSlideNumber=1, targetPresentationId="ABC123")
   → Slide copiée à l'index 0

4. update_slide_text(
     presentationId="ABC123",
     slideIndex=0,
     updates={"g3b4521dbf06_4_0": "Innovation"}
   )
   → Titre modifié

Commence maintenant!
`, indexSummary, userRequest)

	return &mcp.GetPromptResult{
		Messages: []mcp.PromptMessage{
			{
				Role: "user",
				Content: mcp.TextContent{
					Type: "text",
					Text: promptText,
				},
			},
		},
	}, nil
}

// Helper functions

func findBestMatch(query string) *TemplateSlide {
	query = normalizeString(query)
	var bestMatch *TemplateSlide
	bestScore := 0

	for i := range templateIndex.Slides {
		slide := &templateIndex.Slides[i]
		score := 0

		// Check intention
		if containsWords(slide.Intention, query) {
			score += 10
		}

		// Check keywords
		for _, keyword := range slide.Keywords {
			if containsWords(keyword, query) {
				score += 5
			}
		}

		if score > bestScore {
			bestScore = score
			bestMatch = slide
		}
	}

	return bestMatch
}

func buildIndexSummary() string {
	var summary string
	for _, slide := range templateIndex.Slides {
		summary += fmt.Sprintf("- Slide #%d: %s\n", slide.SlideNumber, slide.Intention)
		summary += fmt.Sprintf("  Mots-clés: %v\n", slide.Keywords[:min(5, len(slide.Keywords))])
	}
	return summary
}

func normalizeString(s string) string {
	return strings.ToLower(s)
}

func containsWords(text, query string) bool {
	text = normalizeString(text)
	query = normalizeString(query)
	return strings.Contains(text, query)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
