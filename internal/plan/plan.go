package plan

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"example.com/internal/model"
)

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

func LoadAnalysis(templateID string, slideNumber int) *model.SlideAnalysis {
	path := fmt.Sprintf("template/%s/%d/analysis.json", templateID, slideNumber)
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Warning: could not load analysis.json for slide %d: %v", slideNumber, err)
		return nil
	}

	var analysis model.SlideAnalysis
	if err := json.Unmarshal(data, &analysis); err != nil {
		log.Printf("Warning: could not parse analysis.json for slide %d: %v", slideNumber, err)
		return nil
	}

	return &analysis
}

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

func IsContentField(role string) bool {
	switch role {
	case "annee", "copyright", "entreprise", "numero_page", "page":
		return false
	}
	return true
}

func BuildCompactIndex(index *model.TemplateIndex) string {
	var b strings.Builder
	for _, slide := range index.Slides {
		contentFields := 0
		for _, f := range slide.EditableFields {
			if IsContentField(f.Role) {
				contentFields++
			}
		}
		fmt.Fprintf(&b, "SLIDE %d [%d champs de contenu]: %s\n", slide.SlideNumber, contentFields, slide.Intention)
		if len(slide.Keywords) > 0 {
			limit := min(len(slide.Keywords), 8)
			fmt.Fprintf(&b, "  mots-clés: %s\n", strings.Join(slide.Keywords[:limit], ", "))
		}
		if len(slide.EditableFields) > 0 {
			fmt.Fprintf(&b, "  champs éditables:\n")
			for _, f := range slide.EditableFields {
				fmt.Fprintf(&b, "    - %s (role: %s", f.VariableName, f.Role)
				if f.MaxChars > 0 {
					fmt.Fprintf(&b, ", taille: %s ~%d car.", SizeLabel(f.MaxChars), f.MaxChars)
				}
				if f.Content != "" {
					content := f.Content
					if len(content) > 50 {
						content = content[:50] + "..."
					}
					fmt.Fprintf(&b, ", contenu: %q", content)
				}
				b.WriteString(")\n")
			}
		}
	}
	return b.String()
}

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
			log.Printf("Warning: slide %d not found in template index, skipping", sr.SourceSlide)
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
			log.Printf("Warning: duplicate text %q in slide %d (keeping %s, removing from %s)",
				text, spec.SourceSlideNumber, firstVar, obj.VariableName)
			obj.NewValue = nil
			obj.Modified = false
		} else {
			seen[text] = obj.VariableName
		}
	}
}
