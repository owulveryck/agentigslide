package slides

import (
	"fmt"
	"strings"

	gslides "google.golang.org/api/slides/v1"
)

func CollectElementIds(page *gslides.Page) []string {
	var ids []string
	for _, el := range page.PageElements {
		ids = append(ids, CollectPageElementIds(el)...)
	}
	return ids
}

func CollectPageElementIds(el *gslides.PageElement) []string {
	ids := []string{el.ObjectId}
	if el.ElementGroup != nil {
		for _, child := range el.ElementGroup.Children {
			ids = append(ids, CollectPageElementIds(child)...)
		}
	}
	return ids
}

func BuildTextPresenceMap(pres *gslides.Presentation) map[string]bool {
	m := make(map[string]bool)
	for _, page := range pres.Slides {
		for _, el := range page.PageElements {
			if el.Shape != nil && el.Shape.Text != nil {
				if HasNonEmptyText(el.Shape.Text) {
					m[el.ObjectId] = true
				}
			}
			if el.Table != nil {
				for ri, row := range el.Table.TableRows {
					for ci, cell := range row.TableCells {
						if cell.Text != nil && HasNonEmptyText(cell.Text) {
							m[fmt.Sprintf("%s_%d_%d", el.ObjectId, ri, ci)] = true
						}
					}
				}
			}
		}
	}
	return m
}

func HasNonEmptyText(tc *gslides.TextContent) bool {
	for _, el := range tc.TextElements {
		if el.TextRun != nil {
			content := strings.TrimRight(el.TextRun.Content, "\n")
			if content != "" {
				return true
			}
		}
	}
	return false
}
