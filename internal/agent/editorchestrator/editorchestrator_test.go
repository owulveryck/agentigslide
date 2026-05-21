package editorchestrator

import (
	"sort"
	"testing"

	"github.com/owulveryck/agentigslide/internal/agent"
	"github.com/owulveryck/agentigslide/internal/model"
)

func TestAssemble(t *testing.T) {
	t.Run("single operation", func(t *testing.T) {
		o := &EditOrchestrator{}
		state := &editPipelineState{
			presentationID: "pres-123",
			filledOperations: []model.EditOperation{
				{
					Type:       "modify_content",
					SlideIndex: 2,
					Modifications: []model.TextModification{
						{VariableName: "obj1", NewText: "Hello"},
					},
					Rationale: "test",
				},
			},
		}
		o.assemble(state)
		if state.editPlan == nil {
			t.Fatal("editPlan is nil")
		}
		if state.editPlan.PresentationID != "pres-123" {
			t.Errorf("presentationID = %q, want %q", state.editPlan.PresentationID, "pres-123")
		}
		if len(state.editPlan.Operations) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(state.editPlan.Operations))
		}
		if state.editPlan.Operations[0].Type != "modify_content" {
			t.Errorf("type = %q, want modify_content", state.editPlan.Operations[0].Type)
		}
	})

	t.Run("multiple operations preserve order", func(t *testing.T) {
		o := &EditOrchestrator{}
		state := &editPipelineState{
			presentationID: "pres-456",
			filledOperations: []model.EditOperation{
				{Type: "modify_content", SlideIndex: 0},
				{Type: "delete_slide", SlideIndex: 3},
				{Type: "replace_slide", SlideIndex: 1},
			},
		}
		o.assemble(state)
		if len(state.editPlan.Operations) != 3 {
			t.Fatalf("expected 3 operations, got %d", len(state.editPlan.Operations))
		}
		wantTypes := []string{"modify_content", "delete_slide", "replace_slide"}
		for i, op := range state.editPlan.Operations {
			if op.Type != wantTypes[i] {
				t.Errorf("operation %d: type = %q, want %q", i, op.Type, wantTypes[i])
			}
		}
	})
}

func TestEnrichSkeleton(t *testing.T) {
	t.Run("populates CurrentText from existing slides", func(t *testing.T) {
		o := &EditOrchestrator{}
		state := &editPipelineState{
			existingSlides: []model.ExistingSlideInfo{
				{
					Index:        0,
					PageObjectID: "page0",
					TextElements: []model.ExistingText{
						{ObjectID: "obj-a", Content: "Title text"},
						{ObjectID: "obj-b", Content: "Body text"},
					},
				},
				{
					Index:        1,
					PageObjectID: "page1",
					TextElements: []model.ExistingText{
						{ObjectID: "obj-c", Content: "Other text"},
					},
				},
			},
			skeleton: &model.EditSkeleton{
				Operations: []model.SkeletonOperation{
					{
						Type:       "modify_content",
						SlideIndex: 0,
						Modifications: []model.ModificationIntent{
							{VariableName: "obj-a", Intention: "change title"},
							{VariableName: "obj-b", Intention: "update body"},
						},
					},
					{
						Type:       "modify_content",
						SlideIndex: 1,
						Modifications: []model.ModificationIntent{
							{VariableName: "obj-c", Intention: "update other"},
							{VariableName: "obj-unknown", Intention: "unknown element"},
						},
					},
				},
			},
		}

		o.enrichSkeleton(state)

		if state.skeleton.Operations[0].Modifications[0].CurrentText != "Title text" {
			t.Errorf("obj-a CurrentText = %q, want %q", state.skeleton.Operations[0].Modifications[0].CurrentText, "Title text")
		}
		if state.skeleton.Operations[0].Modifications[1].CurrentText != "Body text" {
			t.Errorf("obj-b CurrentText = %q, want %q", state.skeleton.Operations[0].Modifications[1].CurrentText, "Body text")
		}
		if state.skeleton.Operations[1].Modifications[0].CurrentText != "Other text" {
			t.Errorf("obj-c CurrentText = %q, want %q", state.skeleton.Operations[1].Modifications[0].CurrentText, "Other text")
		}
		if len(state.skeleton.Operations[1].Modifications) != 1 {
			t.Errorf("obj-unknown should have been dropped, got %d modifications", len(state.skeleton.Operations[1].Modifications))
		}
	})
}

func TestBuildSlideNeedFromSkeleton(t *testing.T) {
	t.Run("maps content intents to content items", func(t *testing.T) {
		op := model.SkeletonOperation{
			Intention: "Présenter les avantages de l'IA",
			ContentIntents: []model.ContentIntent{
				{VariableName: "titleShape", Intention: "Titre sur l'IA"},
				{VariableName: "bodyShape", Intention: "3 bullet points sur les avantages"},
				{VariableName: "footerShape", Intention: "Note de bas de page"},
			},
		}

		need := buildSlideNeedFromSkeleton(op)

		if need.Intent != "Présenter les avantages de l'IA" {
			t.Errorf("Intent = %q, want %q", need.Intent, "Présenter les avantages de l'IA")
		}
		if need.ItemCount != 3 {
			t.Errorf("ItemCount = %d, want 3", need.ItemCount)
		}
		if len(need.ContentItems) != 3 {
			t.Fatalf("expected 3 content items, got %d", len(need.ContentItems))
		}
		if need.ContentItems[0] != "Titre sur l'IA" {
			t.Errorf("ContentItems[0] = %q", need.ContentItems[0])
		}
		if !need.NeedsTitle {
			t.Error("NeedsTitle should be true")
		}
	})

	t.Run("empty content intents", func(t *testing.T) {
		op := model.SkeletonOperation{
			Intention: "Delete slide",
		}
		need := buildSlideNeedFromSkeleton(op)
		if need.ItemCount != 0 {
			t.Errorf("ItemCount = %d, want 0", need.ItemCount)
		}
	})
}

func TestEnrichModificationsWithFeedback(t *testing.T) {
	t.Run("appends feedback to matching modifications", func(t *testing.T) {
		mods := []model.ModificationIntent{
			{VariableName: "obj-a", Intention: "change title", CurrentText: "old"},
			{VariableName: "obj-b", Intention: "update body", CurrentText: "old body"},
		}
		issues := []agent.ReviewIssue{
			{Field: "obj-a", IssueType: "intention_mismatch", Description: "Title too long", Suggestion: "Shorten it"},
		}

		result := enrichModificationsWithFeedback(mods, issues)

		if len(result) != 2 {
			t.Fatalf("expected 2 modifications, got %d", len(result))
		}
		if result[0].Intention == mods[0].Intention {
			t.Error("obj-a intention should have been enriched with feedback")
		}
		if result[1].Intention != mods[1].Intention {
			t.Error("obj-b intention should be unchanged")
		}
	})

	t.Run("global issue applies to all modifications", func(t *testing.T) {
		mods := []model.ModificationIntent{
			{VariableName: "obj-a", Intention: "change", CurrentText: "old"},
			{VariableName: "obj-b", Intention: "change", CurrentText: "old"},
		}
		issues := []agent.ReviewIssue{
			{Field: "", IssueType: "coherence_break", Description: "Inconsistent", Suggestion: "Fix coherence"},
		}

		result := enrichModificationsWithFeedback(mods, issues)

		for i, mod := range result {
			if mod.Intention == "change" {
				t.Errorf("modification %d should have been enriched with global feedback", i)
			}
		}
	})
}

func TestGroupIssuesByOperation(t *testing.T) {
	group := func(issues []agent.ReviewIssue, opCount int) (map[int][]agent.ReviewIssue, []int) {
		feedbackByIndex := make(map[int][]agent.ReviewIssue)
		for _, issue := range issues {
			if issue.SlideIndex >= 0 && issue.SlideIndex < opCount {
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

	t.Run("out of bounds ignored", func(t *testing.T) {
		issues := []agent.ReviewIssue{
			{SlideIndex: 10, IssueType: "intention_mismatch"},
			{SlideIndex: 1, IssueType: "coherence_break"},
		}
		m, idx := group(issues, 3)
		if len(m) != 1 {
			t.Errorf("expected 1 entry, got %d", len(m))
		}
		if len(idx) != 1 || idx[0] != 1 {
			t.Errorf("expected indices [1], got %v", idx)
		}
	})
}
