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
	"time"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	htransport "google.golang.org/api/transport/http"
)

// --- Output structs ---

type PresentationPlan struct {
	PresentationTitle string      `json:"presentationTitle"`
	TemplateID        string      `json:"templateId"`
	GeneratedAt       string      `json:"generatedAt"`
	SourceRequest     string      `json:"sourceRequest"`
	Slides            []SlideSpec `json:"slides"`
}

type SlideSpec struct {
	Position          int              `json:"position"`
	SourceSlideNumber int              `json:"sourceSlideNumber"`
	SourceSlideID     string           `json:"sourceSlideId"`
	Intention         string           `json:"intention"`
	Description       string           `json:"description"`
	PreviewImage      string           `json:"previewImage"`
	EditableObjects   []EditableObject `json:"editableObjects"`
	VisualObjects     []VisualObject   `json:"visualObjects,omitempty"`
}

type EditableObject struct {
	ObjectID     string  `json:"objectId"`
	VariableName string  `json:"variableName"`
	Role         string  `json:"role"`
	ElementType  string  `json:"elementType"`
	Placeholder  *string `json:"placeholder"`
	Description  string  `json:"description"`
	Location     string  `json:"location"`
	CurrentValue string  `json:"currentValue"`
	NewValue     *string `json:"newValue,omitempty"`
	Modified     bool    `json:"modified"`
}

type VisualObject struct {
	ObjectID    *string `json:"objectId,omitempty"`
	Type        string  `json:"type"`
	Description string  `json:"description"`
	Purpose     string  `json:"purpose"`
	Reusable    bool    `json:"reusable"`
}

// --- Internal structs for Claude's response ---

type generationPlan struct {
	PresentationTitle string         `json:"presentationTitle"`
	Slides            []slideRequest `json:"slides"`
}

type slideRequest struct {
	SourceSlide   int                `json:"sourceSlide"`
	Modifications []textModification `json:"modifications"`
}

type textModification struct {
	VariableName string `json:"variableName"`
	NewText      string `json:"newText"`
}

// --- Internal structs for template_index.json ---

type templateIndex struct {
	TemplateID string          `json:"templateId"`
	Slides     []templateSlide `json:"slides"`
}

type templateSlide struct {
	SlideNumber    int                    `json:"slideNumber"`
	SlideID        string                 `json:"slideId"`
	Intention      string                 `json:"intention"`
	Keywords       []string               `json:"keywords"`
	EditableFields []editableFieldSummary `json:"editableFields"`
	VisualElements []visualElementSummary `json:"visualElements,omitempty"`
}

type editableFieldSummary struct {
	ObjectID       string  `json:"objectId"`
	Role           string  `json:"role"`
	Placeholder    *string `json:"placeholder"`
	Content        string  `json:"content,omitempty"`
	VariableName   string  `json:"variableName"`
	UpdateFunction string  `json:"updateFunction"`
}

type visualElementSummary struct {
	ObjectID *string `json:"objectId,omitempty"`
	Type     string  `json:"type"`
	Purpose  string  `json:"purpose,omitempty"`
}

// --- Internal structs for analysis.json ---

type slideAnalysis struct {
	SlideNumber      int               `json:"slideNumber"`
	SlideID          string            `json:"slideId"`
	Intention        string            `json:"intention"`
	Description      string            `json:"description"`
	EditableElements []editableElement `json:"editableElements"`
	VisualElements   []visualElement   `json:"visualElements"`
}

type editableElement struct {
	ObjectID    string  `json:"objectId"`
	Type        string  `json:"type"`
	Placeholder *string `json:"placeholder"`
	Content     string  `json:"content"`
	Description string  `json:"description"`
	Location    string  `json:"location"`
}

type visualElement struct {
	ObjectID    *string `json:"objectId,omitempty"`
	Type        string  `json:"type"`
	Description string  `json:"description"`
	Purpose     string  `json:"purpose,omitempty"`
	Reusable    bool    `json:"reusable,omitempty"`
}

func main() {
	interactive := flag.Bool("interactive", false, "Interactive mode (read from stdin)")
	request := flag.String("request", "", "User request for slide generation")
	flag.Parse()

	var userRequest string
	if *interactive {
		fmt.Fprintln(os.Stderr, "Enter your slide generation request:")
		var input bytes.Buffer
		_, _ = io.Copy(&input, os.Stdin)
		userRequest = input.String()
	} else if *request != "" {
		userRequest = *request
	} else {
		log.Fatal("Usage: generate_slide_list --request \"your request\" OR generate_slide_list --interactive")
	}

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

	indexData, err := os.ReadFile("template_index.json")
	if err != nil {
		log.Fatalf("Failed to read template_index.json: %v\nPlease run 'go run buildTemplateIndex/build_template_index.go' first", err)
	}

	var index templateIndex
	if err := json.Unmarshal(indexData, &index); err != nil {
		log.Fatalf("Failed to parse template_index.json: %v", err)
	}

	ctx := context.Background()
	httpClient, err := createGoogleAuthHTTPClient(ctx)
	if err != nil {
		log.Fatalf("Failed to create Google auth HTTP client: %v", err)
	}

	compactIndex := buildCompactIndex(&index)

	plan, err := parseUserRequest(ctx, httpClient, userRequest, compactIndex, projectID, region)
	if err != nil {
		log.Fatalf("Failed to parse user request: %v", err)
	}

	output := enrichPlan(plan, &index, templateID, userRequest)

	result, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal output: %v", err)
	}
	fmt.Println(string(result))
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

func parseUserRequest(ctx context.Context, httpClient *http.Client, userRequest, templateIndexJSON, projectID, region string) (*generationPlan, error) {
	prompt := fmt.Sprintf(`Tu es un expert en création de présentations professionnelles à partir du template OCTO.

RÈGLES FONDAMENTALES :
1. N'INVENTE AUCUNE INFORMATION. Tout le contenu texte doit provenir exclusivement de la demande utilisateur. Si une information n'est pas dans la demande, ne la fabrique pas.
2. ADÉQUATION STRUCTURE/CONTENU : Le choix de chaque slide est dicté par le nombre d'informations à afficher. Compte les éléments de contenu disponibles dans la demande (bullet points, paragraphes, chiffres clés) et choisis une slide dont le nombre de zones éditables correspond. Par exemple : 3 points à afficher → slide avec 3 zones de contenu, PAS une slide avec 6 zones. Ne duplique JAMAIS du contenu pour remplir des zones vides. Préfère une slide plus simple plutôt qu'une slide trop riche avec des champs laissés vides ou répétés.
3. La présentation doit être cohérente et compréhensible : les slides intercalaires (titres de section, séparateurs) doivent être placées entre les parties qu'elles introduisent.
4. L'ordre des slides dans le JSON = l'ordre final dans la présentation.

STRUCTURE ATTENDUE :
- Slide de titre (couverture)
- Pour chaque grande partie : une slide intercalaire de section, puis les slides de contenu
- Slide de conclusion / remerciement / contacts si pertinent

SLIDES DISPONIBLES DANS LE TEMPLATE :
%s

DEMANDE UTILISATEUR :
"""
%s
"""

CONSIGNES POUR LE CONTENU :
- Remplis CHAQUE champ éditable de chaque slide choisie
- Utilise UNIQUEMENT le texte et les informations fournis dans la demande utilisateur
- Pour les champs de type "année" ou "copyright" : utilise 2026
- Pour les numéros de page : ne les inclus pas dans les modifications
- Si la demande ne fournit pas assez de contenu pour remplir un champ, utilise un texte court et neutre en rapport avec le titre de la section (ex: le titre de la partie, ou un tiret)
- Ne génère PAS de bullet points, chiffres ou affirmations qui ne sont pas dans la demande

FORMATAGE MARKDOWN (dans les champs newText) :
- Tu peux utiliser **gras** pour mettre en valeur des mots importants
- Tu peux utiliser *italique* pour des nuances ou termes techniques
- Tu peux utiliser des listes à puces avec - pour structurer le contenu :
  - un seul niveau d'indentation : - item
  - deux niveaux d'indentation :   - sous-item (2 espaces avant le tiret)
- N'utilise PAS d'autres balises markdown (titres #, liens, images, code, etc.)
- Le markdown est optionnel : utilise-le uniquement quand cela améliore la lisibilité

Réponds UNIQUEMENT avec un JSON (pas de texte avant ou après) :
{
  "presentationTitle": "Titre de la présentation",
  "slides": [
    {
      "sourceSlide": 1,
      "modifications": [
        {
          "variableName": "titlemainShape",
          "newText": "Nouveau titre"
        }
      ]
    }
  ]
}

RAPPELS :
- "variableName" doit correspondre exactement à un editableFields.variableName du template
- Tu peux réutiliser la même slide template plusieurs fois avec des contenus différents
- L'ordre des slides est crucial : intercalaire AVANT le contenu de la section
`, templateIndexJSON, userRequest)

	requestBody := map[string]any{
		"anthropic_version": "vertex-2023-10-16",
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "text",
						"text": prompt,
					},
				},
			},
		},
		"max_tokens":  32768,
		"temperature": 0.0,
	}

	reqJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	model := "claude-sonnet-4-5@20250929"
	url := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/anthropic/models/%s:rawPredict",
		region, projectID, region, model)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w\nResponse: %s", err, string(body))
	}

	var responseText string
	for _, block := range apiResp.Content {
		if block.Type == "text" {
			responseText += block.Text
		}
	}

	responseText = strings.TrimSpace(responseText)
	if after, found := strings.CutPrefix(responseText, "```json"); found {
		responseText = strings.TrimSuffix(strings.TrimSpace(after), "```")
	} else if after, found := strings.CutPrefix(responseText, "```"); found {
		responseText = strings.TrimSuffix(strings.TrimSpace(after), "```")
	}

	var plan generationPlan
	if err := json.Unmarshal([]byte(responseText), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse plan: %w\nResponse was: %s", err, responseText)
	}

	return &plan, nil
}

func buildCompactIndex(index *templateIndex) string {
	var b strings.Builder
	for _, slide := range index.Slides {
		fmt.Fprintf(&b, "SLIDE %d: %s\n", slide.SlideNumber, slide.Intention)
		if len(slide.Keywords) > 0 {
			limit := min(len(slide.Keywords), 8)
			fmt.Fprintf(&b, "  mots-clés: %s\n", strings.Join(slide.Keywords[:limit], ", "))
		}
		if len(slide.EditableFields) > 0 {
			fmt.Fprintf(&b, "  champs éditables:\n")
			for _, f := range slide.EditableFields {
				fmt.Fprintf(&b, "    - %s (role: %s", f.VariableName, f.Role)
				if f.Content != "" {
					content := f.Content
					if len(content) > 50 {
						content = content[:50] + "..."
					}
					fmt.Fprintf(&b, ", contenu: %q", content)
				}
				b.WriteString(")\n")
			}
		}
	}
	return b.String()
}

func enrichPlan(plan *generationPlan, index *templateIndex, templateID, userRequest string) *PresentationPlan {
	slidesByNumber := make(map[int]*templateSlide, len(index.Slides))
	for i := range index.Slides {
		slidesByNumber[index.Slides[i].SlideNumber] = &index.Slides[i]
	}

	output := &PresentationPlan{
		PresentationTitle: plan.PresentationTitle,
		TemplateID:        templateID,
		GeneratedAt:       time.Now().UTC().Format(time.RFC3339),
		SourceRequest:     userRequest,
	}

	for i, sr := range plan.Slides {
		ts, ok := slidesByNumber[sr.SourceSlide]
		if !ok {
			log.Printf("Warning: slide %d not found in template index, skipping", sr.SourceSlide)
			continue
		}

		analysis := loadAnalysis(templateID, sr.SourceSlide)

		modsByVar := make(map[string]string, len(sr.Modifications))
		for _, m := range sr.Modifications {
			modsByVar[m.VariableName] = m.NewText
		}

		analysisElementsByID := make(map[string]*editableElement)
		if analysis != nil {
			for j := range analysis.EditableElements {
				analysisElementsByID[analysis.EditableElements[j].ObjectID] = &analysis.EditableElements[j]
			}
		}

		spec := SlideSpec{
			Position:          i + 1,
			SourceSlideNumber: ts.SlideNumber,
			SourceSlideID:     ts.SlideID,
			Intention:         ts.Intention,
			PreviewImage:      fmt.Sprintf("template/%s/%d/slide.png", templateID, ts.SlideNumber),
		}

		if analysis != nil {
			spec.Description = analysis.Description
		}

		for _, field := range ts.EditableFields {
			obj := EditableObject{
				ObjectID:     field.ObjectID,
				VariableName: field.VariableName,
				Role:         field.Role,
				ElementType:  "text",
				Placeholder:  field.Placeholder,
				CurrentValue: field.Content,
			}

			if ae, ok := analysisElementsByID[field.ObjectID]; ok {
				obj.Description = ae.Description
				obj.Location = ae.Location
				obj.ElementType = ae.Type
			}

			if newText, ok := modsByVar[field.VariableName]; ok {
				obj.NewValue = &newText
				obj.Modified = true
			}

			spec.EditableObjects = append(spec.EditableObjects, obj)
		}

		if analysis != nil {
			for _, ve := range analysis.VisualElements {
				spec.VisualObjects = append(spec.VisualObjects, VisualObject(ve))
			}
		} else {
			for _, ve := range ts.VisualElements {
				spec.VisualObjects = append(spec.VisualObjects, VisualObject{
					ObjectID: ve.ObjectID,
					Type:     ve.Type,
					Purpose:  ve.Purpose,
				})
			}
		}

		output.Slides = append(output.Slides, spec)
	}

	return output
}

func loadAnalysis(templateID string, slideNumber int) *slideAnalysis {
	path := fmt.Sprintf("template/%s/%d/analysis.json", templateID, slideNumber)
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Warning: could not load analysis.json for slide %d: %v", slideNumber, err)
		return nil
	}

	var analysis slideAnalysis
	if err := json.Unmarshal(data, &analysis); err != nil {
		log.Printf("Warning: could not parse analysis.json for slide %d: %v", slideNumber, err)
		return nil
	}

	return &analysis
}
