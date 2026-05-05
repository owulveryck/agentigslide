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
	categoryCountRe = regexp.MustCompile(`(\d+)\s+(titre|sous-titre|contenu)`)
)

// slideFieldCounts holds the categorized field counts parsed from the catalog header.
type slideFieldCounts struct {
	titles    int
	subtitles int
	contents  int
}

// catalogInfo holds parsed catalog metadata for validation.
type catalogInfo struct {
	slideNumbers       map[int]bool
	fieldsBySlide      map[int]map[string]bool
	fieldCountsBySlide map[int]slideFieldCounts
}

// parseCatalog extracts slide numbers, per-slide field names, and categorized
// field counts from the compact catalog text format.
func parseCatalog(compactCatalog string) catalogInfo {
	info := catalogInfo{
		slideNumbers:       make(map[int]bool),
		fieldsBySlide:      make(map[int]map[string]bool),
		fieldCountsBySlide: make(map[int]slideFieldCounts),
	}

	lines := strings.Split(compactCatalog, "\n")
	currentSlide := -1
	for _, line := range lines {
		if m := slideHeaderRe.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			currentSlide = n
			info.slideNumbers[n] = true
			info.fieldsBySlide[n] = make(map[string]bool)

			bracketStart := strings.Index(line, "[")
			bracketEnd := strings.Index(line, "]")
			if bracketStart >= 0 && bracketEnd > bracketStart {
				bracketContent := line[bracketStart+1 : bracketEnd]
				var counts slideFieldCounts
				for _, cm := range categoryCountRe.FindAllStringSubmatch(bracketContent, -1) {
					val, _ := strconv.Atoi(cm[1])
					switch cm[2] {
					case "titre":
						counts.titles = val
					case "sous-titre":
						counts.subtitles = val
					case "contenu":
						counts.contents = val
					}
				}
				info.fieldCountsBySlide[n] = counts
			}
			continue
		}
		if currentSlide >= 0 && strings.HasPrefix(strings.TrimSpace(line), "champs:") {
			champsPart := strings.TrimPrefix(strings.TrimSpace(line), "champs:")
			for _, field := range strings.Split(champsPart, "|") {
				if m := fieldNameRe.FindStringSubmatch(field); m != nil {
					info.fieldsBySlide[currentSlide][m[1]] = true
				}
			}
		}
	}

	return info
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
// indices, existing template slides, known field names, and matching field
// counts. Out-of-range outlineIndex values are clamped with a warning; hard
// errors are reserved for truly unrecoverable problems (unknown template,
// unknown field, field count mismatch).
func validateSelection(selections *SelectionPlan, outline *PresentationOutline, compactCatalog string) error {
	needs := flattenNeeds(outline)
	totalNeeds := len(needs)

	catalog := parseCatalog(compactCatalog)

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

		if !catalog.slideNumbers[sel.SourceSlide] {
			errs = append(errs, fmt.Sprintf("selection %d: sourceSlide %d not found in catalog",
				i, sel.SourceSlide))
			continue
		}

		slideFields := catalog.fieldsBySlide[sel.SourceSlide]
		for _, fm := range sel.FieldMapping {
			if slideFields != nil && !slideFields[fm.VariableName] {
				errs = append(errs, fmt.Sprintf("selection %d: variableName %q not found in slide %d",
					i, fm.VariableName, sel.SourceSlide))
			}
		}

		// Field count validation against the categorized catalog counts.
		need := needs[sel.OutlineIndex]
		counts := catalog.fieldCountsBySlide[sel.SourceSlide]

		if need.ItemCount != counts.contents {
			errs = append(errs, fmt.Sprintf(
				"selection %d (slide %d): itemCount=%d but template has %d contenu zones — all zones must be filled",
				i, sel.SourceSlide, need.ItemCount, counts.contents))
		}

		if counts.subtitles > 0 && !need.NeedsSubtitle {
			errs = append(errs, fmt.Sprintf(
				"selection %d (slide %d): template has %d sous-titre field(s) but needsSubtitle=false — unfilled subtitle shows placeholder",
				i, sel.SourceSlide, counts.subtitles))
		}

		if need.NeedsTitle && counts.titles == 0 {
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
