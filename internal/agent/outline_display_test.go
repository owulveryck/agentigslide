package agent

import (
	"strings"
	"testing"
)

func TestFormatOutline(t *testing.T) {
	t.Run("basic outline", func(t *testing.T) {
		outline := &PresentationOutline{
			PresentationTitle: "Test Presentation",
			Sections: []SectionSpec{
				{
					Title:   "Introduction",
					Purpose: "introduction",
					SlideNeeds: []SlideNeed{
						{SlideType: "cover", Intent: "Title slide", ContentItems: []string{"Welcome"}, ItemCount: 1},
					},
				},
				{
					Title:   "Body",
					Purpose: "contenu",
					SlideNeeds: []SlideNeed{
						{SlideType: "content", Intent: "Main point", ContentItems: []string{"Point A", "Point B"}, ItemCount: 2},
						{SlideType: "data", Intent: "Chart slide"},
					},
				},
			},
		}

		result := FormatOutline(outline)

		if !strings.Contains(result, "# Test Presentation") {
			t.Error("should contain presentation title as markdown heading")
		}
		if !strings.Contains(result, "## Section 1: Introduction") {
			t.Error("should contain section 1 as markdown heading")
		}
		if !strings.Contains(result, "## Section 2: Body") {
			t.Error("should contain section 2 as markdown heading")
		}
		if !strings.Contains(result, "*Total: 3 slides, 2 sections*") {
			t.Errorf("should contain correct totals, got: %s", result)
		}
		if !strings.Contains(result, `"Point A"`) {
			t.Error("should contain content items")
		}
	})

	t.Run("empty outline", func(t *testing.T) {
		outline := &PresentationOutline{PresentationTitle: "Empty"}
		result := FormatOutline(outline)
		if !strings.Contains(result, "*Total: 0 slides, 0 sections*") {
			t.Errorf("expected 0 slides and sections, got: %s", result)
		}
	})

	t.Run("long content item truncated", func(t *testing.T) {
		longItem := strings.Repeat("x", 100)
		outline := &PresentationOutline{
			PresentationTitle: "T",
			Sections: []SectionSpec{{
				Title:   "S",
				Purpose: "p",
				SlideNeeds: []SlideNeed{{
					SlideType:    "content",
					Intent:       "test",
					ContentItems: []string{longItem},
					ItemCount:    1,
				}},
			}},
		}
		result := FormatOutline(outline)
		if !strings.Contains(result, "...") {
			t.Error("long items should be truncated with ...")
		}
	})
}
