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

	"github.com/owulveryck/agentigslide/internal/agent"
	"github.com/owulveryck/agentigslide/internal/model"
	"github.com/owulveryck/agentigslide/internal/templateindex"
)

// LoadTemplateIndex reads and parses a template_index.json file at the given
// path, returning the deserialized TemplateIndex. The index is normalized so
// that every field capacity derives from its line geometry (ADR 027): all
// pipeline agents then see the same budget for the same field.
func LoadTemplateIndex(path string) (*model.TemplateIndex, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	var index model.TemplateIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	drifts := NormalizeIndexGeometry(&index)
	if len(drifts) > 0 {
		slog.Warn("template index capacities drifted from geometry — normalized at load; rebuild the index (cmd/buildindex) to persist",
			"path", path,
			"driftedFields", len(drifts),
		)
	}
	MergeLearnedCaveats(&index, filepath.Dir(path))
	return &index, nil
}

// GeometryDrift records a field whose stored MaxChars disagreed with the
// capacity derived from its line geometry. A non-empty drift list is the
// signature of a stale index (built before the geometry-based estimation).
type GeometryDrift struct {
	SlideNumber  int
	VariableName string
	Stored       int
	Derived      int
}

// NormalizeIndexGeometry rewrites every field capacity from its line
// geometry (single source of truth, ADR 027). Fields without geometry are
// left untouched — they disappear at the next index rebuild. Returns the
// fields whose stored capacity disagreed with the derived one.
func NormalizeIndexGeometry(index *model.TemplateIndex) []GeometryDrift {
	if index == nil {
		return nil
	}
	var drifts []GeometryDrift
	noGeometry := 0
	for si := range index.Slides {
		slide := &index.Slides[si]
		for fi := range slide.EditableFields {
			f := &slide.EditableFields[fi]
			if f.CharsPerLine <= 0 || f.Lines <= 0 {
				if f.MaxChars > 0 {
					noGeometry++
				}
				continue
			}
			derived := templateindex.DerivedMaxChars(f.CharsPerLine, f.Lines)
			if f.MaxChars != derived {
				drifts = append(drifts, GeometryDrift{
					SlideNumber:  slide.SlideNumber,
					VariableName: f.VariableName,
					Stored:       f.MaxChars,
					Derived:      derived,
				})
				f.MaxChars = derived
			}
		}
	}
	if noGeometry > 0 {
		slog.Warn("template index has fields without line geometry — their capacities cannot be verified; rebuild the index (cmd/buildindex)",
			"fieldsWithoutGeometry", noGeometry,
		)
	}
	return drifts
}

// learnedCaveatEntry is one slide's learned visual constraints, persisted in
// learned_caveats.json next to the template index (ADR 031). This overlay is
// the structured, verifiable form of template knowledge learned from visual
// review findings — unlike free-text memory rules, it is keyed on a real
// slide number and merged into the catalog seen by selector and reviewer.
type learnedCaveatEntry struct {
	SlideNumber int      `json:"slideNumber"`
	Caveats     []string `json:"caveats"`
}

const learnedCaveatsFile = "learned_caveats.json"

// maxLearnedCaveatsPerSlide bounds the overlay growth: beyond this, new
// caveats for a slide are dropped (the signal is already strong enough).
const maxLearnedCaveatsPerSlide = 5

// MergeLearnedCaveats merges the learned_caveats.json overlay (if present in
// dir) into the VisualCaveats of the corresponding slides. Duplicates are
// skipped. The overlay file is never touched by cmd/buildindex, so learned
// knowledge survives index rebuilds.
func MergeLearnedCaveats(index *model.TemplateIndex, dir string) {
	data, err := os.ReadFile(filepath.Join(dir, learnedCaveatsFile))
	if err != nil {
		return
	}
	var entries []learnedCaveatEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		slog.Warn("invalid learned_caveats.json, ignoring", "error", err)
		return
	}
	bySlide := make(map[int][]string, len(entries))
	for _, e := range entries {
		bySlide[e.SlideNumber] = e.Caveats
	}
	merged := 0
	for i := range index.Slides {
		slide := &index.Slides[i]
		existing := make(map[string]bool, len(slide.VisualCaveats))
		for _, c := range slide.VisualCaveats {
			existing[c] = true
		}
		for _, c := range bySlide[slide.SlideNumber] {
			if !existing[c] {
				slide.VisualCaveats = append(slide.VisualCaveats, c)
				existing[c] = true
				merged++
			}
		}
	}
	if merged > 0 {
		slog.Info("merged learned caveats into template index", "caveats", merged)
	}
}

// AppendLearnedCaveat records a template-geometry constraint observed by the
// visual review into learned_caveats.json (deduplicated, bounded per slide).
// The file is git-versioned: every learned constraint is auditable.
func AppendLearnedCaveat(templateDir string, slideNumber int, caveat string) error {
	if slideNumber <= 0 || strings.TrimSpace(caveat) == "" {
		return nil
	}
	p := filepath.Join(templateDir, learnedCaveatsFile)
	var entries []learnedCaveatEntry
	if data, err := os.ReadFile(p); err == nil {
		if err := json.Unmarshal(data, &entries); err != nil {
			return fmt.Errorf("invalid %s: %w", p, err)
		}
	}
	for i := range entries {
		if entries[i].SlideNumber != slideNumber {
			continue
		}
		for _, c := range entries[i].Caveats {
			if c == caveat {
				return nil
			}
		}
		if len(entries[i].Caveats) >= maxLearnedCaveatsPerSlide {
			slog.Debug("learned caveats cap reached for slide, dropping", "slideNumber", slideNumber)
			return nil
		}
		entries[i].Caveats = append(entries[i].Caveats, caveat)
		return writeLearnedCaveats(p, entries)
	}
	entries = append(entries, learnedCaveatEntry{SlideNumber: slideNumber, Caveats: []string{caveat}})
	return writeLearnedCaveats(p, entries)
}

func writeLearnedCaveats(path string, entries []learnedCaveatEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}
	slog.Info("learned caveat persisted", "path", path)
	return nil
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
// as opposed to metadata fields like year or copyright. Thin wrapper around
// [model.IsContentField], kept for the existing call sites.
func IsContentField(role string) bool {
	return model.IsContentField(role)
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
			if f.Lines > 1 && f.CharsPerLine > 0 {
				part += fmt.Sprintf(" %dLx%dC", f.Lines, f.CharsPerLine)
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
	return loadSlideNumberFile(templateDir, "CLOSING_SLIDE")
}

// LoadDeckInvariants reads the deck-level structural configuration from the
// template directory (ADR 029): COVER_SLIDE, CLOSING_SLIDE and SUMMARY_SLIDE,
// each an optional file containing a single template slide number. Missing or
// invalid files yield -1 (not configured).
func LoadDeckInvariants(templateDir string) agent.DeckInvariants {
	return agent.DeckInvariants{
		CoverSlide:   loadSlideNumberFile(templateDir, "COVER_SLIDE"),
		ClosingSlide: loadSlideNumberFile(templateDir, "CLOSING_SLIDE"),
		SummarySlide: loadSlideNumberFile(templateDir, "SUMMARY_SLIDE"),
	}
}

// loadSlideNumberFile reads a single slide number from an optional template
// configuration file. Returns -1 if the file does not exist or is invalid.
func loadSlideNumberFile(templateDir, name string) int {
	data, err := os.ReadFile(filepath.Join(templateDir, name))
	if err != nil {
		return -1
	}
	s := strings.TrimSpace(string(data))
	n, err := strconv.Atoi(s)
	if err != nil {
		slog.Warn("invalid slide number in template config file", "file", name, "value", s, "error", err)
		return -1
	}
	slog.Info("loaded deck invariant from template config", "file", name, "slideNumber", n)
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
