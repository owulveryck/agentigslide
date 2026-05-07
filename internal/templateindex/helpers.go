package templateindex

import (
	"strings"

	"github.com/owulveryck/agentigslide/internal/model"
)

// FindPageElementByID searches for a PageElement with the given objectId in the
// slide content, including elements nested inside element groups. Returns nil
// if no match is found.
func FindPageElementByID(content *model.SlideContent, objectID string) *model.PageElement {
	if content == nil {
		return nil
	}
	for i := range content.PageElements {
		if content.PageElements[i].ObjectID == objectID {
			return &content.PageElements[i]
		}
		if content.PageElements[i].ElementGroup != nil {
			for j := range content.PageElements[i].ElementGroup.Children {
				if content.PageElements[i].ElementGroup.Children[j].ObjectID == objectID {
					return &content.PageElements[i].ElementGroup.Children[j]
				}
			}
		}
	}
	return nil
}

// Position thresholds in EMU for a standard 10" x 7.5" Google Slides page
// (9,144,000 x 6,858,000 EMU). Elements above topThreshold are considered
// "Top", below bottomThreshold are "Bottom", and similarly for horizontal.
const (
	topThresholdEMU    = 1_500_000
	bottomThresholdEMU = 4_500_000
	leftThresholdEMU   = 2_000_000
	rightThresholdEMU  = 7_000_000
)

// GetSimplePosition maps an element's EMU coordinates to a coarse grid
// position like "TopLeft", "MiddleCenter", or "BottomRight".
func GetSimplePosition(transform *model.Transform) string {
	vPos := "Middle"
	if transform.TranslateY < topThresholdEMU {
		vPos = "Top"
	} else if transform.TranslateY > bottomThresholdEMU {
		vPos = "Bottom"
	}

	hPos := "Center"
	if transform.TranslateX < leftThresholdEMU {
		hPos = "Left"
	} else if transform.TranslateX > rightThresholdEMU {
		hPos = "Right"
	}

	return vPos + hPos
}

// ExtractShapeTextMap builds a mapping from ObjectID to the concatenated text
// content of each shape element in the slide, including shapes nested inside
// element groups.
func ExtractShapeTextMap(content *model.SlideContent) map[string]string {
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
