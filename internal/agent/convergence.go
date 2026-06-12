package agent

import (
	"fmt"
	"strings"
)

// Fingerprint identifies an issue across review passes: same slide (or
// page), same field, same type. Two findings with the same fingerprint are
// the same problem observed twice.
type Fingerprint string

// IssueFingerprint builds the fingerprint of an editorial review issue.
func IssueFingerprint(issue ReviewIssue) Fingerprint {
	return Fingerprint(fmt.Sprintf("%d|%s|%s", issue.SlideIndex, issue.Field, issue.IssueType))
}

// VisualFingerprint builds the fingerprint of a visual finding.
func VisualFingerprint(pageID, issueType string) Fingerprint {
	return Fingerprint(pageID + "|" + issueType)
}

// ConvergenceTracker implements the convergence contract of review loops
// (ADR 031): between two passes, every issue is either resolved (present
// before, absent now), repeated (present in both), or new. A pass makes
// strict progress when it resolves at least one issue and does not create
// more than it resolves. Loops must stop — or escalate — as soon as strict
// progress stops; iterating "to see" only burns tokens and degrades content.
type ConvergenceTracker struct {
	prev map[Fingerprint]bool
	cur  map[Fingerprint]bool
	// pass statistics, updated by EndPass
	resolved int
	repeated int
	fresh    int
	passes   int
}

// NewConvergenceTracker returns an empty tracker.
func NewConvergenceTracker() *ConvergenceTracker {
	return &ConvergenceTracker{cur: make(map[Fingerprint]bool)}
}

// Observe records one issue fingerprint for the current pass. Returns true
// when the same issue was already present in the previous pass (repeated).
func (t *ConvergenceTracker) Observe(fp Fingerprint) (repeated bool) {
	t.cur[fp] = true
	return t.prev[fp]
}

// EndPass closes the current pass, computes the resolved/repeated/new
// statistics against the previous one, and starts a new observation window.
func (t *ConvergenceTracker) EndPass() {
	t.resolved, t.repeated, t.fresh = 0, 0, 0
	for fp := range t.prev {
		if !t.cur[fp] {
			t.resolved++
		}
	}
	for fp := range t.cur {
		if t.prev[fp] {
			t.repeated++
		} else {
			t.fresh++
		}
	}
	t.prev = t.cur
	t.cur = make(map[Fingerprint]bool)
	t.passes++
}

// StrictProgress reports whether the last closed pass made strict progress:
// at least one issue resolved AND no more new issues than resolved ones.
// Before two passes exist there is nothing to compare — it returns true so
// the loop always gets its first correction attempt.
func (t *ConvergenceTracker) StrictProgress() bool {
	if t.passes < 2 {
		return true
	}
	if len(t.prev) == 0 {
		// Last pass is clean: converged.
		return true
	}
	return t.resolved > 0 && t.fresh <= t.resolved
}

// PassStats returns the statistics of the last closed pass.
func (t *ConvergenceTracker) PassStats() (resolved, repeated, fresh int) {
	return t.resolved, t.repeated, t.fresh
}

// VisualIssueClass categorizes a visual finding by what can act on it
// (ADR 031): only content-fixable issues are worth sending back to a writer;
// template-geometry issues feed the template index annotations; subjective
// issues are acknowledged, never retried.
type VisualIssueClass int

const (
	// VisualFixable: the finding can be resolved by rewriting the slide's
	// text content (overflow, truncation, empty field).
	VisualFixable VisualIssueClass = iota
	// VisualTemplateGeometry: the finding is caused by the template's box
	// geometry (too narrow, fragmenting text) — no rewrite fixes it; the
	// knowledge belongs in the template index caveats.
	VisualTemplateGeometry
	// VisualSubjective: layout taste calls (alignment, font preference)
	// that a content correction loop cannot reliably satisfy.
	VisualSubjective
)

// geometryMarkers are description fragments that indicate the box geometry —
// not the content — is the cause. Conservative: only clear geometry language
// reclassifies a finding away from the correction loop.
var geometryMarkers = []string{
	"trop étroit", "trop etroit", "trop étroite", "trop petite zone",
	"fragmenté", "fragmente", "fragmentation",
	"coupure", "coupé en deux", "retours à la ligne artificiels",
	"zone de texte trop", "élargir la zone", "elargir la zone",
	"agrandir la zone",
}

// ClassifyVisualIssue maps a visual finding to its actionable class from its
// type and description. Defaults to VisualFixable for text-content types so
// genuine content problems always get their correction chance.
func ClassifyVisualIssue(issueType, description string) VisualIssueClass {
	switch issueType {
	case "misalignment", "font_issue":
		return VisualSubjective
	case "layout_broken":
		if containsAnyFold(description, geometryMarkers) {
			return VisualTemplateGeometry
		}
		return VisualSubjective
	case "text_overflow", "text_truncated", "empty_field":
		if containsAnyFold(description, geometryMarkers) {
			return VisualTemplateGeometry
		}
		return VisualFixable
	default:
		return VisualFixable
	}
}

func containsAnyFold(s string, markers []string) bool {
	lower := strings.ToLower(s)
	for _, m := range markers {
		if m != "" && strings.Contains(lower, strings.ToLower(m)) {
			return true
		}
	}
	return false
}
