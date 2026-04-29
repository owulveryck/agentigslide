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
	"path/filepath"
	"strings"
	"time"

	"example.com/markdown"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/slides/v1"
	htransport "google.golang.org/api/transport/http"
)

// --- Plan structs ---

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
	ObjectID     string        `json:"objectId"`
	VariableName string        `json:"variableName"`
	Role         string        `json:"role"`
	ElementType  string        `json:"elementType"`
	Placeholder  *string       `json:"placeholder"`
	Description  string        `json:"description"`
	Location     string        `json:"location"`
	CurrentValue string        `json:"currentValue"`
	NewValue     *string       `json:"newValue,omitempty"`
	Modified     bool          `json:"modified"`
	CellLocation *CellLocation `json:"cellLocation,omitempty"`
}

type CellLocation struct {
	RowIndex    int `json:"rowIndex"`
	ColumnIndex int `json:"columnIndex"`
}

type VisualObject struct {
	ObjectID    *string `json:"objectId,omitempty"`
	Type        string  `json:"type"`
	Description string  `json:"description"`
	Purpose     string  `json:"purpose"`
	Reusable    bool    `json:"reusable"`
}

// --- Claude response structs ---

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

// --- Template index structs ---

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
	ObjectID       string        `json:"objectId"`
	Role           string        `json:"role"`
	Placeholder    *string       `json:"placeholder"`
	Content        string        `json:"content,omitempty"`
	RawContent     string        `json:"rawContent,omitempty"`
	VariableName   string        `json:"variableName"`
	UpdateFunction string        `json:"updateFunction"`
	CellLocation   *CellLocation `json:"cellLocation,omitempty"`
}

type visualElementSummary struct {
	ObjectID *string `json:"objectId,omitempty"`
	Type     string  `json:"type"`
	Purpose  string  `json:"purpose,omitempty"`
}

// --- Analysis structs ---

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

// --- slideRef tracks objectId mapping for each duplicated slide ---

type slideRef struct {
	pageObjectId string
	elementMap   map[string]string
}

func main() {
	filePath := flag.String("file", "", "Path to markdown file with the presentation request")
	credentials := flag.String("credentials", "", "Path to OAuth2 client credentials JSON (default: GOOGLE_APPLICATION_CREDENTIALS)")
	flag.Parse()

	if *filePath == "" {
		log.Fatal("Usage: slidegen --file <request.md> [--credentials <creds.json>]")
	}

	userRequest, err := os.ReadFile(*filePath)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
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

	// --- Phase 1: Generate plan via Claude (Vertex AI) ---

	log.Println("Generating slide plan via Claude...")
	vertexClient, err := createVertexAIClient(ctx)
	if err != nil {
		log.Fatalf("Failed to create Vertex AI client: %v", err)
	}

	compactIndex := buildCompactIndex(&index)

	genPlan, err := parseUserRequest(ctx, vertexClient, string(userRequest), compactIndex, projectID, region)
	if err != nil {
		log.Fatalf("Failed to generate plan: %v", err)
	}

	plan := enrichPlan(genPlan, &index, templateID, string(userRequest))
	log.Printf("Plan generated: %q with %d slide(s)", plan.PresentationTitle, len(plan.Slides))

	if len(plan.Slides) == 0 {
		log.Fatal("Plan has no slides")
	}

	// --- Phase 2: Create presentation via Google Slides/Drive APIs ---

	credFile := *credentials
	if credFile == "" {
		credFile = os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	}
	if credFile == "" {
		log.Fatal("Provide --credentials <file> or set GOOGLE_APPLICATION_CREDENTIALS")
	}

	slidesClient, err := getOAuthClient(ctx, credFile)
	if err != nil {
		log.Fatalf("Failed to get authenticated client: %v", err)
	}

	slidesSrv, err := slides.NewService(ctx, option.WithHTTPClient(slidesClient))
	if err != nil {
		log.Fatalf("Failed to create Slides service: %v", err)
	}

	driveSrv, err := drive.NewService(ctx, option.WithHTTPClient(slidesClient))
	if err != nil {
		log.Fatalf("Failed to create Drive service: %v", err)
	}

	url, err := executePlan(ctx, plan, slidesSrv, driveSrv)
	if err != nil {
		log.Fatalf("Failed to execute plan: %v", err)
	}

	fmt.Println(url)
}

// --- Vertex AI authentication (for Claude API) ---

func createVertexAIClient(ctx context.Context) (*http.Client, error) {
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

// --- Google Slides/Drive OAuth2 authentication ---

func getOAuthClient(ctx context.Context, credentialsFile string) (*http.Client, error) {
	b, err := os.ReadFile(credentialsFile)
	if err != nil {
		return nil, fmt.Errorf("unable to read credentials file: %w", err)
	}

	scopes := []string{drive.DriveScope, slides.PresentationsScope}

	config, err := google.ConfigFromJSON(b, scopes...)
	if err == nil {
		tokenFile := tokenCachePath()
		tok, err := tokenFromFile(tokenFile)
		if err != nil {
			tok, err = getTokenFromWeb(config)
			if err != nil {
				return nil, err
			}
			if err := saveToken(tokenFile, tok); err != nil {
				log.Printf("Warning: failed to save token: %v", err)
			}
		}
		return config.Client(ctx, tok), nil
	}

	creds, err := google.CredentialsFromJSON(ctx, b, scopes...)
	if err != nil {
		return nil, fmt.Errorf("unable to parse credentials: %w", err)
	}
	return oauth2.NewClient(ctx, creds.TokenSource), nil
}

func tokenCachePath() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".credentials")
	_ = os.MkdirAll(dir, 0700)
	return filepath.Join(dir, "slideappscripter-token.json")
}

func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

func getTokenFromWeb(config *oauth2.Config) (*oauth2.Token, error) {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	fmt.Fprintf(os.Stderr, "Go to the following link in your browser then type the authorization code:\n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		return nil, fmt.Errorf("unable to read authorization code: %w", err)
	}

	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve token from web: %w", err)
	}
	return tok, nil
}

func saveToken(path string, token *oauth2.Token) error {
	fmt.Fprintf(os.Stderr, "Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return json.NewEncoder(f).Encode(token)
}

// --- Plan generation (Claude via Vertex AI) ---

func parseUserRequest(ctx context.Context, httpClient *http.Client, userRequest, templateIndexJSON, projectID, region string) (*generationPlan, error) {
	prompt := fmt.Sprintf(`Tu es un expert en création de présentations professionnelles à partir du template OCTO.

RÈGLES FONDAMENTALES :
1. N'INVENTE AUCUNE INFORMATION. Tout le contenu texte doit provenir exclusivement de la demande utilisateur. Si une information n'est pas dans la demande, ne la fabrique pas.
2. ADÉQUATION STRUCTURE/CONTENU : Le choix de chaque slide est dicté par le nombre d'informations à afficher. Compte les éléments de contenu disponibles dans la demande (bullet points, paragraphes, chiffres clés) et choisis une slide dont le nombre de zones éditables correspond. Par exemple : 3 points à afficher → slide avec 3 zones de contenu, PAS une slide avec 6 zones. Ne duplique JAMAIS du contenu pour remplir des zones vides. Préfère une slide plus simple plutôt qu'une slide trop riche avec des champs laissés vides ou répétés.
3. ANTI-DUPLICATION : Chaque texte de la demande ne doit apparaître qu'UNE SEULE FOIS dans toute la présentation. Ne mets JAMAIS le même texte (même reformulé) dans deux champs différents d'une même slide. Si une slide a plus de zones de contenu que de contenus disponibles, choisis une slide plus simple avec moins de zones. Le nombre entre crochets [N champs de contenu] t'aide à comparer avec le nombre d'éléments à placer.
4. La présentation doit être cohérente et compréhensible : les slides intercalaires (titres de section, séparateurs) doivent être placées entre les parties qu'elles introduisent.
5. L'ordre des slides dans le JSON = l'ordre final dans la présentation.
6. EXHAUSTIVITÉ : Chaque section et sous-section de la demande utilisateur doit avoir au moins une slide dédiée. Ne saute aucune partie du contenu fourni. Si la demande contient 4 étapes, génère 4 slides de contenu (pas 2 ou 3).

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

	model := "claude-opus-4-6"
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

func isContentField(role string) bool {
	switch role {
	case "annee", "copyright", "entreprise", "numero_page", "page":
		return false
	}
	return true
}

func buildCompactIndex(index *templateIndex) string {
	var b strings.Builder
	for _, slide := range index.Slides {
		contentFields := 0
		for _, f := range slide.EditableFields {
			if isContentField(f.Role) {
				contentFields++
			}
		}
		fmt.Fprintf(&b, "SLIDE %d [%d champs de contenu]: %s\n", slide.SlideNumber, contentFields, slide.Intention)
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
			currentValue := field.RawContent
			if currentValue == "" {
				currentValue = field.Content
			}
			obj := EditableObject{
				ObjectID:     field.ObjectID,
				VariableName: field.VariableName,
				Role:         field.Role,
				ElementType:  "text",
				Placeholder:  field.Placeholder,
				CurrentValue: currentValue,
				CellLocation: field.CellLocation,
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

		deduplicateModifications(&spec)
		output.Slides = append(output.Slides, spec)
	}

	return output
}

func deduplicateModifications(spec *SlideSpec) {
	seen := make(map[string]string)
	for i := range spec.EditableObjects {
		obj := &spec.EditableObjects[i]
		if !obj.Modified || obj.NewValue == nil {
			continue
		}
		text := strings.TrimSpace(*obj.NewValue)
		if len(text) <= 3 {
			continue
		}
		if firstVar, exists := seen[text]; exists {
			log.Printf("Warning: duplicate text %q in slide %d (keeping %s, removing from %s)",
				text, spec.SourceSlideNumber, firstVar, obj.VariableName)
			obj.NewValue = nil
			obj.Modified = false
		} else {
			seen[text] = obj.VariableName
		}
	}
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

// --- Plan execution (Google Slides/Drive APIs) ---

func executePlan(ctx context.Context, plan *PresentationPlan, slidesSrv *slides.Service, driveSrv *drive.Service) (string, error) {
	log.Printf("Copying template %s...", plan.TemplateID)
	copiedFile, err := driveSrv.Files.Copy(plan.TemplateID, &drive.File{
		Name:    plan.PresentationTitle,
		Parents: []string{"root"},
	}).SupportsAllDrives(true).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("failed to copy template: %w", err)
	}
	presId := copiedFile.Id
	log.Printf("Created presentation: %s", presId)

	pres, err := slidesSrv.Presentations.Get(presId).Do()
	if err != nil {
		return "", fmt.Errorf("failed to get presentation: %w", err)
	}

	pageMap := make(map[string]*slides.Page, len(pres.Slides))
	for _, page := range pres.Slides {
		pageMap[page.ObjectId] = page
	}

	refs := make([]slideRef, 0, len(plan.Slides))
	dupCounter := 0

	for _, spec := range plan.Slides {
		srcId := spec.SourceSlideID
		srcPage, ok := pageMap[srcId]
		if !ok {
			log.Printf("Warning: slide %d (id=%s) not found in presentation, skipping", spec.SourceSlideNumber, srcId)
			continue
		}

		dupCounter++
		objectIds := make(map[string]string)
		newPageId := fmt.Sprintf("d%d_%s", dupCounter, srcId)
		objectIds[srcId] = newPageId

		for _, elId := range collectElementIds(srcPage) {
			objectIds[elId] = fmt.Sprintf("d%d_%s", dupCounter, elId)
		}

		log.Printf("Duplicating slide %d/%d (source: %d)...", dupCounter, len(plan.Slides), spec.SourceSlideNumber)
		_, err := slidesSrv.Presentations.BatchUpdate(presId, &slides.BatchUpdatePresentationRequest{
			Requests: []*slides.Request{{
				DuplicateObject: &slides.DuplicateObjectRequest{
					ObjectId:  srcId,
					ObjectIds: objectIds,
				},
			}},
		}).Do()
		if err != nil {
			return "", fmt.Errorf("failed to duplicate slide %d: %w", spec.SourceSlideNumber, err)
		}

		refs = append(refs, slideRef{pageObjectId: newPageId, elementMap: objectIds})
	}

	var deleteRequests []*slides.Request
	for _, page := range pres.Slides {
		deleteRequests = append(deleteRequests, &slides.Request{
			DeleteObject: &slides.DeleteObjectRequest{
				ObjectId: page.ObjectId,
			},
		})
	}

	if len(deleteRequests) > 0 {
		log.Printf("Deleting %d original template slide(s)...", len(deleteRequests))
		_, err := slidesSrv.Presentations.BatchUpdate(presId, &slides.BatchUpdatePresentationRequest{
			Requests: deleteRequests,
		}).Do()
		if err != nil {
			return "", fmt.Errorf("failed to delete original slides: %w", err)
		}
	}

	// Reorder slides to match plan order.
	// DuplicateObject places copies next to sources, not at the end.
	// Moving each slide to position 0 in reverse plan order produces the correct order.
	var reorderRequests []*slides.Request
	for i := len(refs) - 1; i >= 0; i-- {
		reorderRequests = append(reorderRequests, &slides.Request{
			UpdateSlidesPosition: &slides.UpdateSlidesPositionRequest{
				SlideObjectIds:  []string{refs[i].pageObjectId},
				InsertionIndex:  0,
				ForceSendFields: []string{"InsertionIndex"},
			},
		})
	}

	if len(reorderRequests) > 0 {
		log.Printf("Reordering %d slide(s)...", len(refs))
		_, err := slidesSrv.Presentations.BatchUpdate(presId, &slides.BatchUpdatePresentationRequest{
			Requests: reorderRequests,
		}).Do()
		if err != nil {
			return "", fmt.Errorf("failed to reorder slides: %w", err)
		}
	}

	var updateRequests []*slides.Request
	for i, spec := range plan.Slides {
		if i >= len(refs) {
			break
		}
		ref := refs[i]
		for _, obj := range spec.EditableObjects {
			if !obj.Modified || obj.NewValue == nil || obj.ObjectID == "" {
				continue
			}
			actualId := ref.elementMap[obj.ObjectID]
			if actualId == "" {
				actualId = obj.ObjectID
			}

			if obj.CellLocation != nil {
				cellLoc := &slides.TableCellLocation{
					RowIndex:    int64(obj.CellLocation.RowIndex),
					ColumnIndex: int64(obj.CellLocation.ColumnIndex),
				}
				if obj.CurrentValue != "" {
					updateRequests = append(updateRequests, &slides.Request{
						DeleteText: &slides.DeleteTextRequest{
							ObjectId:     actualId,
							CellLocation: cellLoc,
							TextRange: &slides.Range{
								Type: "ALL",
							},
						},
					})
				}
				updateRequests = append(updateRequests, markdown.InsertMarkdownContentInCell(*obj.NewValue, actualId, cellLoc)...)
			} else {
				if obj.CurrentValue != "" {
					updateRequests = append(updateRequests, &slides.Request{
						DeleteText: &slides.DeleteTextRequest{
							ObjectId: actualId,
							TextRange: &slides.Range{
								Type: "ALL",
							},
						},
					})
				}
				updateRequests = append(updateRequests, markdown.InsertMarkdownContent(*obj.NewValue, actualId)...)
			}
		}
	}
	markdown.SortRequests(updateRequests)

	if len(updateRequests) > 0 {
		log.Printf("Updating text in %d element(s)...", len(updateRequests))
		_, err := slidesSrv.Presentations.BatchUpdate(presId, &slides.BatchUpdatePresentationRequest{
			Requests: updateRequests,
		}).Do()
		if err != nil {
			return "", fmt.Errorf("failed to update text content: %w", err)
		}
	}

	url := fmt.Sprintf("https://docs.google.com/presentation/d/%s/edit", presId)
	log.Printf("Presentation created successfully: %s", url)
	return url, nil
}

func collectElementIds(page *slides.Page) []string {
	var ids []string
	for _, el := range page.PageElements {
		ids = append(ids, collectPageElementIds(el)...)
	}
	return ids
}

func collectPageElementIds(el *slides.PageElement) []string {
	ids := []string{el.ObjectId}
	if el.ElementGroup != nil {
		for _, child := range el.ElementGroup.Children {
			ids = append(ids, collectPageElementIds(child)...)
		}
	}
	return ids
}
