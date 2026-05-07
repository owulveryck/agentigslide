package templateindex

import (
	"strings"

	"github.com/owulveryck/agentigslide/internal/model"
)

// minPrefixMatchLength is the minimum character count required for prefix-based
// cell matching. This prevents false positives from very short strings
// (e.g. "A" matching "Accenture").
const minPrefixMatchLength = 5

// ResolveTableCells matches editable fields that have an empty ObjectID to
// actual table cells from the slide content. It collects all table cells in
// row-major order and matches them to empty-ObjectID fields using content
// text comparison. Each table cell is matched at most once.
func ResolveTableCells(fields []model.EditableFieldSummary, content *model.SlideContent) {
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
			for j, cell := range cells {
				if matched[j] {
					continue
				}
				cellLower := strings.ToLower(cell.text)
				if IsPlaceholderContent(cellLower) || cellLower == "" {
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
				(len(cellLower) >= minPrefixMatchLength && strings.HasPrefix(analysisText, cellLower)) ||
				(len(analysisText) >= minPrefixMatchLength && strings.HasPrefix(cellLower, analysisText)) {
				fields[i].ObjectID = cell.tableObjectID
				fields[i].CellLocation = &model.CellLocation{RowIndex: cell.row, ColumnIndex: cell.col}
				matched[j] = true
				break
			}
		}
	}
}

// extractCellText concatenates all text run content within a table cell.
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
