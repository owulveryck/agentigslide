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
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/slides/v1"
	htransport "google.golang.org/api/transport/http"
)

// --- Structure extraction types (sent to Claude) ---

type SlideInfo struct {
	SlideIndex int           `json:"slideIndex"`
	PageID     string        `json:"pageId"`
	Elements   []ElementInfo `json:"elements"`
}

type ElementInfo struct {
	ObjectID        string          `json:"objectId"`
	ShapeType       string          `json:"shapeType,omitempty"`
	PlaceholderType string          `json:"placeholderType,omitempty"`
	BoundingBox     BoundingBox     `json:"boundingBox"`
	TextRuns        []TextRunInfo   `json:"textRuns"`
	Paragraphs      []ParagraphInfo `json:"paragraphs"`
	CellLocation    *CellRef        `json:"cellLocation,omitempty"`
}

type BoundingBox struct {
	WidthPt  float64 `json:"widthPt"`
	HeightPt float64 `json:"heightPt"`
	LeftPt   float64 `json:"leftPt"`
	TopPt    float64 `json:"topPt"`
}

type TextRunInfo struct {
	StartIndex int     `json:"startIndex"`
	EndIndex   int     `json:"endIndex"`
	Content    string  `json:"content"`
	FontFamily string  `json:"fontFamily,omitempty"`
	FontSizePt float64 `json:"fontSizePt,omitempty"`
	Bold       bool    `json:"bold,omitempty"`
	Italic     bool    `json:"italic,omitempty"`
}

type ParagraphInfo struct {
	StartIndex   int     `json:"startIndex"`
	EndIndex     int     `json:"endIndex"`
	LineSpacing  float64 `json:"lineSpacing,omitempty"`
	SpaceAbovePt float64 `json:"spaceAbovePt,omitempty"`
	SpaceBelowPt float64 `json:"spaceBelowPt,omitempty"`
}

type CellRef struct {
	RowIndex    int `json:"rowIndex"`
	ColumnIndex int `json:"columnIndex"`
}

// --- Correction types (Claude's output) ---

type CorrectionPlan struct {
	Corrections []Correction `json:"corrections"`
}

type Correction struct {
	ObjectID     string   `json:"objectId"`
	SlideIndex   int      `json:"slideIndex"`
	CellLocation *CellRef `json:"cellLocation,omitempty"`
	Reason       string   `json:"reason"`
	Type         string   `json:"type"` // "textStyle" or "paragraphStyle"

	// textStyle fields
	StartIndex *int     `json:"startIndex,omitempty"`
	EndIndex   *int     `json:"endIndex,omitempty"`
	FontSizePt *float64 `json:"fontSizePt,omitempty"`
	FontFamily *string  `json:"fontFamily,omitempty"`

	// paragraphStyle fields
	LineSpacing  *float64 `json:"lineSpacing,omitempty"`
	SpaceAbovePt *float64 `json:"spaceAbovePt,omitempty"`
	SpaceBelowPt *float64 `json:"spaceBelowPt,omitempty"`
}

func main() {
	presentationID := flag.String("presentation", "", "Google Slides presentation ID")
	credentials := flag.String("credentials", "", "Path to OAuth2 client credentials JSON")
	flag.Parse()

	if *presentationID == "" {
		log.Fatal("Usage: fixfonts --presentation <ID> [--credentials <creds.json>]")
	}

	projectID := os.Getenv("ANTHROPIC_VERTEX_PROJECT_ID")
	if projectID == "" {
		log.Fatal("ANTHROPIC_VERTEX_PROJECT_ID environment variable must be set")
	}

	region := os.Getenv("CLOUD_ML_REGION")
	if region == "" {
		region = "us-east5"
	}

	credFile := *credentials
	if credFile == "" {
		credFile = os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	}
	if credFile == "" {
		log.Fatal("Provide --credentials <file> or set GOOGLE_APPLICATION_CREDENTIALS")
	}

	ctx := context.Background()

	oauthClient, err := getOAuthClient(ctx, credFile)
	if err != nil {
		log.Fatalf("Failed to get authenticated client: %v", err)
	}

	slidesSrv, err := slides.NewService(ctx, option.WithHTTPClient(oauthClient))
	if err != nil {
		log.Fatalf("Failed to create Slides service: %v", err)
	}

	driveSrv, err := drive.NewService(ctx, option.WithHTTPClient(oauthClient))
	if err != nil {
		log.Fatalf("Failed to create Drive service: %v", err)
	}

	vertexClient, err := createVertexAIClient(ctx)
	if err != nil {
		log.Fatalf("Failed to create Vertex AI client: %v", err)
	}

	// Step 1: Export PDF via Drive API
	log.Println("Exporting presentation as PDF...")
	pdfData, err := exportPDF(ctx, driveSrv, *presentationID)
	if err != nil {
		log.Fatalf("Failed to export PDF: %v", err)
	}
	log.Printf("PDF exported: %d bytes", len(pdfData))

	// Step 2: Fetch presentation structure via Slides API
	log.Println("Fetching presentation structure...")
	pres, err := slidesSrv.Presentations.Get(*presentationID).Do()
	if err != nil {
		log.Fatalf("Failed to get presentation: %v", err)
	}
	structure := extractStructure(pres)
	log.Printf("Extracted structure: %d slide(s)", len(structure))

	// Step 3: Analyze with Claude
	log.Println("Analyzing formatting with Claude Opus...")
	correctionPlan, err := analyzeWithClaude(ctx, vertexClient, pdfData, structure, projectID, region)
	if err != nil {
		log.Fatalf("Failed to analyze with Claude: %v", err)
	}

	// Step 4: Validate and apply corrections
	if len(correctionPlan.Corrections) == 0 {
		log.Println("No formatting issues found.")
		return
	}

	log.Printf("Found %d formatting issue(s):", len(correctionPlan.Corrections))
	for _, c := range correctionPlan.Corrections {
		log.Printf("  - [slide %d] %s: %s", c.SlideIndex, c.ObjectID, c.Reason)
	}

	validCorrections := validateCorrections(correctionPlan, structure)
	if len(validCorrections) == 0 {
		log.Println("All corrections were invalid after validation. Nothing to apply.")
		return
	}

	requests := buildCorrections(validCorrections)
	log.Printf("Applying %d correction request(s)...", len(requests))
	if err := applyCorrections(ctx, slidesSrv, *presentationID, requests); err != nil {
		log.Fatalf("Failed to apply corrections: %v", err)
	}

	log.Println("Formatting corrections applied successfully.")
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

// --- Vertex AI authentication ---

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

// --- PDF export via Drive API ---

func exportPDF(ctx context.Context, driveSrv *drive.Service, presentationID string) ([]byte, error) {
	resp, err := driveSrv.Files.Export(presentationID, "application/pdf").Context(ctx).Download()
	if err != nil {
		return nil, fmt.Errorf("failed to export PDF: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read PDF response: %w", err)
	}

	return data, nil
}

// --- Structure extraction from Slides API ---

const emuToPoints = 12700.0

func extractStructure(pres *slides.Presentation) []SlideInfo {
	var result []SlideInfo

	for i, page := range pres.Slides {
		slide := SlideInfo{
			SlideIndex: i,
			PageID:     page.ObjectId,
		}

		for _, el := range page.PageElements {
			extractElement(&slide, el, nil)
		}

		if len(slide.Elements) > 0 {
			result = append(result, slide)
		}
	}

	return result
}

func extractElement(slide *SlideInfo, el *slides.PageElement, cellLoc *CellRef) {
	bb := computeBoundingBox(el)

	if el.Shape != nil && el.Shape.Text != nil {
		elem := ElementInfo{
			ObjectID:     el.ObjectId,
			BoundingBox:  bb,
			CellLocation: cellLoc,
		}

		if el.Shape.ShapeType != "" {
			elem.ShapeType = el.Shape.ShapeType
		}
		if el.Shape.Placeholder != nil {
			elem.PlaceholderType = el.Shape.Placeholder.Type
		}

		extractTextElements(el.Shape.Text, &elem)

		if len(elem.TextRuns) > 0 {
			slide.Elements = append(slide.Elements, elem)
		}
	}

	if el.Table != nil {
		for rowIdx, row := range el.Table.TableRows {
			for colIdx, cell := range row.TableCells {
				if cell.Text == nil {
					continue
				}
				ref := &CellRef{RowIndex: rowIdx, ColumnIndex: colIdx}
				elem := ElementInfo{
					ObjectID:     el.ObjectId,
					ShapeType:    "TABLE_CELL",
					BoundingBox:  bb,
					CellLocation: ref,
				}
				extractTextElements(cell.Text, &elem)
				if len(elem.TextRuns) > 0 {
					slide.Elements = append(slide.Elements, elem)
				}
			}
		}
	}

	if el.ElementGroup != nil {
		for _, child := range el.ElementGroup.Children {
			extractElement(slide, child, cellLoc)
		}
	}
}

func computeBoundingBox(el *slides.PageElement) BoundingBox {
	var bb BoundingBox
	if el.Size != nil {
		if el.Size.Width != nil {
			bb.WidthPt = el.Size.Width.Magnitude / emuToPoints
		}
		if el.Size.Height != nil {
			bb.HeightPt = el.Size.Height.Magnitude / emuToPoints
		}
	}
	if el.Transform != nil {
		bb.LeftPt = el.Transform.TranslateX / emuToPoints
		bb.TopPt = el.Transform.TranslateY / emuToPoints
	}
	return bb
}

func extractTextElements(text *slides.TextContent, elem *ElementInfo) {
	for _, te := range text.TextElements {
		startIdx := int(te.StartIndex)
		endIdx := int(te.EndIndex)

		if te.TextRun != nil {
			tr := TextRunInfo{
				StartIndex: startIdx,
				EndIndex:   endIdx,
				Content:    te.TextRun.Content,
			}
			if te.TextRun.Style != nil {
				if te.TextRun.Style.FontFamily != "" {
					tr.FontFamily = te.TextRun.Style.FontFamily
				}
				if te.TextRun.Style.FontSize != nil {
					tr.FontSizePt = te.TextRun.Style.FontSize.Magnitude
				}
				tr.Bold = te.TextRun.Style.Bold
				tr.Italic = te.TextRun.Style.Italic
			}
			elem.TextRuns = append(elem.TextRuns, tr)
		}

		if te.ParagraphMarker != nil && te.ParagraphMarker.Style != nil {
			pi := ParagraphInfo{
				StartIndex: startIdx,
				EndIndex:   endIdx,
			}
			style := te.ParagraphMarker.Style
			pi.LineSpacing = style.LineSpacing
			if style.SpaceAbove != nil {
				pi.SpaceAbovePt = style.SpaceAbove.Magnitude
			}
			if style.SpaceBelow != nil {
				pi.SpaceBelowPt = style.SpaceBelow.Magnitude
			}
			elem.Paragraphs = append(elem.Paragraphs, pi)
		}
	}
}

// --- Claude analysis via Vertex AI ---

func analyzeWithClaude(ctx context.Context, httpClient *http.Client, pdfData []byte, structure []SlideInfo, projectID, region string) (*CorrectionPlan, error) {
	structureJSON, err := json.MarshalIndent(structure, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal structure: %w", err)
	}

	pdfBase64 := base64.StdEncoding.EncodeToString(pdfData)

	prompt := fmt.Sprintf(`Tu es un expert en mise en forme de présentations professionnelles. Tu analyses une présentation Google Slides pour détecter les problèmes de formatage.

TÂCHE : Compare le rendu visuel (PDF) avec les données structurelles (JSON) pour identifier les éléments de texte ayant des problèmes de mise en forme. Pour chaque problème trouvé, produis une correction.

DONNÉES STRUCTURELLES (JSON) pour chaque slide :
%s

PROBLÈMES DE FORMATAGE À DÉTECTER :
1. DÉBORDEMENT DE TEXTE : Texte qui déborde visuellement de sa zone. Compare la quantité de texte avec les dimensions de la zone et la taille de police. Si le texte apparaît tronqué, coupé ou dépasse son conteneur dans le PDF, réduis la taille de police.
2. TAILLE DE POLICE TROP GRANDE : Éléments de texte dont la taille de police est disproportionnée par rapport à la zone de texte ou aux autres éléments de la même slide.
3. FAMILLE DE POLICE INCORRECTE : Texte utilisant une famille de police incohérente avec le design de la présentation. Les familles de police prédominantes dans la structure sont les bonnes.
4. PROBLÈMES D'ESPACEMENT DES LIGNES : Paragraphes où l'espacement des lignes est trop serré (lignes qui se chevauchent) ou trop lâche (espace excessif entre les lignes).
5. PROBLÈMES D'ESPACEMENT DES PARAGRAPHES : Espace excessif ou insuffisant au-dessus/en-dessous des paragraphes qui perturbe le flux visuel.

RÈGLES :
- Ne signale QUE les problèmes que tu peux VISUELLEMENT confirmer dans le rendu PDF.
- Chaque correction doit référencer un objectId EXACT des données structurelles.
- Pour les changements de style de texte, spécifie startIndex et endIndex des données structurelles pour cibler des text runs spécifiques. Omets les deux pour appliquer à TOUT le texte de l'élément.
- Pour les corrections de taille de police, suggère une taille précise en points qui corrigerait le débordement.
- Pour les changements de style de paragraphe, n'inclus que les champs qui doivent changer.
- Ne suggère PAS de corrections pour les éléments qui paraissent bien dans le PDF.
- Si aucun problème n'est trouvé, retourne un tableau de corrections vide.

Réponds UNIQUEMENT avec du JSON (pas de texte avant ou après) :
{
  "corrections": [
    {
      "objectId": "objectId exact de la structure",
      "slideIndex": 0,
      "cellLocation": null,
      "reason": "Brève description du problème",
      "type": "textStyle",
      "startIndex": null,
      "endIndex": null,
      "fontSizePt": 12.0,
      "fontFamily": null,
      "lineSpacing": null,
      "spaceAbovePt": null,
      "spaceBelowPt": null
    }
  ]
}`, string(structureJSON))

	requestBody := map[string]any{
		"anthropic_version": "vertex-2023-10-16",
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "document",
						"source": map[string]any{
							"type":       "base64",
							"media_type": "application/pdf",
							"data":       pdfBase64,
						},
					},
					{
						"type": "text",
						"text": prompt,
					},
				},
			},
		},
		"max_tokens":  16384,
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

	var plan CorrectionPlan
	if err := json.Unmarshal([]byte(responseText), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse correction plan: %w\nResponse was: %s", err, responseText)
	}

	return &plan, nil
}

// --- Correction validation ---

func validateCorrections(plan *CorrectionPlan, structure []SlideInfo) []Correction {
	objectIDs := make(map[string]bool)
	for _, slide := range structure {
		for _, elem := range slide.Elements {
			objectIDs[elem.ObjectID] = true
		}
	}

	var valid []Correction
	for _, c := range plan.Corrections {
		if !objectIDs[c.ObjectID] {
			log.Printf("Warning: skipping correction for unknown objectId %q", c.ObjectID)
			continue
		}
		if c.Type != "textStyle" && c.Type != "paragraphStyle" {
			log.Printf("Warning: skipping correction with unknown type %q for %s", c.Type, c.ObjectID)
			continue
		}
		if c.Type == "textStyle" && c.FontSizePt == nil && c.FontFamily == nil {
			log.Printf("Warning: skipping textStyle correction with no changes for %s", c.ObjectID)
			continue
		}
		if c.Type == "paragraphStyle" && c.LineSpacing == nil && c.SpaceAbovePt == nil && c.SpaceBelowPt == nil {
			log.Printf("Warning: skipping paragraphStyle correction with no changes for %s", c.ObjectID)
			continue
		}
		valid = append(valid, c)
	}

	return valid
}

// --- Build Slides API requests from corrections ---

func buildCorrections(corrections []Correction) []*slides.Request {
	var requests []*slides.Request
	for _, c := range corrections {
		switch c.Type {
		case "textStyle":
			requests = append(requests, buildTextStyleRequest(c))
		case "paragraphStyle":
			requests = append(requests, buildParagraphStyleRequest(c))
		}
	}
	return requests
}

func buildTextStyleRequest(c Correction) *slides.Request {
	style := &slides.TextStyle{}
	var fields []string

	if c.FontSizePt != nil {
		style.FontSize = &slides.Dimension{
			Magnitude: *c.FontSizePt,
			Unit:      "PT",
		}
		if *c.FontSizePt == 0 {
			style.FontSize.ForceSendFields = []string{"Magnitude"}
		}
		fields = append(fields, "fontSize")
	}
	if c.FontFamily != nil {
		style.FontFamily = *c.FontFamily
		fields = append(fields, "fontFamily")
	}

	req := &slides.UpdateTextStyleRequest{
		ObjectId: c.ObjectID,
		Style:    style,
		Fields:   strings.Join(fields, ","),
	}

	req.TextRange = buildTextRange(c.StartIndex, c.EndIndex)

	if c.CellLocation != nil {
		req.CellLocation = &slides.TableCellLocation{
			RowIndex:    int64(c.CellLocation.RowIndex),
			ColumnIndex: int64(c.CellLocation.ColumnIndex),
		}
	}

	return &slides.Request{UpdateTextStyle: req}
}

func buildParagraphStyleRequest(c Correction) *slides.Request {
	style := &slides.ParagraphStyle{}
	var fields []string

	if c.LineSpacing != nil {
		style.LineSpacing = *c.LineSpacing
		style.ForceSendFields = append(style.ForceSendFields, "LineSpacing")
		fields = append(fields, "lineSpacing")
	}
	if c.SpaceAbovePt != nil {
		dim := &slides.Dimension{
			Magnitude: *c.SpaceAbovePt,
			Unit:      "PT",
		}
		if *c.SpaceAbovePt == 0 {
			dim.ForceSendFields = []string{"Magnitude"}
		}
		style.SpaceAbove = dim
		fields = append(fields, "spaceAbove")
	}
	if c.SpaceBelowPt != nil {
		dim := &slides.Dimension{
			Magnitude: *c.SpaceBelowPt,
			Unit:      "PT",
		}
		if *c.SpaceBelowPt == 0 {
			dim.ForceSendFields = []string{"Magnitude"}
		}
		style.SpaceBelow = dim
		fields = append(fields, "spaceBelow")
	}

	req := &slides.UpdateParagraphStyleRequest{
		ObjectId: c.ObjectID,
		Style:    style,
		Fields:   strings.Join(fields, ","),
	}

	req.TextRange = buildTextRange(c.StartIndex, c.EndIndex)

	if c.CellLocation != nil {
		req.CellLocation = &slides.TableCellLocation{
			RowIndex:    int64(c.CellLocation.RowIndex),
			ColumnIndex: int64(c.CellLocation.ColumnIndex),
		}
	}

	return &slides.Request{UpdateParagraphStyle: req}
}

func buildTextRange(startIndex, endIndex *int) *slides.Range {
	if startIndex != nil && endIndex != nil {
		si := int64(*startIndex)
		ei := int64(*endIndex)
		return &slides.Range{
			Type:            "FIXED_RANGE",
			StartIndex:      &si,
			EndIndex:        &ei,
			ForceSendFields: []string{"StartIndex"},
		}
	}
	return &slides.Range{Type: "ALL"}
}

// --- Apply corrections via Slides API ---

func applyCorrections(ctx context.Context, slidesSrv *slides.Service, presentationID string, requests []*slides.Request) error {
	_, err := slidesSrv.Presentations.BatchUpdate(presentationID, &slides.BatchUpdatePresentationRequest{
		Requests: requests,
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("batch update failed: %w", err)
	}
	return nil
}
