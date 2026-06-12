package formatter

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// CheckConsistency runs all deterministic consistency rules against the
// presentation structure and returns every issue found.
func CheckConsistency(structure []SlideInfo) []ConsistencyIssue {
	var issues []ConsistencyIssue
	issues = append(issues, checkFontFamilyByRole(structure)...)
	issues = append(issues, checkFontSizeByRole(structure)...)
	issues = append(issues, checkSizeHierarchy(structure)...)
	issues = append(issues, checkColorPalette(structure)...)
	issues = append(issues, checkBackgroundConsistency(structure)...)
	issues = append(issues, checkParagraphSpacing(structure)...)
	issues = append(issues, checkAlignmentByRole(structure)...)
	issues = append(issues, checkEmphasisCoherence(structure)...)
	issues = append(issues, checkOutlineConsistency(structure)...)
	return issues
}

// runEntry captures a text run along with the slide and element it belongs to.
type runEntry struct {
	slideIndex int
	objectID   string
	run        TextRunInfo
}

// paragraphEntry captures a paragraph along with the slide and element it belongs to.
type paragraphEntry struct {
	slideIndex int
	objectID   string
	paragraph  ParagraphInfo
}

// elementEntry captures an element along with the slide it belongs to.
type elementEntry struct {
	slideIndex int
	element    ElementInfo
}

// ---------- Rule 1: checkFontFamilyByRole ----------

func checkFontFamilyByRole(structure []SlideInfo) []ConsistencyIssue {
	byRole := groupRunsByRole(structure)
	var issues []ConsistencyIssue
	for role, entries := range byRole {
		var families []string
		for _, e := range entries {
			families = append(families, e.run.FontFamily)
		}
		expected, ok := majority(families)
		if !ok {
			continue
		}
		for _, e := range entries {
			if e.run.FontFamily != expected {
				issues = append(issues, ConsistencyIssue{
					Rule:       "FontFamilyByRole",
					SlideIndex: e.slideIndex,
					ObjectID:   e.objectID,
					Expected:   expected,
					Actual:     e.run.FontFamily,
					Severity:   "warning",
				})
				_ = role // suppress unused
			}
		}
	}
	return issues
}

// ---------- Rule 2: checkFontSizeByRole ----------

func checkFontSizeByRole(structure []SlideInfo) []ConsistencyIssue {
	byRole := groupRunsByRole(structure)
	var issues []ConsistencyIssue
	for _, entries := range byRole {
		var sizes []float64
		for _, e := range entries {
			sizes = append(sizes, e.run.FontSizePt)
		}
		expected, ok := majorityFloat(sizes, 0.5)
		if !ok {
			continue
		}
		for _, e := range entries {
			if math.Abs(e.run.FontSizePt-expected) > 0.5 {
				issues = append(issues, ConsistencyIssue{
					Rule:       "FontSizeByRole",
					SlideIndex: e.slideIndex,
					ObjectID:   e.objectID,
					Expected:   fmt.Sprintf("%.1fpt", expected),
					Actual:     fmt.Sprintf("%.1fpt", e.run.FontSizePt),
					Severity:   "warning",
				})
			}
		}
	}
	return issues
}

// ---------- Rule 3: checkSizeHierarchy ----------

func checkSizeHierarchy(structure []SlideInfo) []ConsistencyIssue {
	roleSizes := make(map[string][]float64)
	for _, slide := range structure {
		for _, elem := range slide.Elements {
			if elem.PlaceholderType == "" {
				continue
			}
			for _, run := range elem.TextRuns {
				if isWhitespaceOnly(run.Content) || run.FontSizePt == 0 {
					continue
				}
				roleSizes[elem.PlaceholderType] = append(roleSizes[elem.PlaceholderType], run.FontSizePt)
			}
		}
	}

	medians := make(map[string]float64)
	for role, sizes := range roleSizes {
		medians[role] = median(sizes)
	}

	var issues []ConsistencyIssue
	pairs := [][2]string{
		{"TITLE", "SUBTITLE"},
		{"SUBTITLE", "BODY"},
		{"TITLE", "BODY"},
	}
	for _, pair := range pairs {
		upper, hasUpper := medians[pair[0]]
		lower, hasLower := medians[pair[1]]
		if !hasUpper || !hasLower {
			continue
		}
		if upper < lower {
			issues = append(issues, ConsistencyIssue{
				Rule:       "SizeHierarchy",
				SlideIndex: -1,
				ObjectID:   "",
				Expected:   fmt.Sprintf("%s(%.1fpt) >= %s(%.1fpt)", pair[0], upper, pair[1], lower),
				Actual:     fmt.Sprintf("%s(%.1fpt) < %s(%.1fpt)", pair[0], upper, pair[1], lower),
				Severity:   "error",
			})
		}
	}
	return issues
}

// ---------- Rule 4: checkColorPalette ----------

func checkColorPalette(structure []SlideInfo) []ConsistencyIssue {
	type colorSlide struct {
		color      *RGBColor
		slideIndex int
		objectID   string
	}
	var all []colorSlide
	for _, slide := range structure {
		for _, elem := range slide.Elements {
			for _, run := range elem.TextRuns {
				if run.ForegroundColor == nil || isWhitespaceOnly(run.Content) {
					continue
				}
				all = append(all, colorSlide{
					color:      run.ForegroundColor,
					slideIndex: slide.SlideIndex,
					objectID:   elem.ObjectID,
				})
			}
		}
	}

	// Group by color similarity.
	type colorGroup struct {
		representative *RGBColor
		slides         map[int]bool
		entries        []colorSlide
	}
	var groups []colorGroup
	for _, cs := range all {
		found := false
		for i, g := range groups {
			if colorDistance(cs.color, g.representative) < 0.05 {
				groups[i].slides[cs.slideIndex] = true
				groups[i].entries = append(groups[i].entries, cs)
				found = true
				break
			}
		}
		if !found {
			groups = append(groups, colorGroup{
				representative: cs.color,
				slides:         map[int]bool{cs.slideIndex: true},
				entries:        []colorSlide{cs},
			})
		}
	}

	var issues []ConsistencyIssue
	for _, g := range groups {
		if len(g.slides) == 1 {
			for _, e := range g.entries {
				issues = append(issues, ConsistencyIssue{
					Rule:       "ColorPalette",
					SlideIndex: e.slideIndex,
					ObjectID:   e.objectID,
					Expected:   "color used on multiple slides",
					Actual:     fmt.Sprintf("rgb(%.2f,%.2f,%.2f) appears on 1 slide only", e.color.Red, e.color.Green, e.color.Blue),
					Severity:   "warning",
				})
			}
		}
	}
	return issues
}

// ---------- Rule 5: checkBackgroundConsistency ----------

func checkBackgroundConsistency(structure []SlideInfo) []ConsistencyIssue {
	byRole := groupElementsByRole(structure)
	var issues []ConsistencyIssue
	for _, entries := range byRole {
		var colors []*RGBColor
		for _, e := range entries {
			if e.element.BackgroundColor != nil {
				colors = append(colors, e.element.BackgroundColor)
			}
		}
		if len(colors) == 0 {
			continue
		}
		// Find majority color key.
		keys := make([]string, len(colors))
		for i, c := range colors {
			keys[i] = colorKey(c)
		}
		expectedKey, ok := majority(keys)
		if !ok {
			continue
		}
		// Find the actual color for the expected key.
		var expectedColor *RGBColor
		for i, k := range keys {
			if k == expectedKey {
				expectedColor = colors[i]
				break
			}
		}
		for _, e := range entries {
			if e.element.BackgroundColor == nil {
				continue
			}
			if colorKey(e.element.BackgroundColor) != expectedKey {
				issues = append(issues, ConsistencyIssue{
					Rule:       "BackgroundConsistency",
					SlideIndex: e.slideIndex,
					ObjectID:   e.element.ObjectID,
					Expected:   fmt.Sprintf("rgb(%.2f,%.2f,%.2f)", expectedColor.Red, expectedColor.Green, expectedColor.Blue),
					Actual:     fmt.Sprintf("rgb(%.2f,%.2f,%.2f)", e.element.BackgroundColor.Red, e.element.BackgroundColor.Green, e.element.BackgroundColor.Blue),
					Severity:   "warning",
				})
			}
		}
	}
	return issues
}

// ---------- Rule 6: checkParagraphSpacing ----------

func checkParagraphSpacing(structure []SlideInfo) []ConsistencyIssue {
	byRole := groupParagraphsByRole(structure)
	var issues []ConsistencyIssue
	for _, entries := range byRole {
		var lineSpacings, spaceAboves, spaceBelows []float64
		for _, e := range entries {
			// LineSpacing 0 means "inherited/unset" in the Slides API,
			// not "zero percent". Exclude from majority to avoid
			// "correcting" explicit values (like 100) down to 0.
			if e.paragraph.LineSpacing > 0 {
				lineSpacings = append(lineSpacings, e.paragraph.LineSpacing)
			}
			spaceAboves = append(spaceAboves, e.paragraph.SpaceAbovePt)
			spaceBelows = append(spaceBelows, e.paragraph.SpaceBelowPt)
		}

		type spacingCheck struct {
			name   string
			values []float64
			getter func(ParagraphInfo) float64
		}
		checks := []spacingCheck{
			{"LineSpacing", lineSpacings, func(p ParagraphInfo) float64 { return p.LineSpacing }},
			{"SpaceAbovePt", spaceAboves, func(p ParagraphInfo) float64 { return p.SpaceAbovePt }},
			{"SpaceBelowPt", spaceBelows, func(p ParagraphInfo) float64 { return p.SpaceBelowPt }},
		}
		for _, check := range checks {
			expected, ok := majorityFloat(check.values, 0.5)
			if !ok {
				continue
			}
			for _, e := range entries {
				actual := check.getter(e.paragraph)
				if math.Abs(actual-expected) > 0.5 {
					issues = append(issues, ConsistencyIssue{
						Rule:       "ParagraphSpacing",
						SlideIndex: e.slideIndex,
						ObjectID:   e.objectID,
						Expected:   fmt.Sprintf("%s=%.1fpt", check.name, expected),
						Actual:     fmt.Sprintf("%s=%.1fpt", check.name, actual),
						Severity:   "warning",
					})
				}
			}
		}
	}
	return issues
}

// ---------- Rule 7: checkAlignmentByRole ----------

func checkAlignmentByRole(structure []SlideInfo) []ConsistencyIssue {
	byRole := groupParagraphsByRole(structure)
	var issues []ConsistencyIssue
	for _, entries := range byRole {
		var alignments []string
		var filtered []paragraphEntry
		for _, e := range entries {
			if e.paragraph.Alignment != "" {
				alignments = append(alignments, e.paragraph.Alignment)
				filtered = append(filtered, e)
			}
		}
		expected, ok := majority(alignments)
		if !ok {
			continue
		}
		for _, e := range filtered {
			if e.paragraph.Alignment != expected {
				issues = append(issues, ConsistencyIssue{
					Rule:       "AlignmentByRole",
					SlideIndex: e.slideIndex,
					ObjectID:   e.objectID,
					Expected:   expected,
					Actual:     e.paragraph.Alignment,
					Severity:   "warning",
				})
			}
		}
	}
	return issues
}

// ---------- Rule 8: checkEmphasisCoherence ----------

func checkEmphasisCoherence(structure []SlideInfo) []ConsistencyIssue {
	byRole := groupRunsByRole(structure)
	var issues []ConsistencyIssue
	for _, entries := range byRole {
		var bolds, italics []string
		for _, e := range entries {
			bolds = append(bolds, fmt.Sprintf("%t", e.run.Bold))
			italics = append(italics, fmt.Sprintf("%t", e.run.Italic))
		}
		expectedBold, okB := majority(bolds)
		expectedItalic, okI := majority(italics)
		for _, e := range entries {
			if okB && fmt.Sprintf("%t", e.run.Bold) != expectedBold {
				issues = append(issues, ConsistencyIssue{
					Rule:       "EmphasisCoherence",
					SlideIndex: e.slideIndex,
					ObjectID:   e.objectID,
					Expected:   fmt.Sprintf("bold=%s", expectedBold),
					Actual:     fmt.Sprintf("bold=%t", e.run.Bold),
					Severity:   "warning",
				})
			}
			if okI && fmt.Sprintf("%t", e.run.Italic) != expectedItalic {
				issues = append(issues, ConsistencyIssue{
					Rule:       "EmphasisCoherence",
					SlideIndex: e.slideIndex,
					ObjectID:   e.objectID,
					Expected:   fmt.Sprintf("italic=%s", expectedItalic),
					Actual:     fmt.Sprintf("italic=%t", e.run.Italic),
					Severity:   "warning",
				})
			}
		}
	}
	return issues
}

// ---------- Rule 9: checkOutlineConsistency ----------

func checkOutlineConsistency(structure []SlideInfo) []ConsistencyIssue {
	byShape := make(map[string][]elementEntry)
	for _, slide := range structure {
		for _, elem := range slide.Elements {
			if elem.ShapeType == "" {
				continue
			}
			byShape[elem.ShapeType] = append(byShape[elem.ShapeType], elementEntry{
				slideIndex: slide.SlideIndex,
				element:    elem,
			})
		}
	}

	var issues []ConsistencyIssue
	for _, entries := range byShape {
		// Outline color consistency.
		var colorKeys []string
		var colorEntries []elementEntry
		for _, e := range entries {
			if e.element.OutlineColor != nil {
				colorKeys = append(colorKeys, colorKey(e.element.OutlineColor))
				colorEntries = append(colorEntries, e)
			}
		}
		if expectedKey, ok := majority(colorKeys); ok {
			var expectedColor *RGBColor
			for i, k := range colorKeys {
				if k == expectedKey {
					expectedColor = colorEntries[i].element.OutlineColor
					break
				}
			}
			for i, e := range colorEntries {
				if colorKeys[i] != expectedKey {
					issues = append(issues, ConsistencyIssue{
						Rule:       "OutlineConsistency",
						SlideIndex: e.slideIndex,
						ObjectID:   e.element.ObjectID,
						Expected:   fmt.Sprintf("rgb(%.2f,%.2f,%.2f)", expectedColor.Red, expectedColor.Green, expectedColor.Blue),
						Actual:     fmt.Sprintf("rgb(%.2f,%.2f,%.2f)", e.element.OutlineColor.Red, e.element.OutlineColor.Green, e.element.OutlineColor.Blue),
						Severity:   "warning",
					})
				}
			}
		}

		// Outline weight consistency.
		var weights []float64
		for _, e := range entries {
			if e.element.OutlineColor != nil {
				weights = append(weights, e.element.OutlineWeightPt)
			}
		}
		if expectedWeight, ok := majorityFloat(weights, 0.5); ok {
			for _, e := range colorEntries {
				if math.Abs(e.element.OutlineWeightPt-expectedWeight) > 0.5 {
					issues = append(issues, ConsistencyIssue{
						Rule:       "OutlineConsistency",
						SlideIndex: e.slideIndex,
						ObjectID:   e.element.ObjectID,
						Expected:   fmt.Sprintf("%.1fpt", expectedWeight),
						Actual:     fmt.Sprintf("%.1fpt", e.element.OutlineWeightPt),
						Severity:   "warning",
					})
				}
			}
		}
	}
	return issues
}

// ===================== Helpers =====================

// groupRunsByRole collects text runs grouped by their parent element's
// PlaceholderType. It skips elements with empty PlaceholderType, runs with
// empty FontFamily, and whitespace-only runs.
func groupRunsByRole(structure []SlideInfo) map[string][]runEntry {
	byRole := make(map[string][]runEntry)
	for _, slide := range structure {
		for _, elem := range slide.Elements {
			if elem.PlaceholderType == "" {
				continue
			}
			for _, run := range elem.TextRuns {
				if run.FontFamily == "" || isWhitespaceOnly(run.Content) {
					continue
				}
				byRole[elem.PlaceholderType] = append(byRole[elem.PlaceholderType], runEntry{
					slideIndex: slide.SlideIndex,
					objectID:   elem.ObjectID,
					run:        run,
				})
			}
		}
	}
	return byRole
}

// groupElementsByRole collects elements grouped by PlaceholderType. It skips
// elements with empty PlaceholderType.
func groupElementsByRole(structure []SlideInfo) map[string][]elementEntry {
	byRole := make(map[string][]elementEntry)
	for _, slide := range structure {
		for _, elem := range slide.Elements {
			if elem.PlaceholderType == "" {
				continue
			}
			byRole[elem.PlaceholderType] = append(byRole[elem.PlaceholderType], elementEntry{
				slideIndex: slide.SlideIndex,
				element:    elem,
			})
		}
	}
	return byRole
}

// groupParagraphsByRole collects paragraphs grouped by their parent element's
// PlaceholderType. It skips elements with empty PlaceholderType.
func groupParagraphsByRole(structure []SlideInfo) map[string][]paragraphEntry {
	byRole := make(map[string][]paragraphEntry)
	for _, slide := range structure {
		for _, elem := range slide.Elements {
			if elem.PlaceholderType == "" {
				continue
			}
			for _, para := range elem.Paragraphs {
				byRole[elem.PlaceholderType] = append(byRole[elem.PlaceholderType], paragraphEntry{
					slideIndex: slide.SlideIndex,
					objectID:   elem.ObjectID,
					paragraph:  para,
				})
			}
		}
	}
	return byRole
}

// majority returns the most frequent value among values. If values is empty,
// it returns the zero value and false.
func majority[T comparable](values []T) (T, bool) {
	if len(values) == 0 {
		var zero T
		return zero, false
	}
	counts := make(map[T]int)
	for _, v := range values {
		counts[v]++
	}
	var best T
	bestCount := 0
	for v, c := range counts {
		if c > bestCount {
			best = v
			bestCount = c
		}
	}
	return best, true
}

// majorityFloat bins float64 values within tolerance before counting. It
// returns the center value of the most frequent bin and true, or 0 and false
// if values is empty.
func majorityFloat(values []float64, tolerance float64) (float64, bool) {
	if len(values) == 0 {
		return 0, false
	}

	type bin struct {
		center float64
		count  int
	}
	var bins []bin
	for _, v := range values {
		found := false
		for i, b := range bins {
			if math.Abs(v-b.center) <= tolerance {
				// Update center to weighted average.
				bins[i].center = (b.center*float64(b.count) + v) / float64(b.count+1)
				bins[i].count++
				found = true
				break
			}
		}
		if !found {
			bins = append(bins, bin{center: v, count: 1})
		}
	}

	bestIdx := 0
	for i, b := range bins {
		if b.count > bins[bestIdx].count {
			bestIdx = i
		}
	}
	return bins[bestIdx].center, true
}

// colorDistance computes the Euclidean distance between two RGB colors.
func colorDistance(a, b *RGBColor) float64 {
	dr := a.Red - b.Red
	dg := a.Green - b.Green
	db := a.Blue - b.Blue
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

// colorKey returns a string representation of a color rounded to 2 decimal
// places, suitable for map keys.
func colorKey(c *RGBColor) string {
	return fmt.Sprintf("R:%.2f,G:%.2f,B:%.2f", c.Red, c.Green, c.Blue)
}

// median returns the median value of the given slice. It does not modify the
// input slice.
func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 0 {
		return (sorted[n/2-1] + sorted[n/2]) / 2
	}
	return sorted[n/2]
}

// isWhitespaceOnly returns true if s consists solely of whitespace characters.
func isWhitespaceOnly(s string) bool {
	return strings.TrimSpace(s) == ""
}
