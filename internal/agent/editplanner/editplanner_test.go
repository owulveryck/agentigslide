package editplanner

import (
	"testing"

	"github.com/owulveryck/agentigslide/internal/model"
)

func TestFormatEditSkeleton(t *testing.T) {
	t.Run("modify_content with intentions", func(t *testing.T) {
		skeleton := &model.EditSkeleton{
			PresentationID: "pres-123",
			Operations: []model.SkeletonOperation{
				{
					Type:       "modify_content",
					SlideIndex: 2,
					Rationale:  "User asked to update the title",
					Modifications: []model.ModificationIntent{
						{VariableName: "obj-abc", Intention: "Change title to mention AI"},
					},
				},
			},
		}
		result := FormatEditSkeleton(skeleton)
		if result == "" {
			t.Fatal("FormatEditSkeleton returned empty string")
		}
		if !containsSubstring(result, "modify_content") {
			t.Error("should contain operation type")
		}
		if !containsSubstring(result, "obj-abc") {
			t.Error("should contain variable name")
		}
		if !containsSubstring(result, "Change title to mention AI") {
			t.Error("should contain intention")
		}
	})

	t.Run("replace_slide with content intents", func(t *testing.T) {
		skeleton := &model.EditSkeleton{
			Operations: []model.SkeletonOperation{
				{
					Type:           "replace_slide",
					SlideIndex:     1,
					NewSourceSlide: 42,
					Intention:      "Replace with a content slide",
					Rationale:      "Layout doesn't match",
					ContentIntents: []model.ContentIntent{
						{VariableName: "titleShape", Intention: "Title about AI"},
						{VariableName: "bodyShape", Intention: "3 bullet points"},
					},
				},
			},
		}
		result := FormatEditSkeleton(skeleton)
		if !containsSubstring(result, "replace_slide") {
			t.Error("should contain replace_slide type")
		}
		if !containsSubstring(result, "template: slide 42") {
			t.Error("should show template slide number")
		}
		if !containsSubstring(result, "titleShape") {
			t.Error("should contain content intent variable names")
		}
	})

	t.Run("delete_slide minimal output", func(t *testing.T) {
		skeleton := &model.EditSkeleton{
			Operations: []model.SkeletonOperation{
				{
					Type:       "delete_slide",
					SlideIndex: 5,
					Rationale:  "No longer needed",
				},
			},
		}
		result := FormatEditSkeleton(skeleton)
		if !containsSubstring(result, "delete_slide") {
			t.Error("should contain delete_slide type")
		}
		if !containsSubstring(result, "slide 5") {
			t.Error("should show slide index")
		}
	})

	t.Run("empty skeleton", func(t *testing.T) {
		skeleton := &model.EditSkeleton{}
		result := FormatEditSkeleton(skeleton)
		if !containsSubstring(result, "0 opérations") {
			t.Error("should indicate 0 operations")
		}
	})
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && searchSubstring(s, sub)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
