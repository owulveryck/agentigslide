package agent

import (
	"testing"
)

func TestMergeItems(t *testing.T) {
	t.Run("no merge needed", func(t *testing.T) {
		items := []string{"a", "b", "c"}
		got := mergeItems(items, 4)
		if len(got) != 3 {
			t.Errorf("expected 3 items, got %d", len(got))
		}
	})

	t.Run("merge pairs", func(t *testing.T) {
		items := []string{"a", "b", "c", "d"}
		got := mergeItems(items, 2)
		if len(got) != 2 {
			t.Fatalf("expected 2 items, got %d", len(got))
		}
		if got[0] != "a\nb" {
			t.Errorf("got[0] = %q, want %q", got[0], "a\nb")
		}
		if got[1] != "c\nd" {
			t.Errorf("got[1] = %q, want %q", got[1], "c\nd")
		}
	})

	t.Run("odd number of items", func(t *testing.T) {
		items := []string{"a", "b", "c", "d", "e"}
		got := mergeItems(items, 3)
		if len(got) != 3 {
			t.Fatalf("expected 3 items, got %d", len(got))
		}
	})

	t.Run("single item", func(t *testing.T) {
		items := []string{"a"}
		got := mergeItems(items, 1)
		if len(got) != 1 || got[0] != "a" {
			t.Errorf("expected [a], got %v", got)
		}
	})
}

func TestNormalizeOutline(t *testing.T) {
	catalog := `SLIDE 10 [1 titre, 4 contenu]: Four zones
  champs: titleShape (titre_principal ~131) | c1Shape (texte ~400) | c2Shape (texte ~400) | c3Shape (texte ~400) | c4Shape (texte ~400)
SLIDE 20 [1 titre, 2 contenu]: Two zones
  champs: titleShape (titre_principal ~131) | bodyShape (texte ~600) | sideShape (texte ~300)
`

	t.Run("items within capacity unchanged", func(t *testing.T) {
		outline := &PresentationOutline{
			PresentationTitle: "Test",
			Sections: []SectionSpec{{
				Title: "S1",
				SlideNeeds: []SlideNeed{{
					Intent:       "fits",
					ContentItems: []string{"a", "b", "c"},
					ItemCount:    3,
					SlideType:    "content",
				}},
			}},
		}
		NormalizeOutline(outline, catalog)
		need := outline.Sections[0].SlideNeeds[0]
		if need.ItemCount != 3 {
			t.Errorf("itemCount = %d, want 3", need.ItemCount)
		}
	})

	t.Run("items exceeding capacity are merged", func(t *testing.T) {
		outline := &PresentationOutline{
			PresentationTitle: "Test",
			Sections: []SectionSpec{{
				Title: "S1",
				SlideNeeds: []SlideNeed{{
					Intent:        "overflow",
					ContentItems:  []string{"a", "b", "c", "d", "e", "f", "g"},
					ItemCount:     7,
					MaxItemLength: 1,
					SlideType:     "content",
				}},
			}},
		}
		// max text fields = 1 titre + 4 contenu = 5
		NormalizeOutline(outline, catalog)
		need := outline.Sections[0].SlideNeeds[0]
		if need.ItemCount > 5 {
			t.Errorf("itemCount = %d, want <= 5", need.ItemCount)
		}
		if need.ItemCount != len(need.ContentItems) {
			t.Errorf("itemCount=%d != len(contentItems)=%d", need.ItemCount, len(need.ContentItems))
		}
	})

	t.Run("cover slides untouched", func(t *testing.T) {
		outline := &PresentationOutline{
			PresentationTitle: "Test",
			Sections: []SectionSpec{{
				Title: "S1",
				SlideNeeds: []SlideNeed{{
					Intent:       "cover",
					ContentItems: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"},
					ItemCount:    10,
					SlideType:    "cover",
				}},
			}},
		}
		NormalizeOutline(outline, catalog)
		if outline.Sections[0].SlideNeeds[0].ItemCount != 10 {
			t.Error("cover slide should not be normalized")
		}
	})

	t.Run("diagram slides untouched", func(t *testing.T) {
		outline := &PresentationOutline{
			PresentationTitle: "Test",
			Sections: []SectionSpec{{
				Title: "S1",
				SlideNeeds: []SlideNeed{{
					Intent:       "diagram",
					ContentItems: []string{"a", "b", "c", "d", "e", "f", "g", "h"},
					ItemCount:    8,
					SlideType:    "diagram",
				}},
			}},
		}
		NormalizeOutline(outline, catalog)
		if outline.Sections[0].SlideNeeds[0].ItemCount != 8 {
			t.Error("diagram slide should not be normalized")
		}
	})

	t.Run("maxItemLength updated after merge", func(t *testing.T) {
		outline := &PresentationOutline{
			PresentationTitle: "Test",
			Sections: []SectionSpec{{
				Title: "S1",
				SlideNeeds: []SlideNeed{{
					Intent:        "overflow",
					ContentItems:  []string{"short", "also short", "a", "b", "c", "d"},
					ItemCount:     6,
					MaxItemLength: 10,
					SlideType:     "content",
				}},
			}},
		}
		NormalizeOutline(outline, catalog)
		need := outline.Sections[0].SlideNeeds[0]
		if need.MaxItemLength <= 10 {
			t.Errorf("maxItemLength should increase after merging, got %d", need.MaxItemLength)
		}
	})

	t.Run("empty catalog is noop", func(t *testing.T) {
		outline := &PresentationOutline{
			PresentationTitle: "Test",
			Sections: []SectionSpec{{
				Title: "S1",
				SlideNeeds: []SlideNeed{{
					Intent:       "test",
					ContentItems: []string{"a", "b", "c"},
					ItemCount:    3,
					SlideType:    "content",
				}},
			}},
		}
		NormalizeOutline(outline, "")
		if outline.Sections[0].SlideNeeds[0].ItemCount != 3 {
			t.Error("empty catalog should not modify outline")
		}
	})
}

func TestMaxTextFields(t *testing.T) {
	catalog := `SLIDE 10 [1 titre, 4 contenu]: Four zones
  champs: titleShape (titre_principal ~131) | c1Shape (texte ~400) | c2Shape (texte ~400) | c3Shape (texte ~400) | c4Shape (texte ~400)
SLIDE 20 [1 titre, 1 sous-titre, 2 contenu]: Two zones
  champs: titleShape (titre_principal ~131) | subtitleShape (sous-titre ~131) | bodyShape (texte ~600) | sideShape (texte ~300)
`
	got := MaxTextFields(catalog)
	// slide 10: 1+4 = 5, slide 20: 1+1+2 = 4
	if got != 5 {
		t.Errorf("MaxTextFields = %d, want 5", got)
	}
}

func TestCapacitySummary(t *testing.T) {
	catalog := `SLIDE 10 [1 titre, 4 contenu]: Four zones
  champs: a (t ~1) | b (t ~1) | c (t ~1) | d (t ~1) | e (t ~1)
SLIDE 20 [2 contenu]: Two zones
  champs: a (t ~1) | b (t ~1)
SLIDE 30 [AUCUN CHAMP MODIFIABLE]: No fields
`
	summary := CapacitySummary(catalog)
	if summary == "" {
		t.Fatal("expected non-empty summary")
	}
	if !contains(summary, "Max zones texte totales") {
		t.Error("summary should contain max zones info")
	}
	if !contains(summary, "RAPPEL") {
		t.Error("summary should contain reminder")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
