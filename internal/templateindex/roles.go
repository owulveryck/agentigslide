package templateindex

import (
	"strings"

	"github.com/owulveryck/agentigslide/internal/model"
)

// InferRole returns a semantic role identifier for an editable element based on
// pattern matching against its description and content. Roles use French
// snake_case identifiers (e.g. "titre_principal", "sous_titre") to stay
// consistent with downstream consumers like [model.IsContentField].
func InferRole(elem model.EditableElement) string {
	desc := strings.ToLower(elem.Description)
	content := strings.ToLower(elem.Content)

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

	return "texte"
}

// IsPlaceholderContent reports whether the given text content is filler text
// (lorem ipsum or similar) that should not be treated as real slide content.
func IsPlaceholderContent(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	return strings.Contains(lower, "lorem ipsum") ||
		strings.Contains(lower, "dummy text") ||
		strings.Contains(lower, "dolor sit amet")
}

// roleVariablePrefixes maps roles (as returned by [InferRole]) to English
// camelCase prefixes for variable name generation. This ensures downstream
// compatibility with functions like plan.IsMainTitleField that match patterns
// such as "titleMain" in variable names.
var roleVariablePrefixes = map[string]string{
	"titre_principal": "titleMain",
	"sous_titre":      "subtitle",
	"annee":           "year",
	"entreprise":      "company",
	"sommaire":        "summary",
	"copyright":       "copyright",
	"numero_page":     "pageNumber",
	"numerotation":    "numbering",
	"liste_points":    "bulletList",
	"tableau":         "table",
	"legende":         "caption",
	"corps_texte":     "bodyText",
	"titre":           "title",
	"texte":           "text",
}

// RoleToVariablePrefix converts a role identifier to an English camelCase
// prefix suitable for generating Apps Script variable names. Returns "text"
// for unknown roles.
func RoleToVariablePrefix(role string) string {
	if prefix, ok := roleVariablePrefixes[role]; ok {
		return prefix
	}
	return "text"
}
