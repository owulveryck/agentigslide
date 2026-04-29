package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SlideAnalysis représente la structure d'un fichier analysis.json
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

// TemplateSlide représente une slide dans l'index
type TemplateSlide struct {
	SlideNumber    int                    `json:"slideNumber"`
	SlideID        string                 `json:"slideId"`
	Intention      string                 `json:"intention"`
	Keywords       []string               `json:"keywords"`
	EditableFields []EditableFieldSummary `json:"editableFields"`
	VisualElements []VisualElementSummary `json:"visualElements,omitempty"`
}

type EditableFieldSummary struct {
	ObjectID     string        `json:"objectId"`
	Role         string        `json:"role"`
	Placeholder  *string       `json:"placeholder"`
	Content      string        `json:"content,omitempty"`
	RawContent   string        `json:"rawContent,omitempty"`
	VariableName string        `json:"variableName"`
	CellLocation *CellLocation `json:"cellLocation,omitempty"`
	WidthPt      float64       `json:"widthPt,omitempty"`
	HeightPt     float64       `json:"heightPt,omitempty"`
	MaxChars     int           `json:"maxChars,omitempty"`
}

type CellLocation struct {
	RowIndex    int `json:"rowIndex"`
	ColumnIndex int `json:"columnIndex"`
}

type VisualElementSummary struct {
	ObjectID *string `json:"objectId,omitempty"`
	Type     string  `json:"type"`
	Purpose  string  `json:"purpose,omitempty"`
}

// TemplateIndex représente l'index complet
type TemplateIndex struct {
	TemplateID string          `json:"templateId"`
	Slides     []TemplateSlide `json:"slides"`
}

// Structures pour parser le content.json
type SlideContent struct {
	ObjectID     string        `json:"objectId"`
	PageElements []PageElement `json:"pageElements"`
}

type PageElement struct {
	ObjectID     string        `json:"objectId"`
	Shape        *Shape        `json:"shape,omitempty"`
	Table        *Table        `json:"table,omitempty"`
	ElementGroup *ElementGroup `json:"elementGroup,omitempty"`
	Size         *Size         `json:"size,omitempty"`
	Transform    *Transform    `json:"transform,omitempty"`
}

type Table struct {
	Rows      int        `json:"rows"`
	Columns   int        `json:"columns"`
	TableRows []TableRow `json:"tableRows,omitempty"`
}

type TableRow struct {
	TableCells []TableCell `json:"tableCells,omitempty"`
}

type TableCell struct {
	Text *TextContent `json:"text,omitempty"`
}

type TextContent struct {
	TextElements []TextElement `json:"textElements,omitempty"`
}

type TextElement struct {
	TextRun *TextRun `json:"textRun,omitempty"`
}

type TextRunStyle struct {
	FontSize *Magnitude `json:"fontSize,omitempty"`
}

type TextRun struct {
	Content string        `json:"content"`
	Style   *TextRunStyle `json:"style,omitempty"`
}

type Shape struct {
	ShapeType string       `json:"shapeType,omitempty"`
	Text      *TextContent `json:"text,omitempty"`
}

type ElementGroup struct {
	Children []PageElement `json:"children,omitempty"`
}

type Size struct {
	Height Magnitude `json:"height"`
	Width  Magnitude `json:"width"`
}

type Magnitude struct {
	Magnitude float64 `json:"magnitude"`
}

type Transform struct {
	TranslateX float64 `json:"translateX"`
	TranslateY float64 `json:"translateY"`
	ScaleX     float64 `json:"scaleX,omitempty"`
	ScaleY     float64 `json:"scaleY,omitempty"`
}

func main() {
	templateID := os.Getenv("SLIDES_PREFORMATES_ID")
	if templateID == "" {
		log.Fatal("SLIDES_PREFORMATES_ID environment variable must be set")
	}

	baseDir := fmt.Sprintf("template/%s", templateID)

	// Find all analysis.json files
	var analyses []SlideAnalysis
	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Name() == "analysis.json" {
			data, err := os.ReadFile(path)
			if err != nil {
				log.Printf("Warning: failed to read %s: %v", path, err)
				return nil
			}

			var analysis SlideAnalysis
			if err := json.Unmarshal(data, &analysis); err != nil {
				log.Printf("Warning: failed to parse %s: %v", path, err)
				return nil
			}

			analyses = append(analyses, analysis)
		}
		return nil
	})

	if err != nil {
		log.Fatalf("Failed to walk template directory: %v", err)
	}

	if len(analyses) == 0 {
		log.Fatal("No analysis.json files found")
	}

	// Sort by slide number
	sort.Slice(analyses, func(i, j int) bool {
		return analyses[i].SlideNumber < analyses[j].SlideNumber
	})

	// Build index
	index := TemplateIndex{
		TemplateID: templateID,
		Slides:     make([]TemplateSlide, 0, len(analyses)),
	}

	for _, analysis := range analyses {
		slide := TemplateSlide{
			SlideNumber:    analysis.SlideNumber,
			SlideID:        analysis.SlideID,
			Intention:      analysis.Intention,
			Keywords:       extractKeywords(analysis),
			EditableFields: make([]EditableFieldSummary, 0, len(analysis.EditableElements)),
			VisualElements: make([]VisualElementSummary, 0),
		}

		// Load slide content for variable name generation
		slideContent, err := loadSlideContent(baseDir, analysis.SlideNumber)
		if err != nil {
			log.Printf("Warning: failed to load content.json for slide %d: %v", analysis.SlideNumber, err)
			slideContent = nil
		}

		var rawTextMap map[string]string
		if slideContent != nil {
			rawTextMap = extractShapeTextMap(slideContent)
		}

		// Extract editable fields
		for _, elem := range analysis.EditableElements {
			role := inferRole(elem)

			// Generate variable name
			varName := ""
			if slideContent != nil {
				varName = generateVariableName(elem, slideContent, &analysis)
			}

			content := elem.Content
			if isPlaceholderContent(content) {
				content = ""
			}

			rawContent := ""
			if rawTextMap != nil {
				rawContent = rawTextMap[elem.ObjectID]
			}

			var widthPt, heightPt float64
			var maxChars int
			if slideContent != nil {
				if pageElem := findPageElementById(slideContent, elem.ObjectID); pageElem != nil {
					widthPt, heightPt = computeElementSize(pageElem)
					fontSize := extractPredominantFontSize(pageElem)
					maxChars = estimateMaxChars(widthPt, heightPt, fontSize)
				}
			}

			field := EditableFieldSummary{
				ObjectID:     elem.ObjectID,
				Role:         role,
				Placeholder:  elem.Placeholder,
				Content:      content,
				RawContent:   rawContent,
				VariableName: varName,
				WidthPt:      widthPt,
				HeightPt:     heightPt,
				MaxChars:     maxChars,
			}

			slide.EditableFields = append(slide.EditableFields, field)
		}

		if slideContent != nil {
			resolveTableCells(slide.EditableFields, slideContent)
		}
		deduplicateVariableNames(slide.EditableFields)

		// Extract reusable visual elements (with objectId)
		for _, elem := range analysis.VisualElements {
			if elem.ObjectID != nil && *elem.ObjectID != "" {
				visual := VisualElementSummary{
					ObjectID: elem.ObjectID,
					Type:     elem.Type,
					Purpose:  elem.Purpose,
				}
				slide.VisualElements = append(slide.VisualElements, visual)
			}
		}

		index.Slides = append(index.Slides, slide)
	}

	// Write template_index.json
	indexJSON, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal index: %v", err)
	}

	outputPath := "template_index.json"
	if err := os.WriteFile(outputPath, indexJSON, 0644); err != nil {
		log.Fatalf("Failed to write template_index.json: %v", err)
	}

	fmt.Printf("Template index generated successfully!\n")
	fmt.Printf("- Template ID: %s\n", templateID)
	fmt.Printf("- Slides indexed: %d\n", len(index.Slides))
	fmt.Printf("- Output: %s\n", outputPath)
}

// extractKeywords extrait les mots-clés pertinents de l'intention et de la description
func extractKeywords(analysis SlideAnalysis) []string {
	keywords := make(map[string]bool)

	// Tokenize intention and description
	text := strings.ToLower(analysis.Intention + " " + analysis.Description)

	// Remove common French words (stop words) and placeholder terms
	stopWords := map[string]bool{
		"de": true, "la": true, "le": true, "les": true, "des": true, "un": true, "une": true,
		"et": true, "ou": true, "pour": true, "avec": true, "dans": true, "sur": true, "par": true,
		"du": true, "au": true, "aux": true, "à": true, "en": true, "cette": true, "ce": true,
		"qui": true, "que": true, "dont": true, "est": true, "sont": true, "être": true,
		"présente": true, "contient": true, "indique": true, "permet": true,
		"lorem": true, "ipsum": true, "dolor": true, "amet": true, "dummy": true,
	}

	// Split by common delimiters
	words := strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == ',' || r == '.' || r == ':' || r == ';' || r == '/' || r == '-' || r == '(' || r == ')' || r == '\'' || r == '"' || r == '«' || r == '»'
	})

	for _, word := range words {
		word = strings.TrimSpace(word)
		if len(word) >= 3 && !stopWords[word] {
			keywords[word] = true
		}
	}

	// Convert map to sorted slice
	result := make([]string, 0, len(keywords))
	for kw := range keywords {
		result = append(result, kw)
	}
	sort.Strings(result)

	// Limit to top 15 keywords for readability
	if len(result) > 15 {
		result = result[:15]
	}

	return result
}

func isPlaceholderContent(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	return strings.Contains(lower, "lorem ipsum") ||
		strings.Contains(lower, "dummy text") ||
		strings.Contains(lower, "dolor sit amet")
}

// inferRole tente de deviner le rôle d'un élément éditable basé sur sa description et son contenu
func inferRole(elem EditableElement) string {
	desc := strings.ToLower(elem.Description)
	content := strings.ToLower(elem.Content)

	// Common patterns
	if strings.Contains(desc, "titre principal") || strings.Contains(desc, "titre de la slide") {
		return "titre_principal"
	}
	if strings.Contains(desc, "sous-titre") || (elem.Placeholder != nil && *elem.Placeholder == "SUBTITLE") {
		return "sous_titre"
	}
	if strings.Contains(desc, "sommaire") || strings.Contains(content, "sommaire") {
		return "sommaire"
	}
	if strings.Contains(desc, "année") || strings.Contains(content, "2026") || strings.Contains(content, "2025") {
		return "annee"
	}
	if strings.Contains(desc, "entreprise") || strings.Contains(content, "octo") {
		return "entreprise"
	}
	if strings.Contains(desc, "copyright") || strings.Contains(content, "©") || strings.Contains(content, "copyright") {
		return "copyright"
	}
	if strings.Contains(desc, "numéro de page") || strings.Contains(desc, "pagination") {
		return "numero_page"
	}
	if strings.Contains(desc, "bullet") || strings.Contains(desc, "liste") {
		return "liste_points"
	}
	if strings.Contains(desc, "tableau") || strings.Contains(desc, "cellule") {
		return "tableau"
	}
	if strings.Contains(desc, "légende") || strings.Contains(desc, "caption") {
		return "legende"
	}
	if elem.Placeholder != nil && *elem.Placeholder == "BODY" {
		return "corps_texte"
	}
	if elem.Placeholder != nil && *elem.Placeholder == "TITLE" {
		return "titre"
	}

	// Default to generic text
	return "texte"
}

// ===== Helper functions for Apps Script variable names =====

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

const emuToPoints = 12700.0

func computeElementSize(el *PageElement) (widthPt, heightPt float64) {
	if el == nil || el.Size == nil {
		return 0, 0
	}
	scaleX := 1.0
	scaleY := 1.0
	if el.Transform != nil {
		if el.Transform.ScaleX != 0 {
			scaleX = el.Transform.ScaleX
		}
		if el.Transform.ScaleY != 0 {
			scaleY = el.Transform.ScaleY
		}
	}
	widthPt = math.Abs(el.Size.Width.Magnitude*scaleX) / emuToPoints
	heightPt = math.Abs(el.Size.Height.Magnitude*scaleY) / emuToPoints
	return widthPt, heightPt
}

func extractPredominantFontSize(el *PageElement) float64 {
	if el == nil || el.Shape == nil || el.Shape.Text == nil {
		return 14.0
	}
	var totalSize float64
	var count int
	for _, te := range el.Shape.Text.TextElements {
		if te.TextRun != nil && te.TextRun.Style != nil && te.TextRun.Style.FontSize != nil {
			totalSize += te.TextRun.Style.FontSize.Magnitude
			count++
		}
	}
	if count == 0 {
		return 14.0
	}
	return totalSize / float64(count)
}

func estimateMaxChars(widthPt, heightPt, fontSizePt float64) int {
	if widthPt <= 0 || heightPt <= 0 || fontSizePt <= 0 {
		return 0
	}
	charsPerLine := widthPt / (fontSizePt * 0.6)
	lines := heightPt / (fontSizePt * 1.3)
	maxChars := int(charsPerLine * lines)
	if maxChars < 0 {
		return 0
	}
	return maxChars
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

// resolveTableCells matches editable fields with empty ObjectID to table cells from content.json.
// It collects all table cells in row-major order and matches them to empty-objectId fields sequentially
// using content prefix matching. Each table cell is matched at most once.
func resolveTableCells(fields []EditableFieldSummary, content *SlideContent) {
	type tableCell struct {
		tableObjectID string
		row, col      int
		text          string
	}

	var cells []tableCell
	for _, el := range content.PageElements {
		if el.Table == nil {
			continue
		}
		for ri, row := range el.Table.TableRows {
			for ci, cell := range row.TableCells {
				cellText := strings.TrimSpace(extractCellText(&cell))
				cells = append(cells, tableCell{
					tableObjectID: el.ObjectID,
					row:           ri,
					col:           ci,
					text:          cellText,
				})
			}
		}
	}
	if len(cells) == 0 {
		return
	}

	matched := make([]bool, len(cells))
	for i := range fields {
		if fields[i].ObjectID != "" {
			continue
		}
		analysisText := strings.ToLower(strings.TrimSpace(fields[i].Content))
		if analysisText == "" {
			// For empty content fields, try to find an unmatched cell with empty or placeholder text
			for j, cell := range cells {
				if matched[j] {
					continue
				}
				cellLower := strings.ToLower(cell.text)
				if isPlaceholderContent(cellLower) || cellLower == "" {
					fields[i].ObjectID = cell.tableObjectID
					fields[i].CellLocation = &CellLocation{RowIndex: cell.row, ColumnIndex: cell.col}
					matched[j] = true
					break
				}
			}
			continue
		}

		for j, cell := range cells {
			if matched[j] {
				continue
			}
			cellLower := strings.ToLower(cell.text)
			if cellLower == analysisText ||
				strings.HasPrefix(analysisText, cellLower) ||
				strings.HasPrefix(cellLower, analysisText) {
				fields[i].ObjectID = cell.tableObjectID
				fields[i].CellLocation = &CellLocation{RowIndex: cell.row, ColumnIndex: cell.col}
				matched[j] = true
				break
			}
		}
	}
}

func extractShapeTextMap(content *SlideContent) map[string]string {
	result := make(map[string]string)
	for _, el := range content.PageElements {
		extractShapeTexts(&el, result)
	}
	return result
}

func extractShapeTexts(el *PageElement, result map[string]string) {
	if el.Shape != nil && el.Shape.Text != nil {
		var sb strings.Builder
		for _, te := range el.Shape.Text.TextElements {
			if te.TextRun != nil {
				sb.WriteString(te.TextRun.Content)
			}
		}
		text := strings.TrimRight(sb.String(), "\n")
		result[el.ObjectID] = text
	}
	if el.ElementGroup != nil {
		for i := range el.ElementGroup.Children {
			extractShapeTexts(&el.ElementGroup.Children[i], result)
		}
	}
}

func extractCellText(cell *TableCell) string {
	if cell.Text == nil {
		return ""
	}
	var sb strings.Builder
	for _, te := range cell.Text.TextElements {
		if te.TextRun != nil {
			sb.WriteString(te.TextRun.Content)
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// deduplicateVariableNames adds numeric suffixes when multiple fields share the same variableName.
// e.g. [textShape, textShape, textShape] → [textShape, text2Shape, text3Shape]
func deduplicateVariableNames(fields []EditableFieldSummary) {
	counts := make(map[string]int)
	for _, f := range fields {
		counts[f.VariableName]++
	}

	seen := make(map[string]int)
	for i := range fields {
		name := fields[i].VariableName
		if counts[name] <= 1 {
			continue
		}
		seen[name]++
		idx := seen[name]
		if idx == 1 {
			continue
		}
		base := strings.TrimSuffix(name, "Shape")
		newName := fmt.Sprintf("%s%dShape", base, idx)
		fields[i].VariableName = newName
	}
}

// loadSlideContent charge le fichier content.json pour une slide
func loadSlideContent(baseDir string, slideNumber int) (*SlideContent, error) {
	contentPath := filepath.Join(baseDir, fmt.Sprintf("%d", slideNumber), "content.json")
	data, err := os.ReadFile(contentPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read content.json: %w", err)
	}

	var content SlideContent
	if err := json.Unmarshal(data, &content); err != nil {
		return nil, fmt.Errorf("failed to parse content.json: %w", err)
	}

	return &content, nil
}
