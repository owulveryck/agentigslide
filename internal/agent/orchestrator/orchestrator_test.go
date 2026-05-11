package orchestrator

import (
	"sort"
	"testing"

	"github.com/owulveryck/agentigslide/internal/agent"
	"github.com/owulveryck/agentigslide/internal/model"
)

func TestAssemble(t *testing.T) {
	t.Run("single slide", func(t *testing.T) {
		o := &Orchestrator{}
		state := &agent.PipelineState{
			Outline: &agent.PresentationOutline{PresentationTitle: "Test"},
			SlideContents: []agent.SlideContent{
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
		state := &agent.PipelineState{
			Outline: &agent.PresentationOutline{PresentationTitle: "Multi"},
			SlideContents: []agent.SlideContent{
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
		state := &agent.PipelineState{
			Outline:       &agent.PresentationOutline{PresentationTitle: "Empty"},
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
		state := &agent.PipelineState{
			Outline:       &agent.PresentationOutline{PresentationTitle: "T"},
			SlideContents: []agent.SlideContent{{SourceSlide: 1, Modifications: mods}},
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
	group := func(issues []agent.ReviewIssue, selectionCount int) (map[int][]agent.ReviewIssue, []int) {
		feedbackByIndex := make(map[int][]agent.ReviewIssue)
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
		issues := []agent.ReviewIssue{
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
		issues := []agent.ReviewIssue{
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
		issues := []agent.ReviewIssue{
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
