// Command buildTemplateIndex aggregates analysis.json files from all analyzed
// slides into a single template_index.json. For each slide, it extracts
// keywords, generates semantic variable names for editable fields, computes
// field dimensions and character capacity, and resolves table cell mappings.
//
// Usage:
//
//	go run buildTemplateIndex/build_template_index.go
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/owulveryck/slideAppScripter/internal/config"
	"github.com/owulveryck/slideAppScripter/internal/model"
	"github.com/owulveryck/slideAppScripter/internal/plan"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: build_template_index\n\nAggregates analysis.json files into template_index.json.\n")
		config.PrintAllUsage(
			struct {
				Prefix string
				Spec   any
			}{"SLIDES", &config.SlidesConfig{}},
		)
	}
	flag.Parse()

	slidesCfg, err := config.LoadSlidesConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	baseDir := slidesCfg.TemplateDir()

	// Find all analysis.json files
	var analyses []model.SlideAnalysis
	err = filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Name() == "analysis.json" {
			data, err := os.ReadFile(path)
			if err != nil {
				log.Printf("Warning: failed to read %s: %v", path, err)
				return nil
			}

			var analysis model.SlideAnalysis
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
	index := model.TemplateIndex{
		TemplateID: slidesCfg.TemplateID,
		Slides:     make([]model.TemplateSlide, 0, len(analyses)),
	}

	for _, analysis := range analyses {
		slide := model.TemplateSlide{
			SlideNumber:    analysis.SlideNumber,
			SlideID:        analysis.SlideID,
			Intention:      analysis.Intention,
			Keywords:       extractKeywords(analysis),
			EditableFields: make([]model.EditableFieldSummary, 0, len(analysis.EditableElements)),
			VisualElements: make([]model.VisualElementSummary, 0),
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
					font := extractPredominantFont(pageElem)
					maxChars = estimateMaxChars(widthPt, heightPt, font)
				}
			}

			field := model.EditableFieldSummary{
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
				slide.VisualElements = append(slide.VisualElements, model.VisualElementSummary(elem))
			}
		}

		slide.LayoutDescription = generateLayoutDescription(analysis, slideContent, slide.EditableFields)

		index.Slides = append(index.Slides, slide)
	}

	// Write template_index.json
	indexJSON, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal index: %v", err)
	}

	outputPath := slidesCfg.EffectiveTemplateIndex()
	if err := os.WriteFile(outputPath, indexJSON, 0644); err != nil {
		log.Fatalf("Failed to write %s: %v", outputPath, err)
	}

	fmt.Printf("Template index generated successfully!\n")
	fmt.Printf("- Template ID: %s\n", slidesCfg.TemplateID)
	fmt.Printf("- Slides indexed: %d\n", len(index.Slides))
	fmt.Printf("- Output: %s\n", outputPath)
}

// extractKeywords extrait les mots-clés pertinents de l'intention et de la description
func extractKeywords(analysis model.SlideAnalysis) []string {
	keywords := make(map[string]bool)

	// Tokenize intention and description
	text := strings.ToLower(analysis.Intention + " " + analysis.Description)

	stopWords := map[string]bool{
		// French articles/prepositions/conjunctions
		"de": true, "la": true, "le": true, "les": true, "des": true, "un": true, "une": true,
		"et": true, "ou": true, "pour": true, "avec": true, "dans": true, "sur": true, "par": true,
		"du": true, "au": true, "aux": true, "à": true, "en": true, "cette": true, "ce": true,
		"qui": true, "que": true, "dont": true, "est": true, "sont": true, "être": true,
		// Verbs commonly used in descriptions (non-discriminating)
		"présente": true, "contient": true, "indique": true, "permet": true,
		"présentant": true, "affichant": true, "comportant": true, "composé": true,
		"contenant": true, "accompagné": true, "accompagnée": true, "servant": true,
		"destiné": true, "destinée": true, "montrant": true, "illustrant": true,
		// Visual/layout descriptors (non-discriminating)
		"réutilisable": true, "réutilisables": true,
		"positionné": true, "positionnée": true, "positionner": true,
		"rectangulaire": true, "arrondi": true, "apparaît": true, "affiché": true,
		"bleu": true, "blanc": true, "bas": true, "haut": true,
		"gauche": true, "droite": true, "centre": true, "milieu": true,
		// Template/meta terms
		"lorem": true, "ipsum": true, "dolor": true, "amet": true, "dummy": true,
		"accenture": true, "octo": true, "technology": true,
		"slide": true, "template": true, "placeholder": true, "placeholders": true,
		"2026": true, "2025": true, "2024": true,
		// Common non-discriminating nouns
		"texte": true, "zone": true, "zones": true, "fond": true, "titre": true,
		"type": true, "contenu": true, "élément": true, "éléments": true,
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

	// Convert map to slice, sorted by length descending (longer words are more discriminating)
	result := make([]string, 0, len(keywords))
	for kw := range keywords {
		result = append(result, kw)
	}
	sort.Slice(result, func(i, j int) bool {
		if len(result[i]) != len(result[j]) {
			return len(result[i]) > len(result[j])
		}
		return result[i] < result[j]
	})

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
func inferRole(elem model.EditableElement) string {
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
	if strings.Contains(desc, "numéro") || strings.Contains(desc, "numérotation") || strings.Contains(desc, "numbering") {
		return "numerotation"
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
func findPageElementById(content *model.SlideContent, objectId string) *model.PageElement {
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

func computeElementSize(el *model.PageElement) (widthPt, heightPt float64) {
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

type fontInfo struct {
	SizePt float64
	Family string
	Bold   bool
}

func extractPredominantFont(el *model.PageElement) fontInfo {
	if el == nil || el.Shape == nil || el.Shape.Text == nil {
		return fontInfo{SizePt: 14.0}
	}
	var totalSize float64
	var count int
	familyCounts := make(map[string]int)
	boldCount := 0
	for _, te := range el.Shape.Text.TextElements {
		if te.TextRun != nil && te.TextRun.Style != nil {
			if te.TextRun.Style.FontSize != nil {
				totalSize += te.TextRun.Style.FontSize.Magnitude
				count++
			}
			if te.TextRun.Style.FontFamily != "" {
				familyCounts[te.TextRun.Style.FontFamily]++
			}
			if te.TextRun.Style.Bold {
				boldCount++
			}
		}
	}
	info := fontInfo{SizePt: 14.0}
	if count > 0 {
		info.SizePt = totalSize / float64(count)
		info.Bold = boldCount > count/2
	}
	maxCount := 0
	for family, c := range familyCounts {
		if c > maxCount {
			maxCount = c
			info.Family = family
		}
	}
	return info
}

var fontWidthRatios = map[string]float64{
	"Outfit":                    0.52,
	"Arial":                     0.60,
	"Helvetica Neue":            0.58,
	"Century Gothic":            0.58,
	"Merriweather Sans":         0.58,
	"Roboto":                    0.56,
	"Fira Sans Extra Condensed": 0.42,
	"Courier New":               0.60,
	"Noto Sans Symbols":         0.60,
	"Comic Sans MS":             0.60,
}

const defaultWidthRatio = 0.55

func fontCharWidthRatio(fontFamily string, bold bool) float64 {
	ratio, ok := fontWidthRatios[fontFamily]
	if !ok {
		ratio = defaultWidthRatio
	}
	if bold {
		ratio *= 1.08
	}
	return ratio
}

func estimateMaxChars(widthPt, heightPt float64, font fontInfo) int {
	if widthPt <= 0 || heightPt <= 0 || font.SizePt <= 0 {
		return 0
	}
	charWidthRatio := fontCharWidthRatio(font.Family, font.Bold)
	charsPerLine := widthPt / (font.SizePt * charWidthRatio)
	lines := heightPt / (font.SizePt * 1.3)
	maxChars := int(charsPerLine * lines)
	if maxChars < 0 {
		return 0
	}
	return maxChars
}

// getSimplePosition convertit une position EMU en position simple
func getSimplePosition(transform *model.Transform) string {
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
func needsPositionSuffix(elem model.EditableElement, analysis *model.SlideAnalysis) bool {
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
func generateVariableName(elem model.EditableElement, slideContent *model.SlideContent, analysis *model.SlideAnalysis) string {
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
func resolveTableCells(fields []model.EditableFieldSummary, content *model.SlideContent) {
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
					fields[i].CellLocation = &model.CellLocation{RowIndex: cell.row, ColumnIndex: cell.col}
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
				fields[i].CellLocation = &model.CellLocation{RowIndex: cell.row, ColumnIndex: cell.col}
				matched[j] = true
				break
			}
		}
	}
}

func extractShapeTextMap(content *model.SlideContent) map[string]string {
	result := make(map[string]string)
	for _, el := range content.PageElements {
		extractShapeTexts(&el, result)
	}
	return result
}

func extractShapeTexts(el *model.PageElement, result map[string]string) {
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

func extractCellText(cell *model.TableCell) string {
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

func generateLayoutDescription(analysis model.SlideAnalysis, slideContent *model.SlideContent, fields []model.EditableFieldSummary) string {
	var contentFields []model.EditableFieldSummary
	for _, f := range fields {
		if plan.IsContentField(f.Role) {
			contentFields = append(contentFields, f)
		}
	}

	hasTable := false
	var tableRows, tableCols int
	if slideContent != nil {
		for _, el := range slideContent.PageElements {
			if el.Table != nil {
				hasTable = true
				tableRows = el.Table.Rows
				tableCols = el.Table.Columns
				break
			}
		}
	}

	cols := detectColumnCount(contentFields, slideContent)
	rows := detectRowCount(contentFields, slideContent)

	visualTypes := make(map[string]int)
	for _, ve := range analysis.VisualElements {
		if ve.Type != "shape" && ve.Type != "background_image" {
			visualTypes[ve.Type]++
		}
	}

	var parts []string

	if hasTable {
		parts = append(parts, fmt.Sprintf("tableau %dx%d", tableRows, tableCols))
	} else if cols > 1 && rows > 1 {
		parts = append(parts, fmt.Sprintf("grille %dx%d", cols, rows))
	} else if cols > 1 {
		parts = append(parts, fmt.Sprintf("%d colonnes", cols))
	} else if len(contentFields) > 0 {
		parts = append(parts, "pleine largeur")
	}

	if len(contentFields) > 0 {
		parts = append(parts, fmt.Sprintf("%d zones de contenu", len(contentFields)))
	}

	if len(visualTypes) > 0 {
		var vizParts []string
		for typ, count := range visualTypes {
			vizParts = append(vizParts, fmt.Sprintf("%d %s", count, typ))
		}
		sort.Strings(vizParts)
		parts = append(parts, strings.Join(vizParts, " + "))
	}

	return strings.Join(parts, ", ")
}

func detectColumnCount(fields []model.EditableFieldSummary, content *model.SlideContent) int {
	if content == nil || len(fields) < 2 {
		return 1
	}
	var xPositions []float64
	for _, f := range fields {
		if el := findPageElementById(content, f.ObjectID); el != nil && el.Transform != nil {
			xPositions = append(xPositions, el.Transform.TranslateX)
		}
	}
	return clusterCount(xPositions, 2000000)
}

func detectRowCount(fields []model.EditableFieldSummary, content *model.SlideContent) int {
	if content == nil || len(fields) < 2 {
		return 1
	}
	var yPositions []float64
	for _, f := range fields {
		if el := findPageElementById(content, f.ObjectID); el != nil && el.Transform != nil {
			yPositions = append(yPositions, el.Transform.TranslateY)
		}
	}
	return clusterCount(yPositions, 500000)
}

func clusterCount(values []float64, threshold float64) int {
	if len(values) == 0 {
		return 1
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	clusters := 1
	for i := 1; i < len(sorted); i++ {
		if sorted[i]-sorted[i-1] > threshold {
			clusters++
		}
	}
	return clusters
}

// deduplicateVariableNames adds numeric suffixes when multiple fields share the same variableName.
// e.g. [textShape, textShape, textShape] → [textShape, text2Shape, text3Shape]
func deduplicateVariableNames(fields []model.EditableFieldSummary) {
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
func loadSlideContent(baseDir string, slideNumber int) (*model.SlideContent, error) {
	contentPath := filepath.Join(baseDir, fmt.Sprintf("%d", slideNumber), "content.json")
	data, err := os.ReadFile(contentPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read content.json: %w", err)
	}

	var content model.SlideContent
	if err := json.Unmarshal(data, &content); err != nil {
		return nil, fmt.Errorf("failed to parse content.json: %w", err)
	}

	return &content, nil
}
