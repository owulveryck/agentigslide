// Package slides provides helper functions for working with Google Slides API
// page structures. It includes utilities for collecting element IDs from slide
// pages, building text presence maps across presentations, and checking for
// non-empty text content in elements.
package slides

import (
	"fmt"
	"strings"

	gslides "google.golang.org/api/slides/v1"
)

// CollectElementIds returns all element IDs from a Google Slides page,
// including nested children within element groups.
func CollectElementIds(page *gslides.Page) []string {
	var ids []string
	for _, el := range page.PageElements {
		ids = append(ids, CollectPageElementIds(el)...)
	}
	return ids
}

// CollectPageElementIds returns the element ID of a page element and
// recursively collects IDs from any child elements in groups.
func CollectPageElementIds(el *gslides.PageElement) []string {
	ids := []string{el.ObjectId}
	if el.ElementGroup != nil {
		for _, child := range el.ElementGroup.Children {
			ids = append(ids, CollectPageElementIds(child)...)
		}
	}
	return ids
}

// BuildTextPresenceMap scans all slides in a presentation and returns a map
// indicating which element IDs contain non-empty text. For table cells, the
// key format is "{objectId}_{rowIndex}_{columnIndex}".
func BuildTextPresenceMap(pres *gslides.Presentation) map[string]bool {
	m := make(map[string]bool)
	for _, page := range pres.Slides {
		for _, el := range page.PageElements {
			buildTextPresenceFromElement(m, el)
		}
	}
	return m
}

func buildTextPresenceFromElement(m map[string]bool, el *gslides.PageElement) {
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
	if el.ElementGroup != nil {
		for _, child := range el.ElementGroup.Children {
			buildTextPresenceFromElement(m, child)
		}
	}
}

// BuildShapeSet scans all slides in a presentation and returns a set of
// element ObjectIDs that are SHAPEs or TABLEs (the only types supporting InsertText).
func BuildShapeSet(pres *gslides.Presentation) map[string]bool {
	m := make(map[string]bool)
	for _, page := range pres.Slides {
		for _, el := range page.PageElements {
			buildShapeSetFromElement(m, el)
		}
	}
	return m
}

func buildShapeSetFromElement(m map[string]bool, el *gslides.PageElement) {
	if el.Shape != nil {
		m[el.ObjectId] = true
	}
	if el.Table != nil {
		m[el.ObjectId] = true
	}
	if el.ElementGroup != nil {
		for _, child := range el.ElementGroup.Children {
			buildShapeSetFromElement(m, child)
		}
	}
}

// HasNonEmptyText reports whether a TextContent contains at least one text run
// with non-empty content (ignoring trailing newlines).
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
