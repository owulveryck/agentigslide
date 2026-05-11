package agent

import (
	"testing"
)

func TestParseCatalog(t *testing.T) {
	catalog := `SLIDE 83 [1 titre, 0 contenu, 1 numerotation]: Section divider
  champs: sectiontitleShape (titre_principal ~333) | sectionnumberShape (numerotation ~9)
SLIDE 161 [1 titre, 1 sous-titre, 4 contenu]: 4 quadrants
  champs: titlemainShape (titre_principal ~131) | subtitleShape (titre_principal ~131) | topleftShape (texte ~400) | toprightShape (texte ~400) | bottomleftShape (texte ~400) | bottomrightShape (texte ~400)
SLIDE 325 [AUCUN CHAMP MODIFIABLE]: Conclusion visuelle
`
	info := ParseCatalog(catalog)

	if !info.SlideNumbers[83] || !info.SlideNumbers[161] || !info.SlideNumbers[325] {
		t.Error("expected all three slide numbers to be present")
	}

	counts83 := info.FieldCountsBySlide[83]
	if counts83.Titles != 1 || counts83.Contents != 0 || counts83.Numerotation != 1 {
		t.Errorf("slide 83 counts = %+v, want Titles=1 Contents=0 Numerotation=1", counts83)
	}

	counts161 := info.FieldCountsBySlide[161]
	if counts161.Titles != 1 || counts161.Subtitles != 1 || counts161.Contents != 4 {
		t.Errorf("slide 161 counts = %+v, want Titles=1 Subtitles=1 Contents=4", counts161)
	}

	counts325 := info.FieldCountsBySlide[325]
	if !counts325.NoFields {
		t.Error("slide 325 should have NoFields=true")
	}
}

func TestValidateSelection(t *testing.T) {
	catalog := `SLIDE 83 [1 titre, 0 contenu, 1 numerotation]: Section divider
  champs: sectiontitleShape (titre_principal ~333) | sectionnumberShape (numerotation ~9)
SLIDE 161 [1 titre, 1 sous-titre, 4 contenu]: 4 quadrants
  champs: titlemainShape (titre_principal ~131) | subtitleShape (titre_principal ~131) | topleftShape (texte ~400) | toprightShape (texte ~400) | bottomleftShape (texte ~400) | bottomrightShape (texte ~400)
SLIDE 55 [1 titre, 3 contenu]: Content with bullets
  champs: slidetitleShape (titre_principal ~131) | bodybulletlistShape (texte ~589) | highlighttextShape (texte ~56) | mainheadingShape (texte ~20)
SLIDE 325 [AUCUN CHAMP MODIFIABLE]: Conclusion visuelle
`

	t.Run("section divider with sectiontitle no error", func(t *testing.T) {
		outline := &PresentationOutline{
			PresentationTitle: "Test",
			Sections: []SectionSpec{{
				Title:   "Section 1",
				Purpose: "contenu",
				SlideNeeds: []SlideNeed{{
					Intent:     "Section divider",
					ItemCount:  0,
					NeedsTitle: true,
					SlideType:  "section_divider",
				}},
			}},
		}
		selections := &SelectionPlan{
			Selections: []SlideSelection{{
				OutlineIndex: 0,
				SourceSlide:  83,
				Rationale:    "Section divider",
			}},
		}
		err := ValidateSelection(selections, outline, catalog)
		if err != nil {
			t.Errorf("expected no error for section divider, got: %v", err)
		}
	})

	t.Run("no editable fields with content items is error", func(t *testing.T) {
		outline := &PresentationOutline{
			PresentationTitle: "Test",
			Sections: []SectionSpec{{
				Title:   "Conclusion",
				Purpose: "conclusion",
				SlideNeeds: []SlideNeed{{
					Intent:       "Conclusion with content",
					ContentItems: []string{"item1", "item2"},
					ItemCount:    2,
					SlideType:    "conclusion",
				}},
			}},
		}
		selections := &SelectionPlan{
			Selections: []SlideSelection{{
				OutlineIndex: 0,
				SourceSlide:  325,
				Rationale:    "Conclusion slide",
			}},
		}
		err := ValidateSelection(selections, outline, catalog)
		if err == nil {
			t.Error("expected error for no editable fields with content items")
		}
	})

	t.Run("content slide with matching item count no error", func(t *testing.T) {
		outline := &PresentationOutline{
			PresentationTitle: "Test",
			Sections: []SectionSpec{{
				Title:   "Content",
				Purpose: "contenu",
				SlideNeeds: []SlideNeed{{
					Intent:       "4 quadrants content",
					ContentItems: []string{"a", "b", "c", "d"},
					ItemCount:    4,
					NeedsTitle:   true,
					SlideType:    "content",
				}},
			}},
		}
		selections := &SelectionPlan{
			Selections: []SlideSelection{{
				OutlineIndex: 0,
				SourceSlide:  161,
				Rationale:    "4 quadrants",
			}},
		}
		err := ValidateSelection(selections, outline, catalog)
		if err != nil {
			t.Errorf("expected no error for matching item count, got: %v", err)
		}
	})

	t.Run("items exceeding total text fields no hard error", func(t *testing.T) {
		outline := &PresentationOutline{
			PresentationTitle: "Test",
			Sections: []SectionSpec{{
				Title:   "Content",
				Purpose: "contenu",
				SlideNeeds: []SlideNeed{{
					Intent:       "Many items",
					ContentItems: []string{"a", "b", "c", "d", "e", "f", "g"},
					ItemCount:    7,
					NeedsTitle:   true,
					SlideType:    "content",
				}},
			}},
		}
		selections := &SelectionPlan{
			Selections: []SlideSelection{{
				OutlineIndex: 0,
				SourceSlide:  55,
				Rationale:    "Content slide",
			}},
		}
		err := ValidateSelection(selections, outline, catalog)
		if err != nil {
			t.Errorf("itemCount > textFields should be a warning not error, got: %v", err)
		}
	})

	t.Run("severe item count mismatch is error", func(t *testing.T) {
		outline := &PresentationOutline{
			PresentationTitle: "Test",
			Sections: []SectionSpec{{
				Title:   "Content",
				Purpose: "contenu",
				SlideNeeds: []SlideNeed{{
					Intent:       "Many items few fields",
					ContentItems: []string{"a", "b", "c", "d", "e", "f", "g"},
					ItemCount:    7,
					NeedsTitle:   true,
					SlideType:    "content",
				}},
			}},
		}
		selections := &SelectionPlan{
			Selections: []SlideSelection{{
				OutlineIndex: 0,
				SourceSlide:  83,
				Rationale:    "Section divider for 7 items",
			}},
		}
		err := ValidateSelection(selections, outline, catalog)
		if err == nil {
			t.Error("expected error when itemCount > textFields * 2")
		}
	})

	t.Run("needsTitle without title field is error", func(t *testing.T) {
		catalogNoTitle := `SLIDE 99 [0 contenu]: No title template
  champs: bodyShape (texte ~500)
`
		outline := &PresentationOutline{
			PresentationTitle: "Test",
			Sections: []SectionSpec{{
				Title:   "Content",
				Purpose: "contenu",
				SlideNeeds: []SlideNeed{{
					Intent:     "Needs a title",
					ItemCount:  0,
					NeedsTitle: true,
					SlideType:  "content",
				}},
			}},
		}
		selections := &SelectionPlan{
			Selections: []SlideSelection{{
				OutlineIndex: 0,
				SourceSlide:  99,
				Rationale:    "No title",
			}},
		}
		err := ValidateSelection(selections, outline, catalogNoTitle)
		if err == nil {
			t.Error("expected error when needsTitle=true but template has no title field")
		}
	})

	t.Run("unknown source slide is error", func(t *testing.T) {
		outline := &PresentationOutline{
			PresentationTitle: "Test",
			Sections: []SectionSpec{{
				Title:   "Content",
				Purpose: "contenu",
				SlideNeeds: []SlideNeed{{
					Intent:    "Slide",
					ItemCount: 0,
					SlideType: "content",
				}},
			}},
		}
		selections := &SelectionPlan{
			Selections: []SlideSelection{{
				OutlineIndex: 0,
				SourceSlide:  999,
				Rationale:    "Unknown slide",
			}},
		}
		err := ValidateSelection(selections, outline, catalog)
		if err == nil {
			t.Error("expected error for unknown source slide")
		}
	})

	t.Run("selection count mismatch is error", func(t *testing.T) {
		outline := &PresentationOutline{
			PresentationTitle: "Test",
			Sections: []SectionSpec{{
				Title:   "Content",
				Purpose: "contenu",
				SlideNeeds: []SlideNeed{
					{Intent: "Slide 1", ItemCount: 0, SlideType: "content"},
					{Intent: "Slide 2", ItemCount: 0, SlideType: "content"},
				},
			}},
		}
		selections := &SelectionPlan{
			Selections: []SlideSelection{{
				OutlineIndex: 0,
				SourceSlide:  83,
				Rationale:    "Only one selection for two needs",
			}},
		}
		err := ValidateSelection(selections, outline, catalog)
		if err == nil {
			t.Error("expected error for selection count mismatch")
		}
	})
}

func TestValidateSelectionGlobal(t *testing.T) {
	t.Run("consistent section dividers no error", func(t *testing.T) {
		outline := &PresentationOutline{
			PresentationTitle: "Test",
			Sections: []SectionSpec{
				{Title: "S1", Purpose: "contenu", SlideNeeds: []SlideNeed{
					{Intent: "Divider 1", SlideType: "section_divider"},
					{Intent: "Content", SlideType: "content"},
				}},
				{Title: "S2", Purpose: "contenu", SlideNeeds: []SlideNeed{
					{Intent: "Divider 2", SlideType: "section_divider"},
				}},
			},
		}
		selections := &SelectionPlan{
			Selections: []SlideSelection{
				{OutlineIndex: 0, SourceSlide: 83},
				{OutlineIndex: 1, SourceSlide: 55},
				{OutlineIndex: 2, SourceSlide: 83},
			},
		}
		if err := ValidateSelectionGlobal(selections, outline); err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("inconsistent section dividers is error", func(t *testing.T) {
		outline := &PresentationOutline{
			PresentationTitle: "Test",
			Sections: []SectionSpec{
				{Title: "S1", Purpose: "contenu", SlideNeeds: []SlideNeed{
					{Intent: "Divider 1", SlideType: "section_divider"},
				}},
				{Title: "S2", Purpose: "contenu", SlideNeeds: []SlideNeed{
					{Intent: "Divider 2", SlideType: "section_divider"},
				}},
				{Title: "S3", Purpose: "contenu", SlideNeeds: []SlideNeed{
					{Intent: "Divider 3", SlideType: "section_divider"},
				}},
			},
		}
		selections := &SelectionPlan{
			Selections: []SlideSelection{
				{OutlineIndex: 0, SourceSlide: 83},
				{OutlineIndex: 1, SourceSlide: 76},
				{OutlineIndex: 2, SourceSlide: 83},
			},
		}
		err := ValidateSelectionGlobal(selections, outline)
		if err == nil {
			t.Error("expected error for inconsistent section dividers")
		}
	})

	t.Run("single section divider no error", func(t *testing.T) {
		outline := &PresentationOutline{
			PresentationTitle: "Test",
			Sections: []SectionSpec{{
				Title: "S1", Purpose: "contenu", SlideNeeds: []SlideNeed{
					{Intent: "Divider", SlideType: "section_divider"},
				},
			}},
		}
		selections := &SelectionPlan{
			Selections: []SlideSelection{
				{OutlineIndex: 0, SourceSlide: 83},
			},
		}
		if err := ValidateSelectionGlobal(selections, outline); err != nil {
			t.Errorf("expected no error for single divider, got: %v", err)
		}
	})
}

func TestValidateOutline(t *testing.T) {
	t.Run("empty title is error", func(t *testing.T) {
		outline := &PresentationOutline{
			PresentationTitle: "",
			Sections: []SectionSpec{{
				Title:      "S1",
				SlideNeeds: []SlideNeed{{Intent: "slide", ItemCount: 0}},
			}},
		}
		if err := ValidateOutline(outline); err == nil {
			t.Error("expected error for empty title")
		}
	})

	t.Run("no sections is error", func(t *testing.T) {
		outline := &PresentationOutline{
			PresentationTitle: "Test",
			Sections:          nil,
		}
		if err := ValidateOutline(outline); err == nil {
			t.Error("expected error for no sections")
		}
	})

	t.Run("section with no slide needs is error", func(t *testing.T) {
		outline := &PresentationOutline{
			PresentationTitle: "Test",
			Sections: []SectionSpec{{
				Title:      "S1",
				SlideNeeds: nil,
			}},
		}
		if err := ValidateOutline(outline); err == nil {
			t.Error("expected error for section with no slide needs")
		}
	})

	t.Run("itemCount mismatch is error", func(t *testing.T) {
		outline := &PresentationOutline{
			PresentationTitle: "Test",
			Sections: []SectionSpec{{
				Title: "S1",
				SlideNeeds: []SlideNeed{{
					Intent:       "slide",
					ContentItems: []string{"a", "b"},
					ItemCount:    3,
				}},
			}},
		}
		if err := ValidateOutline(outline); err == nil {
			t.Error("expected error for itemCount mismatch")
		}
	})

	t.Run("valid outline no error", func(t *testing.T) {
		outline := &PresentationOutline{
			PresentationTitle: "Valid",
			Sections: []SectionSpec{{
				Title: "S1",
				SlideNeeds: []SlideNeed{{
					Intent:       "slide",
					ContentItems: []string{"a", "b"},
					ItemCount:    2,
				}},
			}},
		}
		if err := ValidateOutline(outline); err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})
}

func TestFlattenNeeds(t *testing.T) {
	t.Run("single section", func(t *testing.T) {
		outline := &PresentationOutline{
			Sections: []SectionSpec{{
				SlideNeeds: []SlideNeed{
					{Intent: "need1"},
					{Intent: "need2"},
				},
			}},
		}
		needs := FlattenNeeds(outline)
		if len(needs) != 2 {
			t.Fatalf("expected 2 needs, got %d", len(needs))
		}
		if needs[0].Intent != "need1" || needs[1].Intent != "need2" {
			t.Errorf("needs = %+v", needs)
		}
	})

	t.Run("multiple sections concatenated in order", func(t *testing.T) {
		outline := &PresentationOutline{
			Sections: []SectionSpec{
				{SlideNeeds: []SlideNeed{{Intent: "a"}, {Intent: "b"}}},
				{SlideNeeds: []SlideNeed{{Intent: "c"}}},
			},
		}
		needs := FlattenNeeds(outline)
		if len(needs) != 3 {
			t.Fatalf("expected 3 needs, got %d", len(needs))
		}
		want := []string{"a", "b", "c"}
		for i, n := range needs {
			if n.Intent != want[i] {
				t.Errorf("needs[%d].Intent = %q, want %q", i, n.Intent, want[i])
			}
		}
	})

	t.Run("empty sections", func(t *testing.T) {
		outline := &PresentationOutline{Sections: nil}
		needs := FlattenNeeds(outline)
		if len(needs) != 0 {
			t.Errorf("expected 0 needs, got %d", len(needs))
		}
	})
}

func TestParseSlideFields(t *testing.T) {
	catalog := `SLIDE 83 [1 titre, 0 contenu, 1 numerotation]: Section divider
  champs: sectiontitleShape (titre_principal ~333) | sectionnumberShape (numerotation ~9)
SLIDE 55 [1 titre, 3 contenu]: Content with bullets
  champs: slidetitleShape (titre_principal ~131) | bodybulletlistShape (texte ~589) | highlighttextShape (texte ~56) | mainheadingShape (texte ~20)
`

	t.Run("parse section divider fields", func(t *testing.T) {
		fields := ParseSlideFields(catalog, 83)
		if len(fields) != 2 {
			t.Fatalf("expected 2 fields, got %d", len(fields))
		}
		if fields[0].VariableName != "sectiontitleShape" || fields[0].Role != "titre_principal" || fields[0].MaxChars != 333 {
			t.Errorf("field 0 = %+v", fields[0])
		}
		if fields[1].VariableName != "sectionnumberShape" || fields[1].Role != "numerotation" || fields[1].MaxChars != 9 {
			t.Errorf("field 1 = %+v", fields[1])
		}
	})

	t.Run("non-existent slide returns nil", func(t *testing.T) {
		fields := ParseSlideFields(catalog, 999)
		if fields != nil {
			t.Errorf("expected nil for non-existent slide, got %v", fields)
		}
	})
}
