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
	"strings"
	"time"

	"github.com/owulveryck/slideAppScripter/internal/model"
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

// DefaultExclusions contains the default list of keywords used to identify
// internal slides (libraries, tutorials, charts) that should not be offered
// for presentation generation.
var DefaultExclusions = []string{
	"bibliothèque", "bibliotheque",
	"palette de couleurs", "charte graphique",
	"tutoriel", "checklist d'accessibilité",
	"pictogrammes", "catalogue d'icônes", "catalogue d'illustrations",
}

// LoadExclusions loads exclusion keywords from EXCLUSIONS.txt in the given
// template directory. Each non-empty line that does not start with # is treated
// as a keyword. If the file does not exist, DefaultExclusions is returned.
func LoadExclusions(templateDir string) []string {
	data, err := os.ReadFile(filepath.Join(templateDir, "EXCLUSIONS.txt"))
	if err != nil {
		return DefaultExclusions
	}
	slog.Info("loaded custom exclusions", "path", filepath.Join(templateDir, "EXCLUSIONS.txt"))
	var exclusions []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			exclusions = append(exclusions, strings.ToLower(line))
		}
	}
	if len(exclusions) == 0 {
		return DefaultExclusions
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
		contentFields := 0
		for _, f := range slide.EditableFields {
			if !IsContentField(f.Role) {
				continue
			}
			contentFields++
			part := f.VariableName + " (" + f.Role
			if f.MaxChars > 0 {
				part += fmt.Sprintf(" ~%d", f.MaxChars)
			}
			part += ")"
			contentFieldParts = append(contentFieldParts, part)
		}

		fmt.Fprintf(&b, "SLIDE %d [%d contenu]: %s\n", slide.SlideNumber, contentFields, slide.Intention)
		if slide.LayoutDescription != "" {
			fmt.Fprintf(&b, "  disposition: %s\n", slide.LayoutDescription)
		}
		if len(contentFieldParts) > 0 {
			fmt.Fprintf(&b, "  champs: %s\n", strings.Join(contentFieldParts, " | "))
		}
	}
	return b.String()
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
