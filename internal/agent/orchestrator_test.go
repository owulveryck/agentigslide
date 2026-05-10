package agent

import (
	"sort"
	"testing"

	"github.com/owulveryck/agentigslide/internal/model"
)

func TestEnforceMaxChars(t *testing.T) {
	t.Run("text under limit is unchanged", func(t *testing.T) {
		content := &SlideContent{
			Modifications: []model.TextModification{
				{VariableName: "title", NewText: "abc"},
			},
		}
		fields := []TemplateField{{VariableName: "title", MaxChars: 10}}
		enforceMaxChars(content, fields)
		if content.Modifications[0].NewText != "abc" {
			t.Errorf("expected unchanged text, got %q", content.Modifications[0].NewText)
		}
	})

	t.Run("text at limit is unchanged", func(t *testing.T) {
		content := &SlideContent{
			Modifications: []model.TextModification{
				{VariableName: "title", NewText: "abcdefghij"},
			},
		}
		fields := []TemplateField{{VariableName: "title", MaxChars: 10}}
		enforceMaxChars(content, fields)
		if content.Modifications[0].NewText != "abcdefghij" {
			t.Errorf("expected unchanged text, got %q", content.Modifications[0].NewText)
		}
	})

	t.Run("truncate at sentence boundary", func(t *testing.T) {
		text := "Phrase un. Phrase deux. Phrase trois qui depasse la limite maximale."
		content := &SlideContent{
			Modifications: []model.TextModification{
				{VariableName: "body", NewText: text},
			},
		}
		fields := []TemplateField{{VariableName: "body", MaxChars: 30}}
		enforceMaxChars(content, fields)
		result := content.Modifications[0].NewText
		if len([]rune(result)) > 30 {
			t.Errorf("result exceeds limit: %d runes", len([]rune(result)))
		}
		if result != "Phrase un. Phrase deux." {
			t.Errorf("expected sentence break, got %q", result)
		}
	})

	t.Run("truncate at space when no sentence boundary", func(t *testing.T) {
		text := "mot mot mot mot mot mot mot mot"
		content := &SlideContent{
			Modifications: []model.TextModification{
				{VariableName: "body", NewText: text},
			},
		}
		fields := []TemplateField{{VariableName: "body", MaxChars: 20}}
		enforceMaxChars(content, fields)
		result := content.Modifications[0].NewText
		if len([]rune(result)) > 20 {
			t.Errorf("result exceeds limit: %d runes", len([]rune(result)))
		}
		lastSpace := len(result) - 1
		for lastSpace >= 0 && result[lastSpace] != ' ' {
			lastSpace--
		}
		if lastSpace < 0 {
			t.Errorf("expected space-based break, got %q", result)
		}
	})

	t.Run("truncate hard when no good break point", func(t *testing.T) {
		text := "abcdefghijklmnopqrstuvwxyz"
		content := &SlideContent{
			Modifications: []model.TextModification{
				{VariableName: "body", NewText: text},
			},
		}
		fields := []TemplateField{{VariableName: "body", MaxChars: 10}}
		enforceMaxChars(content, fields)
		result := content.Modifications[0].NewText
		if len([]rune(result)) > 10 {
			t.Errorf("result exceeds limit: %d runes, got %q", len([]rune(result)), result)
		}
	})

	t.Run("unclosed markdown bold is fixed", func(t *testing.T) {
		text := "Texte avec **gras non ferme et du contenu supplementaire ici"
		content := &SlideContent{
			Modifications: []model.TextModification{
				{VariableName: "body", NewText: text},
			},
		}
		fields := []TemplateField{{VariableName: "body", MaxChars: 40}}
		enforceMaxChars(content, fields)
		result := content.Modifications[0].NewText
		count := 0
		for i := 0; i < len(result)-1; i++ {
			if result[i] == '*' && result[i+1] == '*' {
				count++
				i++
			}
		}
		if count%2 != 0 {
			t.Errorf("unclosed bold markers in %q", result)
		}
	})

	t.Run("multibyte UTF-8 is handled correctly", func(t *testing.T) {
		text := "Présentation àvéc des accénts et des caractères spéciaux très longs"
		content := &SlideContent{
			Modifications: []model.TextModification{
				{VariableName: "body", NewText: text},
			},
		}
		fields := []TemplateField{{VariableName: "body", MaxChars: 20}}
		enforceMaxChars(content, fields)
		result := content.Modifications[0].NewText
		if len([]rune(result)) > 20 {
			t.Errorf("result exceeds limit in runes: %d", len([]rune(result)))
		}
	})

	t.Run("field not in constraints is unchanged", func(t *testing.T) {
		content := &SlideContent{
			Modifications: []model.TextModification{
				{VariableName: "unconstrained", NewText: "this text is very long and should not be truncated at all"},
			},
		}
		fields := []TemplateField{{VariableName: "other", MaxChars: 5}}
		enforceMaxChars(content, fields)
		if content.Modifications[0].NewText != "this text is very long and should not be truncated at all" {
			t.Errorf("unconstrained field was modified")
		}
	})

	t.Run("multiple modifications mixed", func(t *testing.T) {
		content := &SlideContent{
			Modifications: []model.TextModification{
				{VariableName: "title", NewText: "short"},
				{VariableName: "body", NewText: "this is a very long body text that should be truncated"},
				{VariableName: "footer", NewText: "ok"},
			},
		}
		fields := []TemplateField{
			{VariableName: "title", MaxChars: 100},
			{VariableName: "body", MaxChars: 20},
			{VariableName: "footer", MaxChars: 50},
		}
		enforceMaxChars(content, fields)
		if content.Modifications[0].NewText != "short" {
			t.Errorf("title should be unchanged, got %q", content.Modifications[0].NewText)
		}
		if len([]rune(content.Modifications[1].NewText)) > 20 {
			t.Errorf("body exceeds limit: %q", content.Modifications[1].NewText)
		}
		if content.Modifications[2].NewText != "ok" {
			t.Errorf("footer should be unchanged, got %q", content.Modifications[2].NewText)
		}
	})

	t.Run("empty modifications does not panic", func(t *testing.T) {
		content := &SlideContent{Modifications: nil}
		fields := []TemplateField{{VariableName: "body", MaxChars: 10}}
		enforceMaxChars(content, fields)
	})

	t.Run("field with zero maxChars is skipped", func(t *testing.T) {
		content := &SlideContent{
			Modifications: []model.TextModification{
				{VariableName: "body", NewText: "some long text here"},
			},
		}
		fields := []TemplateField{{VariableName: "body", MaxChars: 0}}
		enforceMaxChars(content, fields)
		if content.Modifications[0].NewText != "some long text here" {
			t.Errorf("field with MaxChars=0 should not be truncated")
		}
	})
}

func TestAssemble(t *testing.T) {
	t.Run("single slide", func(t *testing.T) {
		o := &Orchestrator{}
		state := &PipelineState{
			Outline: &PresentationOutline{PresentationTitle: "Test"},
			SlideContents: []SlideContent{
				{
					SourceSlide: 42,
					Modifications: []model.TextModification{
						{VariableName: "title", NewText: "Hello"},
					},
				},
			},
		}
		o.assemble(state)
		if state.AssembledPlan == nil {
			t.Fatal("AssembledPlan is nil")
		}
		if state.AssembledPlan.PresentationTitle != "Test" {
			t.Errorf("title = %q, want %q", state.AssembledPlan.PresentationTitle, "Test")
		}
		if len(state.AssembledPlan.Slides) != 1 {
			t.Fatalf("expected 1 slide, got %d", len(state.AssembledPlan.Slides))
		}
		if state.AssembledPlan.Slides[0].SourceSlide != 42 {
			t.Errorf("SourceSlide = %d, want 42", state.AssembledPlan.Slides[0].SourceSlide)
		}
		if len(state.AssembledPlan.Slides[0].Modifications) != 1 {
			t.Errorf("expected 1 modification, got %d", len(state.AssembledPlan.Slides[0].Modifications))
		}
	})

	t.Run("multiple slides preserve order", func(t *testing.T) {
		o := &Orchestrator{}
		state := &PipelineState{
			Outline: &PresentationOutline{PresentationTitle: "Multi"},
			SlideContents: []SlideContent{
				{SourceSlide: 1},
				{SourceSlide: 5},
				{SourceSlide: 3},
			},
		}
		o.assemble(state)
		if len(state.AssembledPlan.Slides) != 3 {
			t.Fatalf("expected 3 slides, got %d", len(state.AssembledPlan.Slides))
		}
		want := []int{1, 5, 3}
		for i, s := range state.AssembledPlan.Slides {
			if s.SourceSlide != want[i] {
				t.Errorf("slide %d: SourceSlide = %d, want %d", i, s.SourceSlide, want[i])
			}
		}
	})

	t.Run("empty slides", func(t *testing.T) {
		o := &Orchestrator{}
		state := &PipelineState{
			Outline:       &PresentationOutline{PresentationTitle: "Empty"},
			SlideContents: nil,
		}
		o.assemble(state)
		if state.AssembledPlan == nil {
			t.Fatal("AssembledPlan is nil")
		}
		if len(state.AssembledPlan.Slides) != 0 {
			t.Errorf("expected 0 slides, got %d", len(state.AssembledPlan.Slides))
		}
		if state.AssembledPlan.PresentationTitle != "Empty" {
			t.Errorf("title = %q, want %q", state.AssembledPlan.PresentationTitle, "Empty")
		}
	})

	t.Run("modifications are preserved", func(t *testing.T) {
		o := &Orchestrator{}
		mods := []model.TextModification{
			{VariableName: "a", NewText: "val_a"},
			{VariableName: "b", NewText: "val_b"},
		}
		state := &PipelineState{
			Outline:       &PresentationOutline{PresentationTitle: "T"},
			SlideContents: []SlideContent{{SourceSlide: 1, Modifications: mods}},
		}
		o.assemble(state)
		got := state.AssembledPlan.Slides[0].Modifications
		if len(got) != 2 {
			t.Fatalf("expected 2 modifications, got %d", len(got))
		}
		if got[0].VariableName != "a" || got[0].NewText != "val_a" {
			t.Errorf("mod 0 = %+v", got[0])
		}
		if got[1].VariableName != "b" || got[1].NewText != "val_b" {
			t.Errorf("mod 1 = %+v", got[1])
		}
	})
}

func TestGroupIssuesBySlide(t *testing.T) {
	group := func(issues []ReviewIssue, selectionCount int) (map[int][]ReviewIssue, []int) {
		feedbackByIndex := make(map[int][]ReviewIssue)
		for _, issue := range issues {
			if issue.SlideIndex >= 0 && issue.SlideIndex < selectionCount {
				feedbackByIndex[issue.SlideIndex] = append(feedbackByIndex[issue.SlideIndex], issue)
			}
		}
		indices := make([]int, 0, len(feedbackByIndex))
		for idx := range feedbackByIndex {
			indices = append(indices, idx)
		}
		sort.Ints(indices)
		return feedbackByIndex, indices
	}

	t.Run("no issues", func(t *testing.T) {
		m, idx := group(nil, 5)
		if len(m) != 0 {
			t.Errorf("expected empty map, got %d entries", len(m))
		}
		if len(idx) != 0 {
			t.Errorf("expected no indices, got %v", idx)
		}
	})

	t.Run("multiple issues on same slide", func(t *testing.T) {
		issues := []ReviewIssue{
			{SlideIndex: 2, IssueType: "overflow"},
			{SlideIndex: 2, IssueType: "duplicate"},
			{SlideIndex: 2, IssueType: "missing_content"},
		}
		m, idx := group(issues, 5)
		if len(m) != 1 {
			t.Errorf("expected 1 entry, got %d", len(m))
		}
		if len(m[2]) != 3 {
			t.Errorf("expected 3 issues for slide 2, got %d", len(m[2]))
		}
		if len(idx) != 1 || idx[0] != 2 {
			t.Errorf("expected indices [2], got %v", idx)
		}
	})

	t.Run("out of bounds index ignored", func(t *testing.T) {
		issues := []ReviewIssue{
			{SlideIndex: 10, IssueType: "overflow"},
			{SlideIndex: 1, IssueType: "duplicate"},
		}
		m, idx := group(issues, 3)
		if len(m) != 1 {
			t.Errorf("expected 1 entry (only valid index), got %d", len(m))
		}
		if _, ok := m[10]; ok {
			t.Error("out-of-bounds index 10 should be filtered out")
		}
		if len(idx) != 1 || idx[0] != 1 {
			t.Errorf("expected indices [1], got %v", idx)
		}
	})

	t.Run("negative index ignored", func(t *testing.T) {
		issues := []ReviewIssue{
			{SlideIndex: -1, IssueType: "overflow"},
			{SlideIndex: 0, IssueType: "duplicate"},
		}
		m, idx := group(issues, 3)
		if len(m) != 1 {
			t.Errorf("expected 1 entry, got %d", len(m))
		}
		if _, ok := m[-1]; ok {
			t.Error("negative index should be filtered out")
		}
		if len(idx) != 1 || idx[0] != 0 {
			t.Errorf("expected indices [0], got %v", idx)
		}
	})
}
