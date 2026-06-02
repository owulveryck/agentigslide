package agent

import (
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
)

// ValidateOutline checks structural consistency of the Outliner output.
func ValidateOutline(outline *PresentationOutline) error {
	if outline.PresentationTitle == "" {
		return fmt.Errorf("outline has empty presentation title")
	}
	if len(outline.Sections) == 0 {
		return fmt.Errorf("outline has no sections")
	}
	for i, sec := range outline.Sections {
		if len(sec.SlideNeeds) == 0 {
			return fmt.Errorf("section %d (%q) has no slide needs", i, sec.Title)
		}
		for j := range sec.SlideNeeds {
			need := &sec.SlideNeeds[j]
			if need.ItemCount != len(need.ContentItems) {
				slog.Warn("[validate] auto-correcting itemCount to match contentItems",
					"section", i, "slide", j,
					"was", need.ItemCount, "now", len(need.ContentItems),
				)
				need.ItemCount = len(need.ContentItems)
			}
		}
	}
	return nil
}

var (
	slideHeaderRe   = regexp.MustCompile(`(?m)^SLIDE\s+(\d+)\s+`)
	fieldNameRe     = regexp.MustCompile(`(\w+)\s+\(`)
	categoryCountRe = regexp.MustCompile(`(\d+)\s+(titre|sous-titre|contenu|numerotation)`)
)

// SlideFieldCounts holds the categorized field counts parsed from the catalog header.
type SlideFieldCounts struct {
	Titles       int
	Subtitles    int
	Contents     int
	Numerotation int
	NoFields     bool // true when header is [AUCUN CHAMP MODIFIABLE]
}

// CatalogInfo holds parsed catalog metadata for validation.
type CatalogInfo struct {
	SlideNumbers       map[int]bool
	FieldsBySlide      map[int]map[string]bool
	FieldCountsBySlide map[int]SlideFieldCounts
}

// ParseCatalog extracts slide numbers, per-slide field names, and categorized
// field counts from the compact catalog text format.
func ParseCatalog(compactCatalog string) CatalogInfo {
	info := CatalogInfo{
		SlideNumbers:       make(map[int]bool),
		FieldsBySlide:      make(map[int]map[string]bool),
		FieldCountsBySlide: make(map[int]SlideFieldCounts),
	}

	lines := strings.Split(compactCatalog, "\n")
	currentSlide := -1
	for _, line := range lines {
		if m := slideHeaderRe.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			currentSlide = n
			info.SlideNumbers[n] = true
			info.FieldsBySlide[n] = make(map[string]bool)

			bracketStart := strings.Index(line, "[")
			bracketEnd := strings.Index(line, "]")
			if bracketStart >= 0 && bracketEnd > bracketStart {
				bracketContent := line[bracketStart+1 : bracketEnd]
				var counts SlideFieldCounts
				if bracketContent == "AUCUN CHAMP MODIFIABLE" {
					counts.NoFields = true
				} else {
					for _, cm := range categoryCountRe.FindAllStringSubmatch(bracketContent, -1) {
						val, _ := strconv.Atoi(cm[1])
						switch cm[2] {
						case "titre":
							counts.Titles = val
						case "sous-titre":
							counts.Subtitles = val
						case "contenu":
							counts.Contents = val
						case "numerotation":
							counts.Numerotation = val
						}
					}
				}
				info.FieldCountsBySlide[n] = counts
			}
			continue
		}
		if currentSlide >= 0 && strings.HasPrefix(strings.TrimSpace(line), "champs:") {
			champsPart := strings.TrimPrefix(strings.TrimSpace(line), "champs:")
			for _, field := range strings.Split(champsPart, "|") {
				if m := fieldNameRe.FindStringSubmatch(field); m != nil {
					info.FieldsBySlide[currentSlide][m[1]] = true
				}
			}
		}
	}

	return info
}

var fieldDetailRe = regexp.MustCompile(`(\w+)\s+\((\S+?)(?:\s+~(\d+))?\)`)

// ParseSlideFields extracts the editable field details for a specific slide
// from the compact catalog text. It returns nil if the slide is not found.
func ParseSlideFields(compactCatalog string, slideNumber int) []TemplateField {
	lines := strings.Split(compactCatalog, "\n")
	currentSlide := -1
	for _, line := range lines {
		if m := slideHeaderRe.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			currentSlide = n
			continue
		}
		if currentSlide == slideNumber && strings.HasPrefix(strings.TrimSpace(line), "champs:") {
			champsPart := strings.TrimPrefix(strings.TrimSpace(line), "champs:")
			var fields []TemplateField
			for _, segment := range strings.Split(champsPart, "|") {
				if m := fieldDetailRe.FindStringSubmatch(segment); m != nil {
					tf := TemplateField{
						VariableName: m[1],
						Role:         m[2],
					}
					if m[3] != "" {
						tf.MaxChars, _ = strconv.Atoi(m[3])
					}
					fields = append(fields, tf)
				}
			}
			return fields
		}
	}
	return nil
}

// FlattenNeeds returns all SlideNeeds from an outline in order.
func FlattenNeeds(outline *PresentationOutline) []SlideNeed {
	var needs []SlideNeed
	for _, sec := range outline.Sections {
		needs = append(needs, sec.SlideNeeds...)
	}
	return needs
}

// ValidateSelection checks that the Selector output references valid outline
// indices and existing template slides. Field count and subtitle mismatches
// are logged as warnings since the Writer adapts to whatever template it
// receives. Out-of-range outlineIndex values are clamped with a warning.
func ValidateSelection(selections *SelectionPlan, outline *PresentationOutline, compactCatalog string) error {
	needs := FlattenNeeds(outline)
	totalNeeds := len(needs)

	catalog := ParseCatalog(compactCatalog)

	if len(selections.Selections) != totalNeeds {
		return fmt.Errorf("selection count mismatch: got %d selections but outline has %d slide needs",
			len(selections.Selections), totalNeeds)
	}

	var errs []string
	for i := range selections.Selections {
		sel := &selections.Selections[i]

		if sel.OutlineIndex < 0 || sel.OutlineIndex >= totalNeeds {
			clamped := sel.OutlineIndex
			if clamped >= totalNeeds {
				clamped = totalNeeds - 1
			}
			if clamped < 0 {
				clamped = 0
			}
			slog.Warn("[validate] clamping out-of-range outlineIndex",
				"selection", i,
				"original", sel.OutlineIndex,
				"clamped", clamped,
				"totalNeeds", totalNeeds,
			)
			sel.OutlineIndex = clamped
		}

		need := needs[sel.OutlineIndex]

		if need.SlideType == "diagram" {
			if sel.SourceSlide != -1 {
				slog.Warn("[validate] diagram slide should have sourceSlide=-1, ignoring template",
					"selection", i,
					"sourceSlide", sel.SourceSlide,
				)
				sel.SourceSlide = -1
			}
			continue
		}

		if !catalog.SlideNumbers[sel.SourceSlide] {
			errs = append(errs, fmt.Sprintf("selection %d: sourceSlide %d not found in catalog",
				i, sel.SourceSlide))
			continue
		}

		counts := catalog.FieldCountsBySlide[sel.SourceSlide]

		if counts.NoFields && need.ItemCount > 0 {
			errs = append(errs, fmt.Sprintf(
				"selection %d: sourceSlide %d has no editable fields but need has %d content items — choose a template with content zones",
				i, sel.SourceSlide, need.ItemCount))
			continue
		}

		textCapableFields := counts.Titles + counts.Subtitles + counts.Contents
		if need.ItemCount > 0 && textCapableFields == 0 {
			errs = append(errs, fmt.Sprintf(
				"selection %d: sourceSlide %d has no text-capable fields (only %d numerotation) but need has %d content items — choose a template with text fields",
				i, sel.SourceSlide, counts.Numerotation, need.ItemCount))
			continue
		}

		totalTextFields := counts.Titles + counts.Subtitles + counts.Contents
		if need.ItemCount > 0 && totalTextFields > 0 && need.ItemCount > totalTextFields*2 {
			errs = append(errs, fmt.Sprintf(
				"selection %d: sourceSlide %d has %d content items but only %d text fields (ratio > 2x) — choose a template with more content zones",
				i, sel.SourceSlide, need.ItemCount, totalTextFields))
			continue
		}
		if need.ItemCount > totalTextFields {
			slog.Warn("[validate] more content items than text fields (writer will combine)",
				"selection", i,
				"sourceSlide", sel.SourceSlide,
				"itemCount", need.ItemCount,
				"textFields", totalTextFields,
			)
		} else if need.ItemCount != counts.Contents {
			slog.Debug("[validate] itemCount differs from contenu zones (writer will adapt)",
				"selection", i,
				"sourceSlide", sel.SourceSlide,
				"itemCount", need.ItemCount,
				"contenuZones", counts.Contents,
			)
		}

		if counts.Subtitles > 0 && !need.NeedsSubtitle {
			slog.Debug("[validate] template has subtitle but needsSubtitle=false (writer will handle)",
				"selection", i,
				"sourceSlide", sel.SourceSlide,
			)
		}

		if need.NeedsTitle && counts.Titles == 0 {
			errs = append(errs, fmt.Sprintf(
				"selection %d: sourceSlide %d has no title field but needsTitle=true — choose a template with a title field",
				i, sel.SourceSlide))
			continue
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("selection validation failed:\n  %s", strings.Join(errs, "\n  "))
	}
	return nil
}

// EnforceMaxChars truncates any writer output that exceeds the maxChars
// constraint from the template fields.
func EnforceMaxChars(content *SlideContent, fields []TemplateField) {
	maxByField := make(map[string]int, len(fields))
	for _, f := range fields {
		if f.MaxChars > 0 {
			maxByField[f.VariableName] = f.MaxChars
		}
	}

	for i := range content.Modifications {
		mod := &content.Modifications[i]
		limit, ok := maxByField[mod.VariableName]
		if !ok || limit <= 0 {
			continue
		}
		text := []rune(mod.NewText)
		if len(text) <= limit {
			continue
		}
		slog.Warn("[enforceMaxChars] truncating field",
			"sourceSlide", content.SourceSlide,
			"field", mod.VariableName,
			"length", len(text),
			"maxChars", limit,
		)
		truncated := string(text[:limit])
		if idx := strings.LastIndexAny(truncated, ".!?;"); idx > limit*2/3 {
			truncated = truncated[:idx+1]
		} else if idx := strings.LastIndex(truncated, " "); idx > limit*2/3 {
			truncated = truncated[:idx]
		}
		if open := strings.Count(truncated, "**"); open%2 != 0 {
			if idx := strings.LastIndex(truncated, "**"); idx >= 0 {
				truncated = truncated[:idx]
			}
		}
		mod.NewText = strings.TrimSpace(truncated)
	}
}

// ValidateSelectionGlobal checks cross-selection constraints: section_divider
// consistency and template reuse frequency. It returns an error only for
// section_divider inconsistency (actionable by the selector); template
// duplication is logged as a warning.
func ValidateSelectionGlobal(selections *SelectionPlan, outline *PresentationOutline) error {
	needs := FlattenNeeds(outline)

	// Check section_divider consistency: all should use the same template.
	dividerTemplates := make(map[int]int) // sourceSlide -> count
	for i, sel := range selections.Selections {
		if i < len(needs) && needs[sel.OutlineIndex].SlideType == "section_divider" {
			dividerTemplates[sel.SourceSlide]++
		}
	}
	if len(dividerTemplates) > 1 {
		bestTemplate := 0
		bestCount := 0
		for tmpl, count := range dividerTemplates {
			if count > bestCount {
				bestTemplate = tmpl
				bestCount = count
			}
		}
		var others []string
		for tmpl := range dividerTemplates {
			if tmpl != bestTemplate {
				others = append(others, fmt.Sprintf("%d", tmpl))
			}
		}
		return fmt.Errorf("section_divider inconsistency: %d different templates used (%s). "+
			"All section_dividers MUST use the same template — use slide %d for all section_dividers",
			len(dividerTemplates), strings.Join(others, ", ")+" and "+fmt.Sprintf("%d", bestTemplate),
			bestTemplate)
	}

	// Warn on excessive template reuse (3+ times), ignoring diagram slides
	// and section_dividers (which intentionally reuse the same template).
	// This is advisory — if the template truly fits, reuse is acceptable.
	templateUsage := make(map[int]int)
	for i, sel := range selections.Selections {
		if sel.SourceSlide >= 0 {
			if i < len(needs) && needs[sel.OutlineIndex].SlideType != "section_divider" {
				templateUsage[sel.SourceSlide]++
			}
		}
	}
	for tmpl, count := range templateUsage {
		if count >= 3 {
			slog.Warn("[validate] template used many times — verify no better alternative exists in catalog",
				"sourceSlide", tmpl,
				"count", count,
			)
		}
	}

	return nil
}
