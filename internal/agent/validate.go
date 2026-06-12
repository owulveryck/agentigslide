package agent

import (
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/owulveryck/agentigslide/internal/model"
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
	SlideNumbers        map[int]bool
	FieldsBySlide       map[int]map[string]bool
	FieldCountsBySlide  map[int]SlideFieldCounts
	FieldDetailsBySlide map[int][]TemplateField
}

// ParseCatalog extracts slide numbers, per-slide field names, and categorized
// field counts from the compact catalog text format.
func ParseCatalog(compactCatalog string) CatalogInfo {
	info := CatalogInfo{
		SlideNumbers:        make(map[int]bool),
		FieldsBySlide:       make(map[int]map[string]bool),
		FieldCountsBySlide:  make(map[int]SlideFieldCounts),
		FieldDetailsBySlide: make(map[int][]TemplateField),
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
				if m := fieldDetailRe.FindStringSubmatch(field); m != nil {
					tf := TemplateField{VariableName: m[1], Role: m[2]}
					if m[3] != "" {
						tf.MaxChars, _ = strconv.Atoi(m[3])
					}
					if m[4] != "" && m[5] != "" {
						tf.Lines, _ = strconv.Atoi(m[4])
						tf.CharsPerLine, _ = strconv.Atoi(m[5])
					}
					info.FieldDetailsBySlide[currentSlide] = append(info.FieldDetailsBySlide[currentSlide], tf)
				}
			}
		}
	}

	return info
}

var fieldDetailRe = regexp.MustCompile(`(\w+)\s+\((\S+?)(?:\s+~(\d+))?(?:\s+(\d+)Lx(\d+)C)?\)`)

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
					if m[4] != "" && m[5] != "" {
						tf.Lines, _ = strconv.Atoi(m[4])
						tf.CharsPerLine, _ = strconv.Atoi(m[5])
					}
					fields = append(fields, tf)
				}
			}
			return fields
		}
	}
	return nil
}

// NeedConstraints returns a human-readable constraint string for a SlideNeed,
// to be injected into the selector prompt so the LLM avoids picking
// incompatible templates.
func NeedConstraints(need SlideNeed) string {
	if need.SlideType == "diagram" {
		return "sourceSlide=-1 (diagram)"
	}
	var parts []string
	if need.NeedsTitle {
		parts = append(parts, "DOIT avoir un champ titre")
	}
	if need.NeedsSubtitle {
		parts = append(parts, "DOIT avoir un champ sous-titre")
	}
	if need.ItemCount > 0 {
		parts = append(parts, fmt.Sprintf("DOIT avoir >= %d zones contenu", need.ItemCount))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}

// slideCompatible applies the same deterministic compatibility checks as
// ValidateSelection, so eligibility lists shown to the selector can never
// contradict the validation that follows.
func slideCompatible(need SlideNeed, counts SlideFieldCounts, fields []TemplateField) bool {
	if counts.NoFields && need.ItemCount > 0 {
		return false
	}
	textFields := counts.Titles + counts.Subtitles + counts.Contents
	if need.ItemCount > 0 && textFields == 0 {
		return false
	}
	if need.ItemCount > 0 && textFields > 0 && need.ItemCount > textFields*2 {
		return false
	}
	if need.NeedsTitle && counts.Titles == 0 {
		return false
	}
	if stepCount := countStepGroups(fields); stepCount > 0 && need.ItemCount > stepCount {
		return false
	}
	return true
}

// EligibleSlidesForNeed returns the catalog slide numbers that pass every
// deterministic compatibility check for the given need, in ascending order.
func EligibleSlidesForNeed(need SlideNeed, catalog *CatalogInfo) []int {
	if need.SlideType == "diagram" {
		return nil
	}
	var out []int
	for num, counts := range catalog.FieldCountsBySlide {
		if slideCompatible(need, counts, catalog.FieldDetailsBySlide[num]) {
			out = append(out, num)
		}
	}
	sort.Ints(out)
	return out
}

func joinInts(nums []int, sep string) string {
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, sep)
}

// NeedConstraintsWithCatalog extends NeedConstraints with the list of
// eligible template slide numbers computed by the same checks as
// ValidateSelection. When most of the catalog is eligible, the (shorter)
// list of ineligible slides is emitted instead.
func NeedConstraintsWithCatalog(need SlideNeed, catalog *CatalogInfo) string {
	base := NeedConstraints(need)
	if base == "" || need.SlideType == "diagram" {
		return base
	}
	eligible := EligibleSlidesForNeed(need, catalog)
	if len(eligible) == 0 {
		return base
	}
	const maxList = 40
	if len(eligible) <= maxList {
		base += fmt.Sprintf(" (slides éligibles : %s)", joinInts(eligible, ", "))
		return base
	}
	eligibleSet := make(map[int]bool, len(eligible))
	for _, n := range eligible {
		eligibleSet[n] = true
	}
	var ineligible []int
	for num := range catalog.FieldCountsBySlide {
		if !eligibleSet[num] {
			ineligible = append(ineligible, num)
		}
	}
	if len(ineligible) > 0 && len(ineligible) <= maxList {
		sort.Ints(ineligible)
		base += fmt.Sprintf(" (tous les slides du catalogue SAUF : %s)", joinInts(ineligible, ", "))
	}
	return base
}

// FlattenNeeds returns all SlideNeeds from an outline in order.
func FlattenNeeds(outline *PresentationOutline) []SlideNeed {
	var needs []SlideNeed
	for _, sec := range outline.Sections {
		needs = append(needs, sec.SlideNeeds...)
	}
	return needs
}

// SelectionIssue describes one structurally invalid selection entry, with
// enough context for a targeted (partial) selector retry.
type SelectionIssue struct {
	SelectionIndex int
	OutlineIndex   int
	Reason         string
}

// ValidateSelection checks that the Selector output references valid outline
// indices and existing template slides. Field count and subtitle mismatches
// are logged as warnings since the Writer adapts to whatever template it
// receives. Out-of-range outlineIndex values are clamped with a warning.
func ValidateSelection(selections *SelectionPlan, outline *PresentationOutline, compactCatalog string) error {
	issues, err := ValidateSelectionDetailed(selections, outline, compactCatalog)
	if err != nil {
		return err
	}
	if len(issues) > 0 {
		errs := make([]string, len(issues))
		for i, issue := range issues {
			errs[i] = fmt.Sprintf("selection %d: %s", issue.SelectionIndex, issue.Reason)
		}
		return fmt.Errorf("selection validation failed:\n  %s", strings.Join(errs, "\n  "))
	}
	return nil
}

// ValidateSelectionDetailed returns one SelectionIssue per structurally
// invalid selection entry. A selection count mismatch is returned as error
// because it cannot be repaired entry by entry.
func ValidateSelectionDetailed(selections *SelectionPlan, outline *PresentationOutline, compactCatalog string) ([]SelectionIssue, error) {
	needs := FlattenNeeds(outline)
	totalNeeds := len(needs)

	catalog := ParseCatalog(compactCatalog)

	if len(selections.Selections) != totalNeeds {
		return nil, fmt.Errorf("selection count mismatch: got %d selections but outline has %d slide needs",
			len(selections.Selections), totalNeeds)
	}

	var issues []SelectionIssue
	addIssue := func(selIdx, outlineIdx int, format string, args ...any) {
		issues = append(issues, SelectionIssue{
			SelectionIndex: selIdx,
			OutlineIndex:   outlineIdx,
			Reason:         fmt.Sprintf(format, args...),
		})
	}

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
			addIssue(i, sel.OutlineIndex, "sourceSlide %d not found in catalog", sel.SourceSlide)
			continue
		}

		counts := catalog.FieldCountsBySlide[sel.SourceSlide]

		if counts.NoFields && need.ItemCount > 0 {
			addIssue(i, sel.OutlineIndex,
				"sourceSlide %d has no editable fields but need has %d content items — choose a template with content zones",
				sel.SourceSlide, need.ItemCount)
			continue
		}

		textCapableFields := counts.Titles + counts.Subtitles + counts.Contents
		if need.ItemCount > 0 && textCapableFields == 0 {
			addIssue(i, sel.OutlineIndex,
				"sourceSlide %d has no text-capable fields (only %d numerotation) but need has %d content items — choose a template with text fields",
				sel.SourceSlide, counts.Numerotation, need.ItemCount)
			continue
		}

		totalTextFields := counts.Titles + counts.Subtitles + counts.Contents
		if need.ItemCount > 0 && totalTextFields > 0 && need.ItemCount > totalTextFields*2 {
			addIssue(i, sel.OutlineIndex,
				"sourceSlide %d has %d content items but only %d text fields (ratio > 2x) — choose a template with more content zones",
				sel.SourceSlide, need.ItemCount, totalTextFields)
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
			candidates := findTemplatesWithTitle(catalog)
			hint := ""
			if len(candidates) > 0 {
				if len(candidates) > 10 {
					candidates = candidates[:10]
				}
				hint = fmt.Sprintf(" (exemples avec titre : %s)", strings.Join(candidates, ", "))
			}
			addIssue(i, sel.OutlineIndex,
				"sourceSlide %d has no title field but needsTitle=true — choose a template with a title field%s",
				sel.SourceSlide, hint)
			continue
		}

		fields := ParseSlideFields(compactCatalog, sel.SourceSlide)

		if stepCount := countStepGroups(fields); stepCount > 0 && need.ItemCount > stepCount {
			addIssue(i, sel.OutlineIndex,
				"sourceSlide %d has %d step groups but need has %d items — choose a template with more steps or split across multiple slides",
				sel.SourceSlide, stepCount, need.ItemCount)
			continue
		}

		if need.MaxItemLength > 0 {
			for _, f := range fields {
				if f.MaxChars > 0 && f.MaxChars < 40 && f.MaxChars < need.MaxItemLength/2 {
					slog.Warn("[validate] card/small field may be too small for content",
						"selection", i,
						"sourceSlide", sel.SourceSlide,
						"field", f.VariableName,
						"maxChars", f.MaxChars,
						"maxItemLength", need.MaxItemLength,
					)
				}
			}
		}
	}

	return issues, nil
}

var stepGroupRe = regexp.MustCompile(`^step(\d+)`)

func countStepGroups(fields []TemplateField) int {
	seen := make(map[string]bool)
	for _, f := range fields {
		if m := stepGroupRe.FindStringSubmatch(strings.ToLower(f.VariableName)); m != nil {
			seen[m[1]] = true
		}
	}
	return len(seen)
}

func findTemplatesWithTitle(catalog CatalogInfo) []string {
	var nums []int
	for slideNum, counts := range catalog.FieldCountsBySlide {
		if counts.Titles > 0 {
			nums = append(nums, slideNum)
		}
	}
	sort.Ints(nums)
	candidates := make([]string, len(nums))
	for i, n := range nums {
		candidates[i] = fmt.Sprintf("%d", n)
	}
	return candidates
}

// effectiveFieldLimit returns the hard character ceiling for a field: 100%
// of maxChars for body text, 90% for titles (margin preservation).
func effectiveFieldLimit(maxChars int, role string) int {
	if strings.HasPrefix(role, "titre") {
		return maxChars * 9 / 10
	}
	return maxChars
}

// FieldOverrun describes a writer modification whose text exceeds the
// effective limit that EnforceMaxChars would truncate to.
type FieldOverrun struct {
	VariableName string
	Length       int
	Limit        int
}

// OverLimitFields returns the modifications that exceed their effective
// per-field limit, using the same limits as EnforceMaxChars. It lets the
// orchestrator re-ask the writer for a shorter version before resorting to
// hard truncation.
func OverLimitFields(content *SlideContent, fields []TemplateField) []FieldOverrun {
	limitByField := make(map[string]int, len(fields))
	for _, f := range fields {
		if f.MaxChars > 0 {
			limitByField[f.VariableName] = effectiveFieldLimit(f.MaxChars, f.Role)
		}
	}
	var overruns []FieldOverrun
	for _, mod := range content.Modifications {
		limit, ok := limitByField[mod.VariableName]
		if !ok || limit <= 0 {
			continue
		}
		if length := len([]rune(mod.NewText)); length > limit {
			overruns = append(overruns, FieldOverrun{
				VariableName: mod.VariableName,
				Length:       length,
				Limit:        limit,
			})
		}
	}
	return overruns
}

// EnforceMaxChars truncates any writer output that exceeds the maxChars
// constraint from the template fields. The writer schema already guides
// the LLM toward a lower target (60-80%); enforcement is the hard ceiling
// at 100% of maxChars.
func EnforceMaxChars(content *SlideContent, fields []TemplateField) {
	type fieldInfo struct {
		maxChars int
		role     string
	}
	infoByField := make(map[string]fieldInfo, len(fields))
	for _, f := range fields {
		if f.MaxChars > 0 {
			infoByField[f.VariableName] = fieldInfo{maxChars: f.MaxChars, role: f.Role}
		}
	}

	for i := range content.Modifications {
		mod := &content.Modifications[i]
		info, ok := infoByField[mod.VariableName]
		if !ok || info.maxChars <= 0 {
			continue
		}
		limit := effectiveFieldLimit(info.maxChars, info.role)
		text := []rune(mod.NewText)
		if len(text) <= limit {
			continue
		}
		slog.Warn("[enforceMaxChars] truncating field",
			"sourceSlide", content.SourceSlide,
			"field", mod.VariableName,
			"length", len(text),
			"maxChars", info.maxChars,
			"effectiveLimit", limit,
		)
		truncated := string(text[:limit])
		minKeep := limit * 3 / 4
		if minKeep < 1 {
			minKeep = 1
		}
		minKeepBytes := len(string(text[:minKeep]))
		if idx := strings.LastIndexAny(truncated, ".!?;"); idx >= minKeepBytes {
			truncated = truncated[:idx+1]
		} else if idx := strings.LastIndex(truncated, " "); idx >= minKeepBytes {
			truncated = strings.TrimSpace(truncated[:idx]) + "…"
		}
		if open := strings.Count(truncated, "**"); open%2 != 0 {
			if idx := strings.LastIndex(truncated, "**"); idx >= 0 {
				truncated = truncated[:idx]
			}
		}
		mod.NewText = strings.TrimSpace(truncated)
	}
}

// PreReviewValidation runs deterministic checks on the assembled plan before
// sending it to the LLM reviewer. It catches issues that don't need an LLM:
// out-of-catalog slide numbers, duplicate content, empty slides, and deck
// invariant violations (ADR 029).
func PreReviewValidation(plan *model.GenerationPlan, compactCatalog string, inv DeckInvariants) []ReviewIssue {
	catalog := ParseCatalog(compactCatalog)
	var issues []ReviewIssue

	for i, slide := range plan.Slides {
		if slide.Diagram != nil {
			continue
		}

		// Configured cover/closing slides may legitimately be absent from the
		// compact catalog (decorative, no editable fields) and may carry no
		// modifications: they are deck invariants, not content slides.
		isInvariantSlide := (inv.CoverSlide > 0 && slide.SourceSlide == inv.CoverSlide) ||
			(inv.ClosingSlide > 0 && slide.SourceSlide == inv.ClosingSlide)

		if slide.SourceSlide > 0 && !isInvariantSlide && !catalog.SlideNumbers[slide.SourceSlide] {
			issues = append(issues, ReviewIssue{
				SlideIndex:  i,
				IssueType:   "wrong_template",
				Description: fmt.Sprintf("sourceSlide %d does not exist in catalog", slide.SourceSlide),
				Suggestion:  "Choose a valid template from the catalog",
			})
		}

		if slide.SourceSlide >= 0 && !isInvariantSlide && len(slide.Modifications) == 0 {
			issues = append(issues, ReviewIssue{
				SlideIndex:  i,
				IssueType:   "missing_content",
				Description: "slide has no text modifications",
				Suggestion:  "Fill in text content for this slide",
			})
		}
	}

	issues = append(issues, ValidateDeckInvariants(plan, inv)...)
	issues = append(issues, CheckTextHeuristics(plan, catalog)...)

	type contentKey struct {
		sourceSlide int
		textHash    string
	}
	seen := make(map[contentKey]int)
	for i, slide := range plan.Slides {
		if slide.Diagram != nil || len(slide.Modifications) == 0 {
			continue
		}
		var texts []string
		for _, mod := range slide.Modifications {
			texts = append(texts, mod.NewText)
		}
		key := contentKey{sourceSlide: slide.SourceSlide, textHash: strings.Join(texts, "|")}
		if prev, ok := seen[key]; ok {
			issues = append(issues, ReviewIssue{
				SlideIndex:  i,
				IssueType:   "duplicate",
				Description: fmt.Sprintf("slide %d has identical content and template as slide %d", i, prev),
				Suggestion:  fmt.Sprintf("Remove duplicate slide %d or differentiate its content", i),
			})
		} else {
			seen[key] = i
		}
	}

	return issues
}

// bulletLineRe matches a markdown bullet at the start of a line.
var bulletLineRe = regexp.MustCompile(`(?m)^\s*-\s+`)

// CheckTextHeuristics runs the deterministic text-shape checks that used to
// live in the reviewer prompt (ADR 030): bullet lists in fields that cannot
// hold them, and text whose line count physically exceeds the box geometry.
// Reliable since ADR 027 guarantees the geometry behind every capacity.
func CheckTextHeuristics(plan *model.GenerationPlan, catalog CatalogInfo) []ReviewIssue {
	var issues []ReviewIssue
	for i, slide := range plan.Slides {
		if slide.Diagram != nil {
			continue
		}
		fieldByName := make(map[string]TemplateField)
		for _, f := range catalog.FieldDetailsBySlide[slide.SourceSlide] {
			fieldByName[f.VariableName] = f
		}
		for _, mod := range slide.Modifications {
			f, ok := fieldByName[mod.VariableName]
			if !ok {
				continue
			}
			hasBullets := bulletLineRe.MatchString(mod.NewText)
			if hasBullets && (strings.HasPrefix(f.Role, "titre") || strings.HasPrefix(f.Role, "sous_titre") ||
				f.Role == "numerotation" || f.Role == "numero_page" ||
				(f.MaxChars > 0 && f.MaxChars < 150)) {
				issues = append(issues, ReviewIssue{
					SlideIndex:  i,
					Field:       mod.VariableName,
					IssueType:   "inappropriate_bullets",
					Description: fmt.Sprintf("bullet list in field %s (role %s, ~%d chars) which cannot hold one", mod.VariableName, f.Role, f.MaxChars),
					Suggestion:  "Rewrite as continuous prose without bullet markers",
				})
				continue
			}
			if f.Lines > 0 && f.CharsPerLine > 0 {
				if needed := estimateLinesNeeded(mod.NewText, f.CharsPerLine); needed > f.Lines {
					issues = append(issues, ReviewIssue{
						SlideIndex:  i,
						Field:       mod.VariableName,
						IssueType:   "text_density",
						Description: fmt.Sprintf("text needs ~%d lines but field %s has only %d (%d chars/line)", needed, mod.VariableName, f.Lines, f.CharsPerLine),
						Suggestion:  "Shorten the text or remove line breaks so it fits the box",
					})
					continue
				}
			}
			if f.MaxChars > 0 && strings.Contains(mod.NewText, "\n") &&
				len([]rune(mod.NewText)) > f.MaxChars*9/10 {
				issues = append(issues, ReviewIssue{
					SlideIndex:  i,
					Field:       mod.VariableName,
					IssueType:   "text_density",
					Description: fmt.Sprintf("text fills >90%% of field %s (~%d chars) and uses line breaks", mod.VariableName, f.MaxChars),
					Suggestion:  "Reduce the content or drop the line breaks to regain space",
				})
			}
		}
	}
	return issues
}

// estimateLinesNeeded counts the display lines a text needs in a box with the
// given characters-per-line, accounting for hard line breaks and word wrap.
func estimateLinesNeeded(text string, charsPerLine int) int {
	if charsPerLine <= 0 {
		return 0
	}
	lines := 0
	for _, hard := range strings.Split(text, "\n") {
		n := len([]rune(hard))
		if n == 0 {
			lines++
			continue
		}
		lines += (n + charsPerLine - 1) / charsPerLine
	}
	return lines
}

// DroppedIssue is a reviewer finding discarded by the deterministic
// cross-check, with the reason it was judged a false positive.
type DroppedIssue struct {
	Issue  ReviewIssue
	Reason string
}

// CrossCheckReviewIssues verifies every reviewer finding of a computable type
// against ground truth before any correction is engaged (ADR 030). An LLM
// judge can hallucinate facts (a slide "missing from the catalog" that exists,
// an "overflow" within budget): such findings must never trigger a rewrite.
// Findings the code cannot verify are kept.
func CrossCheckReviewIssues(issues []ReviewIssue, plan *model.GenerationPlan, catalog CatalogInfo, inv DeckInvariants) (kept []ReviewIssue, dropped []DroppedIssue) {
	for _, issue := range issues {
		inRange := issue.SlideIndex >= 0 && issue.SlideIndex < len(plan.Slides)

		if inRange {
			src := plan.Slides[issue.SlideIndex].SourceSlide
			if (inv.CoverSlide > 0 && src == inv.CoverSlide) || (inv.ClosingSlide > 0 && src == inv.ClosingSlide) {
				dropped = append(dropped, DroppedIssue{Issue: issue,
					Reason: fmt.Sprintf("targets configured invariant slide (sourceSlide %d) which is applied by construction", src)})
				continue
			}
		}

		switch issue.IssueType {
		case "wrong_template":
			if inRange {
				src := plan.Slides[issue.SlideIndex].SourceSlide
				if src == -1 || catalog.SlideNumbers[src] {
					dropped = append(dropped, DroppedIssue{Issue: issue,
						Reason: fmt.Sprintf("sourceSlide %d does exist in the catalog — reviewer claim is false", src)})
					continue
				}
			}
		case "overflow":
			if inRange && issue.Field != "" {
				slide := plan.Slides[issue.SlideIndex]
				var f *TemplateField
				for _, tf := range catalog.FieldDetailsBySlide[slide.SourceSlide] {
					if tf.VariableName == issue.Field {
						f = &tf
						break
					}
				}
				var text string
				found := false
				for _, mod := range slide.Modifications {
					if mod.VariableName == issue.Field {
						text = mod.NewText
						found = true
						break
					}
				}
				if f != nil && f.MaxChars > 0 && found {
					if length := len([]rune(text)); length <= effectiveFieldLimit(f.MaxChars, f.Role) {
						dropped = append(dropped, DroppedIssue{Issue: issue,
							Reason: fmt.Sprintf("field %s holds %d chars within its limit of %d — reviewer claim is false", issue.Field, length, effectiveFieldLimit(f.MaxChars, f.Role))})
						continue
					}
				}
			}
		}

		kept = append(kept, issue)
	}
	return kept, dropped
}

// ValidateDeckInvariants verifies the deck-level structural rules declared by
// the template configuration (ADR 029). These issues use SlideIndex=-1 so the
// correction router never sends them to content writers: they are enforced by
// construction in the orchestrator's assemble step; a violation here means a
// bug in the enforcement, not a content problem.
func ValidateDeckInvariants(plan *model.GenerationPlan, inv DeckInvariants) []ReviewIssue {
	var issues []ReviewIssue
	if len(plan.Slides) == 0 {
		return issues
	}

	if inv.CoverSlide > 0 && plan.Slides[0].SourceSlide != inv.CoverSlide {
		issues = append(issues, ReviewIssue{
			SlideIndex:  -1,
			IssueType:   "deck_invariant",
			Description: fmt.Sprintf("first slide must be the official cover (sourceSlide %d), got %d", inv.CoverSlide, plan.Slides[0].SourceSlide),
		})
	}

	if inv.ClosingSlide > 0 {
		last := plan.Slides[len(plan.Slides)-1]
		if last.SourceSlide != inv.ClosingSlide {
			issues = append(issues, ReviewIssue{
				SlideIndex:  -1,
				IssueType:   "deck_invariant",
				Description: fmt.Sprintf("last slide must be the official closing slide (sourceSlide %d), got %d", inv.ClosingSlide, last.SourceSlide),
			})
		}
		for i, s := range plan.Slides[:len(plan.Slides)-1] {
			if s.SourceSlide == inv.ClosingSlide {
				issues = append(issues, ReviewIssue{
					SlideIndex:  -1,
					IssueType:   "deck_invariant",
					Description: fmt.Sprintf("closing slide (sourceSlide %d) appears at index %d instead of last position only", inv.ClosingSlide, i),
				})
			}
		}
	}

	if inv.SummarySlide > 0 {
		found := false
		for _, s := range plan.Slides {
			if s.SourceSlide == inv.SummarySlide {
				found = true
				break
			}
		}
		if !found {
			issues = append(issues, ReviewIssue{
				SlideIndex:  -1,
				IssueType:   "deck_invariant",
				Description: fmt.Sprintf("mandatory summary slide (sourceSlide %d) is absent from the deck", inv.SummarySlide),
			})
		}
	}

	return issues
}

var reSourceSlide = regexp.MustCompile(`(?i)(?:source\s*slide|SLIDE)\s*(?::?\s*)(\d+)`)

// ParseTemplateSuggestion extracts a sourceSlide number from a reviewer
// suggestion string (e.g. "Remplacer par sourceSlide 250" or "use SLIDE 83").
func ParseTemplateSuggestion(suggestion string) (int, bool) {
	m := reSourceSlide.FindStringSubmatch(suggestion)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// SanitizeSelection removes or replaces entries that are structurally
// incompatible with their SlideNeed. It is called as a last resort after
// the selector fails max retries, to avoid propagating invalid selections.
func SanitizeSelection(selections *SelectionPlan, outline *PresentationOutline, compactCatalog string) int {
	catalog := ParseCatalog(compactCatalog)
	needs := FlattenNeeds(outline)
	fixed := 0
	valid := selections.Selections[:0]
	for _, sel := range selections.Selections {
		if sel.SourceSlide == -1 {
			valid = append(valid, sel)
			continue
		}
		if !catalog.SlideNumbers[sel.SourceSlide] {
			slog.Warn("[sanitize] dropping selection with non-existent template",
				"sourceSlide", sel.SourceSlide,
				"outlineIndex", sel.OutlineIndex,
			)
			fixed++
			continue
		}
		if sel.OutlineIndex >= 0 && sel.OutlineIndex < len(needs) {
			need := needs[sel.OutlineIndex]
			counts := catalog.FieldCountsBySlide[sel.SourceSlide]
			incompatible := false
			if need.NeedsTitle && counts.Titles == 0 {
				incompatible = true
			}
			if need.ItemCount > 0 && counts.Titles+counts.Subtitles+counts.Contents == 0 {
				incompatible = true
			}
			if incompatible {
				replacement := findCompatibleTemplate(catalog, need)
				if replacement > 0 {
					slog.Warn("[sanitize] replacing incompatible template",
						"outlineIndex", sel.OutlineIndex,
						"original", sel.SourceSlide,
						"replacement", replacement,
					)
					sel.SourceSlide = replacement
					fixed++
				} else {
					slog.Warn("[sanitize] dropping selection with no compatible replacement",
						"sourceSlide", sel.SourceSlide,
						"outlineIndex", sel.OutlineIndex,
					)
					fixed++
					continue
				}
			}
		}
		valid = append(valid, sel)
	}
	selections.Selections = valid
	return fixed
}

func findCompatibleTemplate(catalog CatalogInfo, need SlideNeed) int {
	best := -1
	bestScore := -1
	for slideNum, counts := range catalog.FieldCountsBySlide {
		if need.NeedsTitle && counts.Titles == 0 {
			continue
		}
		textFields := counts.Titles + counts.Subtitles + counts.Contents
		if need.ItemCount > 0 && textFields == 0 {
			continue
		}
		if counts.NoFields {
			continue
		}
		score := textFields
		if score > bestScore {
			bestScore = score
			best = slideNum
		}
	}
	return best
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
