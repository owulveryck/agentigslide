package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/owulveryck/agentigslide/internal/model"

	"google.golang.org/api/slides/v1"
)

// ReadPresentation reads an existing Google Slides presentation and returns
// a structured description of each slide's text content and element IDs.
func ReadPresentation(ctx context.Context, slidesAPI SlidesAPI, presentationID string) ([]model.ExistingSlideInfo, error) {
	pres, err := slidesAPI.GetPresentation(presentationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get presentation %s: %w", presentationID, err)
	}

	infos := make([]model.ExistingSlideInfo, 0, len(pres.Slides))
	for i, page := range pres.Slides {
		info := model.ExistingSlideInfo{
			Index:        i,
			PageObjectID: page.ObjectId,
		}

		for _, el := range page.PageElements {
			info.TextElements = append(info.TextElements, extractTextElements(el)...)
		}

		infos = append(infos, info)
	}

	return infos, nil
}

// extractTextElements recursively extracts text-bearing elements from a
// PageElement, including shapes, table cells, and grouped elements.
func extractTextElements(el *slides.PageElement) []model.ExistingText {
	var results []model.ExistingText

	if el.Shape != nil {
		text := extractShapeText(el.Shape)
		if text != "" {
			et := model.ExistingText{
				ObjectID:  el.ObjectId,
				Content:   text,
				ShapeType: el.Shape.ShapeType,
			}
			results = append(results, et)
		}
	}

	if el.Table != nil {
		for ri, row := range el.Table.TableRows {
			for ci, cell := range row.TableCells {
				text := extractTextContent(cell.Text)
				if text != "" {
					results = append(results, model.ExistingText{
						ObjectID:  el.ObjectId,
						Content:   text,
						ShapeType: "TABLE",
						CellLocation: &model.CellLocation{
							RowIndex:    ri,
							ColumnIndex: ci,
						},
					})
				}
			}
		}
	}

	if el.ElementGroup != nil {
		for _, child := range el.ElementGroup.Children {
			results = append(results, extractTextElements(child)...)
		}
	}

	return results
}

func extractShapeText(shape *slides.Shape) string {
	if shape == nil || shape.Text == nil {
		return ""
	}
	return extractTextContent(shape.Text)
}

func extractTextContent(tc *slides.TextContent) string {
	if tc == nil {
		return ""
	}
	var b strings.Builder
	for _, el := range tc.TextElements {
		if el.TextRun != nil {
			b.WriteString(el.TextRun.Content)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
