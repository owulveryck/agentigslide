package agent

import (
	"fmt"
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
	slideHeaderRe = regexp.MustCompile(`(?m)^SLIDE\s+(\d+)\s+`)
	fieldNameRe   = regexp.MustCompile(`(\w+)\s+\(`)
)

// catalogInfo holds parsed catalog metadata for validation.
type catalogInfo struct {
	slideNumbers  map[int]bool
	fieldsBySlide map[int]map[string]bool
}

// parseCatalog extracts slide numbers and per-slide field names from the
// compact catalog text format.
func parseCatalog(compactCatalog string) catalogInfo {
	info := catalogInfo{
		slideNumbers:  make(map[int]bool),
		fieldsBySlide: make(map[int]map[string]bool),
	}

	lines := strings.Split(compactCatalog, "\n")
	currentSlide := -1
	for _, line := range lines {
		if m := slideHeaderRe.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			currentSlide = n
			info.slideNumbers[n] = true
			info.fieldsBySlide[n] = make(map[string]bool)
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

// validateSelection checks that the Selector output references valid outline
// indices, existing template slides, and known field names.
func validateSelection(selections *SelectionPlan, outline *PresentationOutline, compactCatalog string) error {
	totalNeeds := 0
	for _, sec := range outline.Sections {
		totalNeeds += len(sec.SlideNeeds)
	}

	catalog := parseCatalog(compactCatalog)

	var errs []string
	for i, sel := range selections.Selections {
		if sel.OutlineIndex < 0 || sel.OutlineIndex >= totalNeeds {
			errs = append(errs, fmt.Sprintf("selection %d: outlineIndex %d out of range [0,%d)",
				i, sel.OutlineIndex, totalNeeds))
		}

		if !catalog.slideNumbers[sel.SourceSlide] {
			errs = append(errs, fmt.Sprintf("selection %d: sourceSlide %d not found in catalog",
				i, sel.SourceSlide))
		}

		slideFields := catalog.fieldsBySlide[sel.SourceSlide]
		for _, fm := range sel.FieldMapping {
			if slideFields != nil && !slideFields[fm.VariableName] {
				errs = append(errs, fmt.Sprintf("selection %d: variableName %q not found in slide %d",
					i, fm.VariableName, sel.SourceSlide))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("selection validation failed:\n  %s", strings.Join(errs, "\n  "))
	}
	return nil
}
