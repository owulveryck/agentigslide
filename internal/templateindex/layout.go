package templateindex

import (
	"fmt"
	"sort"
	"strings"

	"github.com/owulveryck/agentigslide/internal/model"
)

// GenerateLayoutDescription produces a human-readable summary of a slide's
// layout structure, including grid dimensions, content zone count, and
// visual element types. For example: "grille 3x2, 6 zones de contenu, 2 icon".
func GenerateLayoutDescription(analysis model.SlideAnalysis, slideContent *model.SlideContent, fields []model.EditableFieldSummary) string {
	var contentFields []model.EditableFieldSummary
	for _, f := range fields {
		if model.IsContentField(f.Role) {
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

	cols := DetectColumnCount(contentFields, slideContent)
	rows := DetectRowCount(contentFields, slideContent)

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

// columnClusterThresholdEMU is the minimum horizontal distance in EMU between
// two elements for them to be considered in different columns.
const columnClusterThresholdEMU = 2_000_000

// rowClusterThresholdEMU is the minimum vertical distance in EMU between two
// elements for them to be considered in different rows.
const rowClusterThresholdEMU = 500_000

// DetectColumnCount estimates the number of content columns by clustering
// field X positions. Returns 1 if fewer than 2 fields or no content data.
func DetectColumnCount(fields []model.EditableFieldSummary, content *model.SlideContent) int {
	if content == nil || len(fields) < 2 {
		return 1
	}
	var xPositions []float64
	for _, f := range fields {
		if el := FindPageElementByID(content, f.ObjectID); el != nil && el.Transform != nil {
			xPositions = append(xPositions, el.Transform.TranslateX)
		}
	}
	return ClusterCount(xPositions, columnClusterThresholdEMU)
}

// DetectRowCount estimates the number of content rows by clustering field Y
// positions. Returns 1 if fewer than 2 fields or no content data.
func DetectRowCount(fields []model.EditableFieldSummary, content *model.SlideContent) int {
	if content == nil || len(fields) < 2 {
		return 1
	}
	var yPositions []float64
	for _, f := range fields {
		if el := FindPageElementByID(content, f.ObjectID); el != nil && el.Transform != nil {
			yPositions = append(yPositions, el.Transform.TranslateY)
		}
	}
	return ClusterCount(yPositions, rowClusterThresholdEMU)
}

// ClusterCount counts the number of distinct clusters in a sorted list of
// values, where consecutive values differing by more than threshold are
// considered separate clusters. Returns 1 for empty input.
func ClusterCount(values []float64, threshold float64) int {
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
