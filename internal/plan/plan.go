// Package plan provides functions for loading template indexes, building
// compact index representations for Claude prompts, and enriching AI-generated
// slide plans into fully resolved presentation plans ready for execution
// against the Google Slides API.
package plan

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/owulveryck/agentigslide/internal/model"
)

// LoadTemplateIndex reads and parses a template_index.json file at the given
// path, returning the deserialized TemplateIndex.
func LoadTemplateIndex(path string) (*model.TemplateIndex, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	var index model.TemplateIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	return &index, nil
}

// LoadAnalysis reads and parses the analysis.json file for a specific slide
// from the template directory. It returns nil if the file cannot be read or parsed.
func LoadAnalysis(templateID string, slideNumber int) *model.SlideAnalysis {
	path := fmt.Sprintf("template/%s/%d/analysis.json", templateID, slideNumber)
	data, err := os.ReadFile(path)
	if err != nil {
		slog.Warn("could not load analysis.json", "slideNumber", slideNumber, "error", err)
		return nil
	}

	var analysis model.SlideAnalysis
	if err := json.Unmarshal(data, &analysis); err != nil {
		slog.Warn("could not parse analysis.json", "slideNumber", slideNumber, "error", err)
		return nil
	}

	return &analysis
}

// SizeLabel returns a human-readable size label ("petit", "moyen", or "grand")
// based on the maximum character capacity of a text field.
func SizeLabel(maxChars int) string {
	switch {
	case maxChars <= 30:
		return "petit"
	case maxChars <= 150:
		return "moyen"
	default:
		return "grand"
	}
}

// IsContentField reports whether a field role represents user-editable content
// as opposed to metadata fields like year or copyright.
func IsContentField(role string) bool {
	switch role {
	case "annee", "copyright", "entreprise":
		return false
	}
	return true
}

// IsNumerotationField reports whether a field is a short numeric field (page
// numbers, step numbers, etc.) that cannot hold paragraph text.
func IsNumerotationField(role string, maxChars int) bool {
	switch role {
	case "numerotation", "numero_page", "page":
		return true
	}
	return maxChars > 0 && maxChars <= 10
}

// IsMainTitleField reports whether a field is the slide's main title based on
// its variableName. This is more reliable than role-based detection because
// some subtitle fields share the "titre_principal" role.
func IsMainTitleField(variableName string) bool {
	vn := strings.ToLower(variableName)
	return strings.Contains(vn, "maintitle") ||
		strings.Contains(vn, "titlemain") ||
		strings.Contains(vn, "slidetitle") ||
		strings.Contains(vn, "sectiontitle") ||
		strings.Contains(vn, "chaptertitle")
}

// IsSubtitleField reports whether a field is a subtitle based on its
// variableName.
func IsSubtitleField(variableName string) bool {
	return strings.Contains(strings.ToLower(variableName), "subtitle")
}

// DefaultExclusions contains the default list of keywords used to identify
// internal slides (libraries, tutorials, charts) that should not be offered
// for presentation generation.
var DefaultExclusions = []string{
	"bibliothèque", "bibliotheque",
	"palette de couleurs", "charte graphique",
	"tutoriel", "checklist d'accessibilité",
	"pictogrammes", "catalogue d'icônes", "catalogue d'illustrations",
}

// LoadExclusions returns the exclusion keywords to use for filtering internal
// slides. It starts with DefaultExclusions and appends any additional keywords
// found in EXCLUSIONS.txt in the given template directory. Each non-empty line
// that does not start with # is treated as a keyword.
func LoadExclusions(templateDir string) []string {
	exclusions := append([]string{}, DefaultExclusions...)
	data, err := os.ReadFile(filepath.Join(templateDir, "EXCLUSIONS.txt"))
	if err != nil {
		return exclusions
	}
	slog.Info("loaded additional exclusions", "path", filepath.Join(templateDir, "EXCLUSIONS.txt"))
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			exclusions = append(exclusions, strings.ToLower(line))
		}
	}
	return exclusions
}

func isInternalSlide(intention string, exclusions []string) bool {
	lower := strings.ToLower(intention)
	for _, kw := range exclusions {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// BuildCompactIndex generates a compact text representation of the template
// index suitable for inclusion in Claude prompts. It lists each slide with its
// layout, and editable content fields with roles and approximate character
// capacities. Internal/library slides, metadata fields, and small decoration
// fields are excluded. The seed parameter controls the shuffle order for
// reproducibility; use 0 for random ordering. The exclusions parameter
// specifies keywords for filtering internal slides; use LoadExclusions to
// obtain it.
func BuildCompactIndex(index *model.TemplateIndex, seed int64, exclusions []string) string {
	order := make([]int, 0, len(index.Slides))
	for i, slide := range index.Slides {
		if !isInternalSlide(slide.Intention, exclusions) {
			order = append(order, i)
		}
	}

	rng := rand.New(rand.NewSource(seed))
	rng.Shuffle(len(order), func(i, j int) {
		order[i], order[j] = order[j], order[i]
	})

	var b strings.Builder
	for _, idx := range order {
		slide := index.Slides[idx]

		var contentFieldParts []string
		titleFields := 0
		subtitleFields := 0
		contentFields := 0
		numerotationFields := 0
		for _, f := range slide.EditableFields {
			if !IsContentField(f.Role) {
				continue
			}
			if IsMainTitleField(f.VariableName) {
				titleFields++
			} else if IsSubtitleField(f.VariableName) {
				subtitleFields++
			} else if IsNumerotationField(f.Role, f.MaxChars) {
				numerotationFields++
			} else {
				contentFields++
			}
			part := f.VariableName + " (" + f.Role
			if f.MaxChars > 0 {
				part += fmt.Sprintf(" ~%d", f.MaxChars)
			}
			part += ")"
			contentFieldParts = append(contentFieldParts, part)
		}

		var countParts []string
		if titleFields > 0 {
			countParts = append(countParts, fmt.Sprintf("%d titre", titleFields))
		}
		if subtitleFields > 0 {
			countParts = append(countParts, fmt.Sprintf("%d sous-titre", subtitleFields))
		}
		countParts = append(countParts, fmt.Sprintf("%d contenu", contentFields))
		if numerotationFields > 0 {
			countParts = append(countParts, fmt.Sprintf("%d numerotation", numerotationFields))
		}

		categoryPart := ""
		if slide.Category != "" {
			categoryPart = " (" + slide.Category + ")"
		}

		if len(contentFieldParts) == 0 {
			fmt.Fprintf(&b, "SLIDE %d%s [AUCUN CHAMP MODIFIABLE]: %s\n", slide.SlideNumber, categoryPart, slide.Intention)
		} else {
			fmt.Fprintf(&b, "SLIDE %d%s [%s]: %s\n", slide.SlideNumber, categoryPart, strings.Join(countParts, ", "), slide.Intention)
		}
		if slide.Description != "" {
			fmt.Fprintf(&b, "  description: %s\n", truncateDescription(slide.Description))
		}
		if slide.LayoutDescription != "" {
			fmt.Fprintf(&b, "  disposition: %s\n", slide.LayoutDescription)
		}
		if len(slide.UseCaseTags) > 0 {
			fmt.Fprintf(&b, "  tags: %s\n", strings.Join(slide.UseCaseTags, ", "))
		} else if tags := topKeywords(slide.Keywords, 5); len(tags) > 0 {
			fmt.Fprintf(&b, "  tags: %s\n", strings.Join(tags, ", "))
		}
		if len(slide.VisualCaveats) > 0 {
			fmt.Fprintf(&b, "  contraintes: %s\n", strings.Join(slide.VisualCaveats, " ; "))
		}
		if len(contentFieldParts) > 0 {
			fmt.Fprintf(&b, "  champs: %s\n", strings.Join(contentFieldParts, " | "))
		}
	}
	return b.String()
}

func truncateDescription(s string) string {
	const limit = 250
	if idx := strings.Index(s, ". "); idx >= 0 && idx < limit {
		return s[:idx+1]
	}
	if len(s) <= limit {
		return s
	}
	if idx := strings.LastIndex(s[:limit], " "); idx > 0 {
		return s[:idx] + "..."
	}
	return s[:limit] + "..."
}

func topKeywords(keywords []string, n int) []string {
	if len(keywords) <= n {
		return keywords
	}
	return keywords[:n]
}

// LoadClosingSlide reads the optional CLOSING_SLIDE file from the template
// directory. The file should contain a single slide number. Returns -1 if
// the file does not exist or cannot be parsed.
func LoadClosingSlide(templateDir string) int {
	data, err := os.ReadFile(filepath.Join(templateDir, "CLOSING_SLIDE"))
	if err != nil {
		return -1
	}
	s := strings.TrimSpace(string(data))
	n, err := strconv.Atoi(s)
	if err != nil {
		slog.Warn("invalid CLOSING_SLIDE content", "value", s, "error", err)
		return -1
	}
	slog.Info("loaded closing slide from template config", "slideNumber", n)
	return n
}

// HashSeed returns a deterministic seed from a string, suitable for
// BuildCompactIndex. The same input always produces the same seed.
func HashSeed(s string) int64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return int64(h.Sum64())
}

// EnrichPlan converts a raw GenerationPlan from Claude into a fully resolved
// PresentationPlan. It maps source slide numbers to template slide IDs, loads
// analysis data for element descriptions, applies text modifications from the
// generation plan, and attaches visual object references.
func EnrichPlan(genPlan *model.GenerationPlan, index *model.TemplateIndex, templateID, userRequest string) *model.PresentationPlan {
	slidesByNumber := make(map[int]*model.TemplateSlide, len(index.Slides))
	for i := range index.Slides {
		slidesByNumber[index.Slides[i].SlideNumber] = &index.Slides[i]
	}

	output := &model.PresentationPlan{
		PresentationTitle: genPlan.PresentationTitle,
		TemplateID:        templateID,
		GeneratedAt:       time.Now().UTC().Format(time.RFC3339),
		SourceRequest:     userRequest,
	}

	for i, sr := range genPlan.Slides {
		if sr.Diagram != nil {
			spec := model.SlideSpec{
				Position:          i + 1,
				SourceSlideNumber: -1,
				Intention:         "Slide diagramme",
				Diagram:           sr.Diagram,
			}
			if sr.Diagram.Title != "" {
				spec.Description = sr.Diagram.Title
			}
			output.Slides = append(output.Slides, spec)
			continue
		}

		ts, ok := slidesByNumber[sr.SourceSlide]
		if !ok {
			slog.Warn("slide not found in template index, skipping", "slideNumber", sr.SourceSlide)
			continue
		}

		analysis := LoadAnalysis(templateID, sr.SourceSlide)

		modsByVar := make(map[string]string, len(sr.Modifications))
		for _, m := range sr.Modifications {
			modsByVar[m.VariableName] = m.NewText
		}

		analysisElementsByID := make(map[string]*model.EditableElement)
		if analysis != nil {
			for j := range analysis.EditableElements {
				analysisElementsByID[analysis.EditableElements[j].ObjectID] = &analysis.EditableElements[j]
			}
		}

		spec := model.SlideSpec{
			Position:          i + 1,
			SourceSlideNumber: ts.SlideNumber,
			SourceSlideID:     ts.SlideID,
			Intention:         ts.Intention,
			PreviewImage:      fmt.Sprintf("template/%s/%d/slide.png", templateID, ts.SlideNumber),
		}

		if analysis != nil {
			spec.Description = analysis.Description
		}

		for _, field := range ts.EditableFields {
			currentValue := field.RawContent
			if currentValue == "" {
				currentValue = field.Content
			}
			obj := model.EditableObject{
				ObjectID:     field.ObjectID,
				VariableName: field.VariableName,
				Role:         field.Role,
				ElementType:  "text",
				Placeholder:  field.Placeholder,
				CurrentValue: currentValue,
				CellLocation: field.CellLocation,
			}

			if ae, ok := analysisElementsByID[field.ObjectID]; ok {
				obj.Description = ae.Description
				obj.Location = ae.Location
				obj.ElementType = ae.Type
			}

			if newText, ok := modsByVar[field.VariableName]; ok {
				obj.NewValue = &newText
				obj.Modified = true
			}

			spec.EditableObjects = append(spec.EditableObjects, obj)
		}

		if analysis != nil {
			for _, ve := range analysis.VisualElements {
				spec.VisualObjects = append(spec.VisualObjects, model.VisualObject(ve))
			}
		} else {
			for _, ve := range ts.VisualElements {
				spec.VisualObjects = append(spec.VisualObjects, model.VisualObject{
					ObjectID: ve.ObjectID,
					Type:     ve.Type,
					Purpose:  ve.Purpose,
				})
			}
		}

		DeduplicateModifications(&spec)
		output.Slides = append(output.Slides, spec)
	}

	return output
}

// DeduplicateModifications removes duplicate text assignments within a single
// slide specification. When the same non-trivial text (more than 3 characters)
// is assigned to multiple fields, only the first assignment is kept.
func DeduplicateModifications(spec *model.SlideSpec) {
	seen := make(map[string]string)
	for i := range spec.EditableObjects {
		obj := &spec.EditableObjects[i]
		if !obj.Modified || obj.NewValue == nil {
			continue
		}
		text := strings.TrimSpace(*obj.NewValue)
		if len(text) <= 3 {
			continue
		}
		if firstVar, exists := seen[text]; exists {
			slog.Warn("duplicate text in slide", "text", text, "slideNumber", spec.SourceSlideNumber, "keeping", firstVar, "removing", obj.VariableName)
			obj.NewValue = nil
			obj.Modified = false
		} else {
			seen[text] = obj.VariableName
		}
	}
}
