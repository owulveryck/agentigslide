package agent

import "testing"

func TestConvergenceTracker_StrictProgress(t *testing.T) {
	t.Run("first pass always allows a correction attempt", func(t *testing.T) {
		tr := NewConvergenceTracker()
		tr.Observe("p1|text_overflow")
		tr.Observe("p2|empty_field")
		tr.EndPass()
		if !tr.StrictProgress() {
			t.Fatal("first pass must allow the loop to attempt a correction")
		}
	})

	t.Run("resolution with fewer new issues is progress", func(t *testing.T) {
		tr := NewConvergenceTracker()
		tr.Observe("p1|text_overflow")
		tr.Observe("p2|empty_field")
		tr.EndPass()
		// Pass 2: p1 resolved, p2 remains.
		tr.Observe("p2|empty_field")
		tr.EndPass()
		if !tr.StrictProgress() {
			t.Fatal("one resolved, zero new must be strict progress")
		}
		resolved, repeated, fresh := tr.PassStats()
		if resolved != 1 || repeated != 1 || fresh != 0 {
			t.Errorf("stats = (%d,%d,%d), want (1,1,0)", resolved, repeated, fresh)
		}
	})

	t.Run("same issues repeating is not progress", func(t *testing.T) {
		// Reproduces the v3 visual loop: 13→11→10 unapproved slides with the
		// same defects re-observed pass after pass.
		tr := NewConvergenceTracker()
		tr.Observe("p1|text_truncated")
		tr.Observe("p2|layout_broken")
		tr.EndPass()
		tr.Observe("p1|text_truncated")
		tr.Observe("p2|layout_broken")
		tr.EndPass()
		if tr.StrictProgress() {
			t.Fatal("all issues repeated, zero resolved: must NOT be strict progress")
		}
	})

	t.Run("churn (as many new as resolved plus one) is not progress", func(t *testing.T) {
		tr := NewConvergenceTracker()
		tr.Observe("p1|a")
		tr.EndPass()
		// p1 resolved but two new appeared: net regression.
		tr.Observe("p2|b")
		tr.Observe("p3|c")
		tr.EndPass()
		if tr.StrictProgress() {
			t.Fatal("1 resolved but 2 new: must NOT be strict progress")
		}
	})

	t.Run("clean pass converges", func(t *testing.T) {
		tr := NewConvergenceTracker()
		tr.Observe("p1|a")
		tr.EndPass()
		tr.EndPass() // nothing observed: all resolved
		if !tr.StrictProgress() {
			t.Fatal("empty pass after issues must be progress (converged)")
		}
	})
}

func TestClassifyVisualIssue(t *testing.T) {
	tests := []struct {
		issueType   string
		description string
		want        VisualIssueClass
	}{
		// From the v3 trace: text fragmented by a too-narrow zone is a
		// template problem, not a content problem.
		{"text_truncated", "Ces coupures semblent dues à une zone de texte trop étroite forçant des retours à la ligne artificiels.", VisualTemplateGeometry},
		{"layout_broken", "Élargir la zone de texte du bloc Capability", VisualTemplateGeometry},
		{"text_overflow", "Le texte déborde de la zone car il est trop long.", VisualFixable},
		{"empty_field", "La zone de texte principale est vide.", VisualFixable},
		{"misalignment", "Le bloc droit dépasse légèrement au-dessus du bloc gauche, asymétrie visuelle.", VisualSubjective},
		{"font_issue", "Police trop petite.", VisualSubjective},
		{"layout_broken", "Le cadre chevauche la liste en dessous.", VisualSubjective},
	}
	for _, tt := range tests {
		if got := ClassifyVisualIssue(tt.issueType, tt.description); got != tt.want {
			t.Errorf("ClassifyVisualIssue(%s, %q) = %v, want %v", tt.issueType, tt.description[:30], got, tt.want)
		}
	}
}
