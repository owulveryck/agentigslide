package agent

import (
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
)

// validateOutline checks structural consistency of the Outliner output.
func validateOutline(outline *PresentationOutline) error {
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
		for j, need := range sec.SlideNeeds {
			if need.ItemCount != len(need.ContentItems) {
				return fmt.Errorf("section %d slide %d: itemCount=%d but len(contentItems)=%d",
					i, j, need.ItemCount, len(need.ContentItems))
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

// flattenNeeds returns all SlideNeeds from an outline in order.
func flattenNeeds(outline *PresentationOutline) []SlideNeed {
	var needs []SlideNeed
	for _, sec := range outline.Sections {
		needs = append(needs, sec.SlideNeeds...)
	}
	return needs
}

// validateSelection checks that the Selector output references valid outline
// indices and existing template slides. Field count and subtitle mismatches
// are logged as warnings since the Writer adapts to whatever template it
// receives. Out-of-range outlineIndex values are clamped with a warning.
func validateSelection(selections *SelectionPlan, outline *PresentationOutline, compactCatalog string) error {
	needs := flattenNeeds(outline)
	totalNeeds := len(needs)

	catalog := ParseCatalog(compactCatalog)

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

		if !catalog.SlideNumbers[sel.SourceSlide] {
			errs = append(errs, fmt.Sprintf("selection %d: sourceSlide %d not found in catalog",
				i, sel.SourceSlide))
			continue
		}

		need := needs[sel.OutlineIndex]
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

		if need.ItemCount != counts.Contents {
			slog.Warn("[validate] itemCount mismatch (writer will adapt)",
				"selection", i,
				"sourceSlide", sel.SourceSlide,
				"itemCount", need.ItemCount,
				"contenuZones", counts.Contents,
			)
		}

		if counts.Subtitles > 0 && !need.NeedsSubtitle {
			slog.Warn("[validate] template has subtitle but needsSubtitle=false (writer will handle)",
				"selection", i,
				"sourceSlide", sel.SourceSlide,
			)
		}

		if need.NeedsTitle && counts.Titles == 0 {
			slog.Warn("[validate] template has no title field but needsTitle=true",
				"selection", i,
				"sourceSlide", sel.SourceSlide,
			)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("selection validation failed:\n  %s", strings.Join(errs, "\n  "))
	}
	return nil
}
