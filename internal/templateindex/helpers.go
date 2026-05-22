package templateindex

import (
	"log"
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
		if found := findElementByID(&content.PageElements[i], objectID); found != nil {
			return found
		}
	}
	return nil
}

func findElementByID(el *model.PageElement, objectID string) *model.PageElement {
	if el.ObjectID == objectID {
		return el
	}
	if el.ElementGroup != nil {
		for i := range el.ElementGroup.Children {
			if found := findElementByID(&el.ElementGroup.Children[i], objectID); found != nil {
				return found
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

// ResolveGroupIDs checks each editable element's ObjectID against content.json.
// If an ObjectID refers to an element group rather than a shape, it resolves
// it to the correct child shape ObjectID by matching text content. This handles
// analysis errors where Claude Vision used a parent group ID instead of the
// child shape ID.
func ResolveGroupIDs(elements []model.EditableElement, content *model.SlideContent) []model.EditableElement {
	if content == nil {
		return elements
	}

	resolved := make([]model.EditableElement, 0, len(elements))
	for _, elem := range elements {
		pageElem := FindPageElementByID(content, elem.ObjectID)
		if pageElem == nil || pageElem.ElementGroup == nil {
			resolved = append(resolved, elem)
			continue
		}

		childTexts := collectTextChildren(pageElem)
		if len(childTexts) == 0 {
			resolved = append(resolved, elem)
			continue
		}

		matched := matchChildByContent(elem.Content, childTexts)
		if matched != "" {
			log.Printf("Resolved group ObjectID %s → child shape %s for element %q", elem.ObjectID, matched, elem.VariableName)
			elem.ObjectID = matched
			resolved = append(resolved, elem)
		} else {
			log.Printf("Warning: could not resolve group ObjectID %s to a child shape for element %q, keeping group ID", elem.ObjectID, elem.VariableName)
			resolved = append(resolved, elem)
		}
	}
	return resolved
}

type textChild struct {
	objectID string
	text     string
}

func collectTextChildren(el *model.PageElement) []textChild {
	var result []textChild
	if el.ElementGroup == nil {
		return result
	}
	for i := range el.ElementGroup.Children {
		child := &el.ElementGroup.Children[i]
		if child.Shape != nil && child.Shape.Text != nil {
			text := shapeText(child)
			if text != "" {
				result = append(result, textChild{objectID: child.ObjectID, text: text})
			}
		}
		if child.ElementGroup != nil {
			result = append(result, collectTextChildren(child)...)
		}
	}
	return result
}

func shapeText(el *model.PageElement) string {
	if el.Shape == nil || el.Shape.Text == nil {
		return ""
	}
	var sb strings.Builder
	for _, te := range el.Shape.Text.TextElements {
		if te.TextRun != nil {
			sb.WriteString(te.TextRun.Content)
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

func matchChildByContent(elemContent string, children []textChild) string {
	elemNorm := normalizeWhitespace(elemContent)
	if elemNorm == "" && len(children) == 1 {
		return children[0].objectID
	}

	for _, child := range children {
		if normalizeWhitespace(child.text) == elemNorm {
			return child.objectID
		}
	}

	for _, child := range children {
		childNorm := normalizeWhitespace(child.text)
		if strings.HasPrefix(elemNorm, childNorm) || strings.HasPrefix(childNorm, elemNorm) {
			return child.objectID
		}
	}

	return ""
}

func normalizeWhitespace(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	prevSpace := true
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f' {
			if !prevSpace {
				sb.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		sb.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(sb.String())
}
