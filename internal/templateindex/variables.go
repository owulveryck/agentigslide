package templateindex

import (
	"fmt"
	"strings"

	"github.com/owulveryck/agentigslide/internal/model"
)

// ToCamelCase converts a string with underscore, space, or hyphen delimiters
// to camelCase. The first segment is lowercased entirely, subsequent segments
// have their first letter capitalized. For example, "title_main" becomes
// "titleMain" and "body-text" becomes "bodyText".
func ToCamelCase(s string) string {
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

// GenerateVariableName builds a semantic variable name for an editable element
// by combining its English role prefix (from [RoleToVariablePrefix]) with an
// optional grid position suffix when multiple elements on the same slide share
// the same role. The result always ends with "Shape".
func GenerateVariableName(elem model.EditableElement, slideContent *model.SlideContent, analysis *model.SlideAnalysis) string {
	prefix := RoleToVariablePrefix(InferRole(elem))

	pageElem := FindPageElementByID(slideContent, elem.ObjectID)
	if pageElem != nil && pageElem.Transform != nil && needsPositionSuffix(elem, analysis) {
		position := GetSimplePosition(pageElem.Transform)
		prefix = prefix + position
	}

	return ToCamelCase(prefix) + "Shape"
}

// needsPositionSuffix reports whether an element needs a position suffix to
// disambiguate its variable name from other elements sharing the same role.
func needsPositionSuffix(elem model.EditableElement, analysis *model.SlideAnalysis) bool {
	role := InferRole(elem)
	count := 0
	for _, e := range analysis.EditableElements {
		if InferRole(e) == role {
			count++
		}
	}
	return count > 1
}

// DeduplicateVariableNames adds numeric suffixes when multiple fields share the
// same variableName. For example, [textShape, textShape, textShape] becomes
// [textShape, text2Shape, text3Shape]. The first occurrence keeps its original
// name; subsequent duplicates get an increasing numeric suffix.
func DeduplicateVariableNames(fields []model.EditableFieldSummary) {
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
