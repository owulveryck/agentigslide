// Package fixfonts detects and corrects formatting issues in Google Slides
// presentations. It exports the presentation as PDF, extracts the structural
// information of each slide's text elements, uses Claude Vision via Vertex AI
// to identify formatting problems (text overflow, wrong fonts, bad spacing),
// and applies corrections through the Google Slides BatchUpdate API.
package fixfonts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/owulveryck/agentigslide/internal/vertex"

	"github.com/kelseyhightower/envconfig"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/slides/v1"
)

// Config holds fixfonts-specific parameters loaded from environment variables
// with the "FIXFONTS" prefix (e.g. FIXFONTS_MODEL, FIXFONTS_MAX_TOKENS).
type Config struct {
	Model     string `envconfig:"MODEL" default:"claude-opus-4-6" desc:"Claude model for formatting analysis"`
	MaxTokens int    `envconfig:"MAX_TOKENS" default:"16384" desc:"Maximum tokens in Claude response"`
}

// LoadConfig loads the fixfonts Config from environment variables with the
// "FIXFONTS" prefix.
func LoadConfig() (Config, error) {
	var cfg Config
	if err := envconfig.Process("FIXFONTS", &cfg); err != nil {
		return cfg, fmt.Errorf("loading FIXFONTS config: %w", err)
	}
	return cfg, nil
}

// SlideInfo holds structural information about a single slide, including its
// index, page ID, and all text-bearing elements found on the slide.
type SlideInfo struct {
	SlideIndex int           `json:"slideIndex"`
	PageID     string        `json:"pageId"`
	Elements   []ElementInfo `json:"elements"`
}

// ElementInfo contains detailed information about a text-bearing element,
// including its object ID, shape type, bounding box, text runs with styling,
// and paragraph spacing information.
type ElementInfo struct {
	ObjectID        string          `json:"objectId"`
	ShapeType       string          `json:"shapeType,omitempty"`
	PlaceholderType string          `json:"placeholderType,omitempty"`
	BoundingBox     BoundingBox     `json:"boundingBox"`
	TextRuns        []TextRunInfo   `json:"textRuns"`
	Paragraphs      []ParagraphInfo `json:"paragraphs"`
	CellLocation    *CellRef        `json:"cellLocation,omitempty"`
}

// BoundingBox represents the position and dimensions of an element in points.
type BoundingBox struct {
	WidthPt  float64 `json:"widthPt"`
	HeightPt float64 `json:"heightPt"`
	LeftPt   float64 `json:"leftPt"`
	TopPt    float64 `json:"topPt"`
}

// TextRunInfo holds the style and content information for a single text run,
// including its character range, font family, font size, and emphasis flags.
type TextRunInfo struct {
	StartIndex int     `json:"startIndex"`
	EndIndex   int     `json:"endIndex"`
	Content    string  `json:"content"`
	FontFamily string  `json:"fontFamily,omitempty"`
	FontSizePt float64 `json:"fontSizePt,omitempty"`
	Bold       bool    `json:"bold,omitempty"`
	Italic     bool    `json:"italic,omitempty"`
}

// ParagraphInfo holds the spacing information for a single paragraph,
// including its character range, line spacing, and space above/below.
type ParagraphInfo struct {
	StartIndex   int     `json:"startIndex"`
	EndIndex     int     `json:"endIndex"`
	LineSpacing  float64 `json:"lineSpacing,omitempty"`
	SpaceAbovePt float64 `json:"spaceAbovePt,omitempty"`
	SpaceBelowPt float64 `json:"spaceBelowPt,omitempty"`
}

// CellRef identifies a specific table cell by its row and column indices.
type CellRef struct {
	RowIndex    int `json:"rowIndex"`
	ColumnIndex int `json:"columnIndex"`
}

// CorrectionPlan holds the set of formatting corrections proposed by Claude
// after analyzing the presentation's visual rendering against its structure.
type CorrectionPlan struct {
	Corrections []Correction `json:"corrections"`
}

// Correction describes a single formatting correction to apply, including the
// target element, slide index, correction type ("textStyle" or "paragraphStyle"),
// reason for the fix, and the new style values (font size, font family,
// line spacing, or paragraph spacing).
type Correction struct {
	ObjectID     string   `json:"objectId"`
	SlideIndex   int      `json:"slideIndex"`
	CellLocation *CellRef `json:"cellLocation,omitempty"`
	Reason       string   `json:"reason"`
	Type         string   `json:"type"`

	StartIndex *int     `json:"startIndex,omitempty"`
	EndIndex   *int     `json:"endIndex,omitempty"`
	FontSizePt *float64 `json:"fontSizePt,omitempty"`
	FontFamily *string  `json:"fontFamily,omitempty"`

	LineSpacing  *float64 `json:"lineSpacing,omitempty"`
	SpaceAbovePt *float64 `json:"spaceAbovePt,omitempty"`
	SpaceBelowPt *float64 `json:"spaceBelowPt,omitempty"`
}

// Run executes the full fixfonts pipeline: export PDF, extract structure,
// analyze with Claude, validate, and apply corrections.
func Run(ctx context.Context, slidesSrv *slides.Service, driveSrv *drive.Service, vc *vertex.Client, cfg Config, presentationID string) error {
	slog.Info("exporting presentation as PDF")
	pdfData, err := ExportPDF(ctx, driveSrv, presentationID)
	if err != nil {
		return fmt.Errorf("failed to export PDF: %w", err)
	}
	slog.Info("PDF exported", "bytes", len(pdfData))

	slog.Info("fetching presentation structure")
	pres, err := slidesSrv.Presentations.Get(presentationID).Do()
	if err != nil {
		return fmt.Errorf("failed to get presentation: %w", err)
	}
	structure := ExtractStructure(pres)
	slog.Info("extracted structure", "slides", len(structure))

	slog.Info("analyzing formatting with Claude")
	correctionPlan, err := AnalyzeWithClaude(ctx, vc, cfg, pdfData, structure)
	if err != nil {
		return fmt.Errorf("failed to analyze with Claude: %w", err)
	}

	if len(correctionPlan.Corrections) == 0 {
		slog.Info("no formatting issues found")
		return nil
	}

	slog.Info("formatting issues found", "count", len(correctionPlan.Corrections))
	for _, c := range correctionPlan.Corrections {
		slog.Info("formatting issue", "slide", c.SlideIndex, "objectID", c.ObjectID, "reason", c.Reason)
	}

	validCorrections := ValidateCorrections(correctionPlan, structure)
	if len(validCorrections) == 0 {
		slog.Warn("all corrections were invalid after validation")
		return nil
	}

	requests := BuildCorrections(validCorrections)
	slog.Info("applying corrections", "count", len(requests))
	if err := ApplyCorrections(ctx, slidesSrv, presentationID, requests); err != nil {
		return fmt.Errorf("failed to apply corrections: %w", err)
	}

	slog.Info("formatting corrections applied")
	return nil
}

// ExportPDF exports a Google Slides presentation as a PDF via the Drive API
// and returns the raw PDF bytes.
func ExportPDF(ctx context.Context, driveSrv *drive.Service, presentationID string) ([]byte, error) {
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

const emuToPoints = 12700.0

// ExtractStructure extracts text element structural information from all slides
// in a presentation, including bounding boxes, text runs with styling, and
// paragraph spacing data.
func ExtractStructure(pres *slides.Presentation) []SlideInfo {
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

// AnalyzeWithClaude sends the presentation PDF and structural data to Claude
// via Vertex AI for formatting analysis. It returns a CorrectionPlan
// containing the identified formatting issues and proposed fixes.
func AnalyzeWithClaude(ctx context.Context, vc *vertex.Client, cfg Config, pdfData []byte, structure []SlideInfo) (*CorrectionPlan, error) {
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

	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{
			{
				Type: "document",
				Source: &vertex.DataSource{
					Type:      "base64",
					MediaType: "application/pdf",
					Data:      pdfBase64,
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
		return nil, fmt.Errorf("claude API call failed: %w", err)
	}

	var plan CorrectionPlan
	if err := json.Unmarshal([]byte(responseText), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse correction plan: %w\nResponse was: %s", err, responseText)
	}

	return &plan, nil
}

// ValidateCorrections filters a correction plan to keep only corrections that
// reference known element IDs, have a valid type, and include at least one
// style change. Invalid corrections are logged and discarded.
func ValidateCorrections(plan *CorrectionPlan, structure []SlideInfo) []Correction {
	objectIDs := make(map[string]bool)
	for _, slide := range structure {
		for _, elem := range slide.Elements {
			objectIDs[elem.ObjectID] = true
		}
	}

	var valid []Correction
	for _, c := range plan.Corrections {
		if !objectIDs[c.ObjectID] {
			slog.Warn("skipping correction for unknown objectId", "objectID", c.ObjectID)
			continue
		}
		if c.Type != "textStyle" && c.Type != "paragraphStyle" {
			slog.Warn("skipping correction with unknown type", "type", c.Type, "objectID", c.ObjectID)
			continue
		}
		if c.Type == "textStyle" && c.FontSizePt == nil && c.FontFamily == nil {
			slog.Warn("skipping textStyle correction with no changes", "objectID", c.ObjectID)
			continue
		}
		if c.Type == "paragraphStyle" && c.LineSpacing == nil && c.SpaceAbovePt == nil && c.SpaceBelowPt == nil {
			slog.Warn("skipping paragraphStyle correction with no changes", "objectID", c.ObjectID)
			continue
		}
		valid = append(valid, c)
	}

	return valid
}

// BuildCorrections converts validated corrections into Google Slides API
// BatchUpdate requests (UpdateTextStyle or UpdateParagraphStyle).
func BuildCorrections(corrections []Correction) []*slides.Request {
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

// ApplyCorrections sends the correction requests to the Google Slides API
// via a BatchUpdate call.
func ApplyCorrections(ctx context.Context, slidesSrv *slides.Service, presentationID string, requests []*slides.Request) error {
	_, err := slidesSrv.Presentations.BatchUpdate(presentationID, &slides.BatchUpdatePresentationRequest{
		Requests: requests,
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("batch update failed: %w", err)
	}
	return nil
}
