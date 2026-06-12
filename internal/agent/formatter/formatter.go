package formatter

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/owulveryck/agentigslide/internal/revision"
	"google.golang.org/api/slides/v1"
)

// Agent is the Formatter agent. It is deterministic (no LLM calls).
type Agent struct {
	slidesSrv *slides.Service
}

// New creates a Formatter agent.
func New(slidesSrv *slides.Service) *Agent {
	return &Agent{slidesSrv: slidesSrv}
}

// Run executes the full formatter pipeline on all slides of the presentation.
func (a *Agent) Run(ctx context.Context, presentationID string, revLog *revision.Log) (*FormatterResult, error) {
	start := time.Now()
	slog.Info("[agent:formatter] starting", "presentationID", presentationID)

	pres, err := a.slidesSrv.Presentations.Get(presentationID).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("formatter: failed to get presentation: %w", err)
	}

	structure := ExtractStructure(pres)
	slog.Info("[agent:formatter] extracted structure", "slides", len(structure))

	issues := CheckConsistency(structure)
	slog.Info("[agent:formatter] consistency check complete", "issues", len(issues))

	for _, issue := range issues {
		slog.Info("[agent:formatter] issue", "rule", issue.Rule, "slide", issue.SlideIndex, "objectID", issue.ObjectID, "severity", issue.Severity, "expected", issue.Expected, "actual", issue.Actual)
	}

	result := &FormatterResult{Issues: issues}

	if len(issues) == 0 {
		slog.Info("[agent:formatter] no issues found", "duration", time.Since(start).Round(time.Millisecond))
		return result, nil
	}

	corrections := GenerateCorrections(issues, structure)
	if len(corrections) == 0 {
		slog.Info("[agent:formatter] no corrections generated", "duration", time.Since(start).Round(time.Millisecond))
		return result, nil
	}

	validCorrections := ValidateCorrections(&CorrectionPlan{Corrections: corrections}, structure)
	if len(validCorrections) == 0 {
		slog.Warn("[agent:formatter] all corrections were invalid after validation")
		return result, nil
	}

	requests := BuildCorrections(validCorrections)
	slog.Info("[agent:formatter] applying corrections", "count", len(requests))

	if err := ApplyCorrections(ctx, &slidesBatchAdapter{a.slidesSrv}, presentationID, requests, revLog); err != nil {
		return result, fmt.Errorf("formatter: failed to apply corrections: %w", err)
	}

	result.Corrections = validCorrections
	result.AppliedCount = len(requests)

	slog.Info("[agent:formatter] done", "applied", result.AppliedCount, "duration", time.Since(start).Round(time.Millisecond))
	return result, nil
}

// RunForPages executes the formatter pipeline scoped to specific page IDs.
// The full presentation is read for majority computation, but only issues
// on targeted pages produce corrections.
func (a *Agent) RunForPages(ctx context.Context, presentationID string, pageIDs []string, revLog *revision.Log) (*FormatterResult, error) {
	if len(pageIDs) == 0 {
		return &FormatterResult{}, nil
	}

	start := time.Now()
	slog.Info("[agent:formatter] starting (scoped)", "presentationID", presentationID, "pages", len(pageIDs))

	pres, err := a.slidesSrv.Presentations.Get(presentationID).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("formatter: failed to get presentation: %w", err)
	}

	// Full structure for majority voting
	fullStructure := ExtractStructure(pres)

	// Run consistency on full structure to get correct majority values
	allIssues := CheckConsistency(fullStructure)

	// Filter to only issues on targeted pages
	pageIDSet := make(map[string]bool, len(pageIDs))
	for _, id := range pageIDs {
		pageIDSet[id] = true
	}

	targetPageIndices := make(map[int]bool)
	for _, si := range fullStructure {
		if pageIDSet[si.PageID] {
			targetPageIndices[si.SlideIndex] = true
		}
	}

	var scopedIssues []ConsistencyIssue
	for _, issue := range allIssues {
		// Presentation-level issues (SlideIndex=-1) always apply
		if issue.SlideIndex < 0 || targetPageIndices[issue.SlideIndex] {
			scopedIssues = append(scopedIssues, issue)
		}
	}

	slog.Info("[agent:formatter] consistency check complete (scoped)", "totalIssues", len(allIssues), "scopedIssues", len(scopedIssues))

	for _, issue := range scopedIssues {
		slog.Info("[agent:formatter] issue", "rule", issue.Rule, "slide", issue.SlideIndex, "objectID", issue.ObjectID, "severity", issue.Severity)
	}

	result := &FormatterResult{Issues: scopedIssues}

	if len(scopedIssues) == 0 {
		slog.Info("[agent:formatter] no scoped issues found", "duration", time.Since(start).Round(time.Millisecond))
		return result, nil
	}

	corrections := GenerateCorrections(scopedIssues, fullStructure)
	if len(corrections) == 0 {
		return result, nil
	}

	validCorrections := ValidateCorrections(&CorrectionPlan{Corrections: corrections}, fullStructure)
	if len(validCorrections) == 0 {
		slog.Warn("[agent:formatter] all corrections were invalid after validation (scoped)")
		return result, nil
	}

	requests := BuildCorrections(validCorrections)
	slog.Info("[agent:formatter] applying corrections (scoped)", "count", len(requests))

	if err := ApplyCorrections(ctx, &slidesBatchAdapter{a.slidesSrv}, presentationID, requests, revLog); err != nil {
		return result, fmt.Errorf("formatter: failed to apply corrections: %w", err)
	}

	result.Corrections = validCorrections
	result.AppliedCount = len(requests)

	slog.Info("[agent:formatter] done (scoped)", "applied", result.AppliedCount, "duration", time.Since(start).Round(time.Millisecond))
	return result, nil
}

// GenerateCorrections translates consistency issues into concrete corrections
// using the majority values from the structure.
func GenerateCorrections(issues []ConsistencyIssue, structure []SlideInfo) []Correction {
	var corrections []Correction

	for _, issue := range issues {
		c, ok := generateOneCorrection(issue)
		if ok {
			corrections = append(corrections, c)
		}
	}

	return corrections
}

// generateOneCorrection maps a single ConsistencyIssue to a Correction.
// It returns the correction and true if successful, or a zero Correction and
// false if the issue should be skipped or cannot be parsed.
func generateOneCorrection(issue ConsistencyIssue) (Correction, bool) {
	base := Correction{
		ObjectID:   issue.ObjectID,
		SlideIndex: issue.SlideIndex,
		Reason:     fmt.Sprintf("%s: expected %s, got %s", issue.Rule, issue.Expected, issue.Actual),
	}

	switch issue.Rule {
	case "FontFamilyByRole":
		base.Type = "textStyle"
		base.FontFamily = strPtr(issue.Expected)
		return base, true

	case "FontSizeByRole":
		pt, err := parsePtValue(issue.Expected)
		if err != nil {
			slog.Warn("[agent:formatter] skipping correction: cannot parse font size", "expected", issue.Expected, "err", err)
			return Correction{}, false
		}
		base.Type = "textStyle"
		base.FontSizePt = &pt
		return base, true

	case "AlignmentByRole":
		base.Type = "paragraphStyle"
		base.Alignment = strPtr(issue.Expected)
		return base, true

	case "EmphasisCoherence":
		base.Type = "textStyle"
		// Expected is "bold=true"/"bold=false" or "italic=true"/"italic=false"
		key, val, ok := parseKeyValueBool(issue.Expected)
		if !ok {
			slog.Warn("[agent:formatter] skipping correction: cannot parse emphasis", "expected", issue.Expected)
			return Correction{}, false
		}
		switch key {
		case "bold":
			base.Bold = &val
		case "italic":
			base.Italic = &val
		default:
			slog.Warn("[agent:formatter] skipping correction: unknown emphasis key", "key", key)
			return Correction{}, false
		}
		return base, true

	case "ParagraphSpacing":
		// Expected is "LineSpacing=100.0pt", "SpaceAbovePt=5.0pt", or "SpaceBelowPt=0.0pt"
		base.Type = "paragraphStyle"
		name, pt, err := parseNamedPtValue(issue.Expected)
		if err != nil {
			slog.Warn("[agent:formatter] skipping correction: cannot parse paragraph spacing", "expected", issue.Expected, "err", err)
			return Correction{}, false
		}
		switch name {
		case "LineSpacing":
			if pt <= 0 {
				slog.Warn("[agent:formatter] skipping correction: lineSpacing=0 is invalid (means unset)", "objectID", issue.ObjectID)
				return Correction{}, false
			}
			base.LineSpacing = &pt
		case "SpaceAbovePt":
			base.SpaceAbovePt = &pt
		case "SpaceBelowPt":
			base.SpaceBelowPt = &pt
		default:
			slog.Warn("[agent:formatter] skipping correction: unknown spacing property", "name", name)
			return Correction{}, false
		}
		return base, true

	case "BackgroundConsistency":
		base.Type = "shapeProperties"
		rgb, err := parseRGB(issue.Expected)
		if err != nil {
			slog.Warn("[agent:formatter] skipping correction: cannot parse background color", "expected", issue.Expected, "err", err)
			return Correction{}, false
		}
		base.BackgroundColor = &rgb
		return base, true

	case "OutlineConsistency":
		base.Type = "shapeProperties"
		// Could be a color "rgb(R,G,B)" or a weight "N.Npt"
		if strings.HasPrefix(issue.Expected, "rgb(") {
			rgb, err := parseRGB(issue.Expected)
			if err != nil {
				slog.Warn("[agent:formatter] skipping correction: cannot parse outline color", "expected", issue.Expected, "err", err)
				return Correction{}, false
			}
			base.OutlineColor = &rgb
		} else {
			pt, err := parsePtValue(issue.Expected)
			if err != nil {
				slog.Warn("[agent:formatter] skipping correction: cannot parse outline weight", "expected", issue.Expected, "err", err)
				return Correction{}, false
			}
			base.OutlineWeightPt = &pt
		}
		return base, true

	case "SizeHierarchy":
		// Presentation-level issue, no single object to fix.
		return Correction{}, false

	case "ColorPalette":
		// Orphan colors are warnings with no clear fix.
		return Correction{}, false

	default:
		slog.Warn("[agent:formatter] skipping correction: unknown rule", "rule", issue.Rule)
		return Correction{}, false
	}
}

// parsePtValue parses a string like "14.0pt" and returns 14.0.
func parsePtValue(s string) (float64, error) {
	s = strings.TrimSuffix(s, "pt")
	return strconv.ParseFloat(s, 64)
}

// parseNamedPtValue parses a string like "LineSpacing=100.0pt" into ("LineSpacing", 100.0).
func parseNamedPtValue(s string) (string, float64, error) {
	parts := strings.SplitN(s, "=", 2)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("expected Name=Value format, got %q", s)
	}
	name := parts[0]
	pt, err := parsePtValue(parts[1])
	if err != nil {
		return "", 0, fmt.Errorf("cannot parse value in %q: %w", s, err)
	}
	return name, pt, nil
}

// parseKeyValueBool parses a string like "bold=true" into ("bold", true).
func parseKeyValueBool(s string) (string, bool, bool) {
	parts := strings.SplitN(s, "=", 2)
	if len(parts) != 2 {
		return "", false, false
	}
	val, err := strconv.ParseBool(parts[1])
	if err != nil {
		return "", false, false
	}
	return parts[0], val, true
}

// parseRGB parses a string like "rgb(0.50,0.20,0.80)" into an RGBColor.
func parseRGB(s string) (RGBColor, error) {
	var r, g, b float64
	n, err := fmt.Sscanf(s, "rgb(%f,%f,%f)", &r, &g, &b)
	if err != nil || n != 3 {
		return RGBColor{}, fmt.Errorf("cannot parse rgb color %q: %w", s, err)
	}
	return RGBColor{Red: r, Green: g, Blue: b}, nil
}

// strPtr returns a pointer to the given string.
func strPtr(s string) *string {
	return &s
}
