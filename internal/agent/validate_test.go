package agent

import (
	"strings"
	"testing"

	"github.com/owulveryck/agentigslide/internal/model"
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

	t.Run("itemCount mismatch is auto-corrected", func(t *testing.T) {
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
		if err := ValidateOutline(outline); err != nil {
			t.Errorf("expected no error after auto-correction, got: %v", err)
		}
		if outline.Sections[0].SlideNeeds[0].ItemCount != 2 {
			t.Errorf("itemCount should have been corrected to 2, got %d",
				outline.Sections[0].SlideNeeds[0].ItemCount)
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

func TestEnforceMaxChars_ShortTitleNotOverTruncated(t *testing.T) {
	fields := []TemplateField{
		{VariableName: "step1titleShape", Role: "texte", MaxChars: 28},
	}
	content := &SlideContent{
		SourceSlide: 126,
		Modifications: []model.TextModification{
			{VariableName: "step1titleShape", NewText: "📦 Ce qu'on standardise"},
		},
	}
	EnforceMaxChars(content, fields)
	got := content.Modifications[0].NewText
	if len([]rune(got)) < 20 {
		t.Errorf("short title over-truncated: got %q (%d runes), want >= 20 runes", got, len([]rune(got)))
	}
}

func TestEnforceMaxChars_WordBreakPreserves75Percent(t *testing.T) {
	fields := []TemplateField{
		{VariableName: "titleShape", Role: "titre", MaxChars: 30},
	}
	content := &SlideContent{
		SourceSlide: 1,
		Modifications: []model.TextModification{
			{VariableName: "titleShape", NewText: "Un titre assez long pour tester la troncature intelligente"},
		},
	}
	EnforceMaxChars(content, fields)
	got := content.Modifications[0].NewText
	runeLen := len([]rune(got))
	minExpected := 30 * 3 / 4 // 75% of limit
	if runeLen < minExpected {
		t.Errorf("word-break too aggressive: got %d runes (%q), want >= %d", runeLen, got, minExpected)
	}
}

func TestEnforceMaxChars_EmojiHandling(t *testing.T) {
	fields := []TemplateField{
		{VariableName: "cardTitle", Role: "titre", MaxChars: 25},
	}
	content := &SlideContent{
		SourceSlide: 1,
		Modifications: []model.TextModification{
			{VariableName: "cardTitle", NewText: "🚀 Lancement du projet"},
		},
	}
	EnforceMaxChars(content, fields)
	got := content.Modifications[0].NewText
	if len([]rune(got)) < 20 {
		t.Errorf("emoji text over-truncated: got %q (%d runes)", got, len([]rune(got)))
	}
}

func TestEnforceMaxChars_UnderLimitNotTruncated(t *testing.T) {
	fields := []TemplateField{
		{VariableName: "titleShape", Role: "titre", MaxChars: 28},
	}
	content := &SlideContent{
		SourceSlide: 1,
		Modifications: []model.TextModification{
			{VariableName: "titleShape", NewText: "Titre court"},
		},
	}
	EnforceMaxChars(content, fields)
	if content.Modifications[0].NewText != "Titre court" {
		t.Errorf("text under limit was modified: got %q", content.Modifications[0].NewText)
	}
}

func TestSanitizeSelection_ReplacesIncompatibleTemplate(t *testing.T) {
	catalog := `SLIDE 131 [0 contenu]: Cards without title
  champs: card1bodyShape (texte ~382) | card2bodyShape (texte ~382)
SLIDE 55 [1 titre, 3 contenu]: Content with bullets
  champs: slidetitleShape (titre_principal ~131) | bodybulletlistShape (texte ~589) | highlighttextShape (texte ~56) | mainheadingShape (texte ~20)
`
	outline := &PresentationOutline{
		PresentationTitle: "Test",
		Sections: []SectionSpec{{
			Title: "Section",
			SlideNeeds: []SlideNeed{{
				Intent:     "Content with title",
				NeedsTitle: true,
				ItemCount:  2,
				SlideType:  "content",
			}},
		}},
	}
	selections := &SelectionPlan{
		Selections: []SlideSelection{{
			OutlineIndex: 0,
			SourceSlide:  131,
		}},
	}
	fixed := SanitizeSelection(selections, outline, catalog)
	if fixed == 0 {
		t.Error("expected SanitizeSelection to fix incompatible template")
	}
	if len(selections.Selections) == 0 {
		t.Fatal("selection was dropped instead of replaced")
	}
	if selections.Selections[0].SourceSlide == 131 {
		t.Errorf("template should have been replaced, still 131")
	}
}

func TestSanitizeSelection_DropsNonExistentTemplate(t *testing.T) {
	catalog := `SLIDE 55 [1 titre, 3 contenu]: Content
  champs: slidetitleShape (titre_principal ~131)
`
	outline := &PresentationOutline{
		PresentationTitle: "Test",
		Sections: []SectionSpec{{
			Title:      "Section",
			SlideNeeds: []SlideNeed{{Intent: "Slide", SlideType: "content"}},
		}},
	}
	selections := &SelectionPlan{
		Selections: []SlideSelection{{
			OutlineIndex: 0,
			SourceSlide:  999,
		}},
	}
	fixed := SanitizeSelection(selections, outline, catalog)
	if fixed == 0 {
		t.Error("expected SanitizeSelection to drop non-existent template")
	}
	if len(selections.Selections) != 0 {
		t.Errorf("expected empty selections after drop, got %d", len(selections.Selections))
	}
}

func TestValidateSelection_StepGroupCapacity(t *testing.T) {
	catalog := `SLIDE 126 [1 titre, 1 sous-titre, 4 contenu]: Sommaire
  champs: maintitleShape (titre_principal ~131) | subtitleShape (titre_principal ~131) | step1numberShape (numerotation ~4) | step1titleShape (texte ~28) | step2numberShape (numerotation ~4) | step2titleShape (texte ~28) | step3numberShape (numerotation ~4) | step3titleShape (texte ~28) | step4numberShape (numerotation ~4) | step4titleShape (texte ~28)
`
	outline := &PresentationOutline{
		PresentationTitle: "Test",
		Sections: []SectionSpec{{
			Title: "Sommaire",
			SlideNeeds: []SlideNeed{{
				Intent:       "Sommaire with 6 sections",
				ContentItems: []string{"H1", "H2", "H3", "H4", "Le point", "Actions"},
				ItemCount:    6,
				NeedsTitle:   true,
				SlideType:    "content",
			}},
		}},
	}
	selections := &SelectionPlan{
		Selections: []SlideSelection{{
			OutlineIndex: 0,
			SourceSlide:  126,
		}},
	}
	err := ValidateSelection(selections, outline, catalog)
	if err == nil {
		t.Error("expected error when itemCount (6) exceeds step group count (4)")
	}
}

func TestCountStepGroups(t *testing.T) {
	fields := []TemplateField{
		{VariableName: "maintitleShape"},
		{VariableName: "step1numberShape"},
		{VariableName: "step1titleShape"},
		{VariableName: "step2numberShape"},
		{VariableName: "step2titleShape"},
		{VariableName: "step3numberShape"},
		{VariableName: "step3titleShape"},
		{VariableName: "step4numberShape"},
		{VariableName: "step4titleShape"},
	}
	got := countStepGroups(fields)
	if got != 4 {
		t.Errorf("countStepGroups = %d, want 4", got)
	}
}

func TestCountStepGroups_NoSteps(t *testing.T) {
	fields := []TemplateField{
		{VariableName: "maintitleShape"},
		{VariableName: "bodyShape"},
	}
	got := countStepGroups(fields)
	if got != 0 {
		t.Errorf("countStepGroups = %d, want 0", got)
	}
}

func TestValidateSelection_NeedsTitleErrorIncludesCandidates(t *testing.T) {
	catalog := `SLIDE 99 [0 contenu]: No title
  champs: bodyShape (texte ~500)
SLIDE 55 [1 titre, 3 contenu]: Has title
  champs: slidetitleShape (titre_principal ~131) | bodybulletlistShape (texte ~589)
`
	outline := &PresentationOutline{
		PresentationTitle: "Test",
		Sections: []SectionSpec{{
			Title: "Content",
			SlideNeeds: []SlideNeed{{
				Intent:     "Need title",
				NeedsTitle: true,
				SlideType:  "content",
			}},
		}},
	}
	selections := &SelectionPlan{
		Selections: []SlideSelection{{
			OutlineIndex: 0,
			SourceSlide:  99,
		}},
	}
	err := ValidateSelection(selections, outline, catalog)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "55") {
		t.Errorf("error should mention candidate template 55, got: %v", err)
	}
}

func TestEnforceMaxChars_EllipsisOnWordBreak(t *testing.T) {
	fields := []TemplateField{
		{VariableName: "bodyShape", Role: "texte", MaxChars: 40},
	}
	content := &SlideContent{
		SourceSlide: 1,
		Modifications: []model.TextModification{
			{VariableName: "bodyShape", NewText: "Ceci est un texte assez long pour être tronqué intelligemment"},
		},
	}
	EnforceMaxChars(content, fields)
	got := content.Modifications[0].NewText
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected ellipsis at end of truncated text, got %q", got)
	}
}

func TestEnforceMaxChars_TitleEffectiveLimit(t *testing.T) {
	fields := []TemplateField{
		{VariableName: "titleShape", Role: "titre_principal", MaxChars: 30},
	}
	content := &SlideContent{
		SourceSlide: 1,
		Modifications: []model.TextModification{
			{VariableName: "titleShape", NewText: "Un titre qui fait exactement trente car"},
		},
	}
	EnforceMaxChars(content, fields)
	got := content.Modifications[0].NewText
	if len([]rune(got)) > 27 {
		t.Errorf("title field should use 90%% effective limit (27), got %d runes: %q", len([]rune(got)), got)
	}
}

func TestParseTemplateSuggestion(t *testing.T) {
	tests := []struct {
		name       string
		suggestion string
		wantSlide  int
		wantOK     bool
	}{
		{"sourceSlide number", "Remplacer sourceSlide 214 par sourceSlide 250", 214, true},
		{"SLIDE uppercase", "Remplacer le slide par SLIDE 250", 250, true},
		{"suggestion without number", "Choose a better template", 0, false},
		{"empty string", "", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseTemplateSuggestion(tt.suggestion)
			if ok != tt.wantOK || got != tt.wantSlide {
				t.Errorf("ParseTemplateSuggestion(%q) = (%d, %v), want (%d, %v)", tt.suggestion, got, ok, tt.wantSlide, tt.wantOK)
			}
		})
	}
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

func TestEligibleSlidesForNeed_ConsistentWithValidation(t *testing.T) {
	catalog := `SLIDE 83 [1 titre, 0 contenu, 1 numerotation]: Section divider
  champs: sectiontitleShape (titre_principal ~333) | sectionnumberShape (numerotation ~9)
SLIDE 130 [0 titre, 1 sous-titre, 2 contenu]: Subtitle only
  champs: subtitleShape (sous_titre ~100) | leftShape (texte ~300) | rightShape (texte ~300)
SLIDE 161 [1 titre, 1 sous-titre, 4 contenu]: 4 quadrants
  champs: titlemainShape (titre_principal ~131) | subtitleShape (sous_titre ~131) | topleftShape (texte ~400) | toprightShape (texte ~400) | bottomleftShape (texte ~400) | bottomrightShape (texte ~400)
SLIDE 325 [AUCUN CHAMP MODIFIABLE]: Conclusion visuelle
`
	info := ParseCatalog(catalog)
	need := SlideNeed{
		Intent:       "content with title",
		NeedsTitle:   true,
		ItemCount:    2,
		ContentItems: []string{"a", "b"},
		SlideType:    "content",
	}

	eligible := EligibleSlidesForNeed(need, &info)

	// Slide 130 has a subtitle but NO title: it must NOT be eligible,
	// because ValidateSelection rejects needsTitle on a title-less slide.
	for _, n := range eligible {
		if n == 130 {
			t.Fatal("slide 130 (no title field) must not be eligible for needsTitle=true")
		}
	}

	// Every eligible slide must pass ValidateSelection for this need.
	outline := &PresentationOutline{
		PresentationTitle: "Test",
		Sections: []SectionSpec{{
			Title: "S", Purpose: "p",
			SlideNeeds: []SlideNeed{need},
		}},
	}
	for _, n := range eligible {
		selections := &SelectionPlan{Selections: []SlideSelection{{OutlineIndex: 0, SourceSlide: n, Rationale: "r"}}}
		if err := ValidateSelection(selections, outline, catalog); err != nil {
			t.Errorf("eligible slide %d rejected by ValidateSelection: %v", n, err)
		}
	}
	if len(eligible) == 0 {
		t.Fatal("expected at least one eligible slide (161)")
	}
}

func TestValidateSelectionDetailed_PerEntryIssues(t *testing.T) {
	catalog := `SLIDE 83 [1 titre, 0 contenu, 1 numerotation]: Section divider
  champs: sectiontitleShape (titre_principal ~333) | sectionnumberShape (numerotation ~9)
SLIDE 161 [1 titre, 1 sous-titre, 4 contenu]: 4 quadrants
  champs: titlemainShape (titre_principal ~131) | subtitleShape (sous_titre ~131) | topleftShape (texte ~400) | toprightShape (texte ~400) | bottomleftShape (texte ~400) | bottomrightShape (texte ~400)
`
	outline := &PresentationOutline{
		PresentationTitle: "Test",
		Sections: []SectionSpec{{
			Title: "S", Purpose: "p",
			SlideNeeds: []SlideNeed{
				{Intent: "ok", NeedsTitle: true, ItemCount: 2, ContentItems: []string{"a", "b"}, SlideType: "content"},
				{Intent: "bad", NeedsTitle: true, ItemCount: 1, ContentItems: []string{"a"}, SlideType: "content"},
			},
		}},
	}
	selections := &SelectionPlan{Selections: []SlideSelection{
		{OutlineIndex: 0, SourceSlide: 161, Rationale: "r"},
		{OutlineIndex: 1, SourceSlide: 999, Rationale: "r"}, // out of catalog
	}}

	issues, err := ValidateSelectionDetailed(selections, outline, catalog)
	if err != nil {
		t.Fatalf("unexpected global error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %+v", len(issues), issues)
	}
	if issues[0].SelectionIndex != 1 || issues[0].OutlineIndex != 1 {
		t.Errorf("issue indices = %+v, want SelectionIndex=1 OutlineIndex=1", issues[0])
	}

	// Count mismatch is a global error, not per-entry issues.
	short := &SelectionPlan{Selections: selections.Selections[:1]}
	if _, err := ValidateSelectionDetailed(short, outline, catalog); err == nil {
		t.Error("expected count mismatch error")
	}
}

func TestOverLimitFields_MatchesEnforceMaxChars(t *testing.T) {
	fields := []TemplateField{
		{VariableName: "titleShape", Role: "titre_principal", MaxChars: 30},
		{VariableName: "bodyShape", Role: "texte", MaxChars: 100},
	}
	content := &SlideContent{
		SourceSlide: 1,
		Modifications: []model.TextModification{
			{VariableName: "titleShape", NewText: strings.Repeat("a", 28)}, // > 27 (90% de 30)
			{VariableName: "bodyShape", NewText: strings.Repeat("b", 100)}, // == limite
		},
	}

	overruns := OverLimitFields(content, fields)
	if len(overruns) != 1 {
		t.Fatalf("expected 1 overrun, got %d: %+v", len(overruns), overruns)
	}
	if overruns[0].VariableName != "titleShape" || overruns[0].Limit != 27 {
		t.Errorf("overrun = %+v, want titleShape with limit 27", overruns[0])
	}

	// EnforceMaxChars must truncate exactly the fields OverLimitFields flags.
	EnforceMaxChars(content, fields)
	if got := len([]rune(content.Modifications[0].NewText)); got > 27 {
		t.Errorf("title not truncated to effective limit: len=%d", got)
	}
	if got := len([]rune(content.Modifications[1].NewText)); got != 100 {
		t.Errorf("body should be untouched, len=%d", got)
	}
}

func TestParseSlideFields_LineGeometry(t *testing.T) {
	catalog := `SLIDE 7 [1 titre, 1 contenu]: Test
  champs: titleShape (titre_principal ~50) | bodyShape (texte ~240 6Lx40C)
`
	fields := ParseSlideFields(catalog, 7)
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fields))
	}
	if fields[1].Lines != 6 || fields[1].CharsPerLine != 40 {
		t.Errorf("bodyShape geometry = %dLx%dC, want 6Lx40C", fields[1].Lines, fields[1].CharsPerLine)
	}
	if fields[0].Lines != 0 {
		t.Errorf("titleShape should have no geometry, got %dL", fields[0].Lines)
	}
}

func TestValidateDeckInvariants(t *testing.T) {
	inv := DeckInvariants{CoverSlide: 1, ClosingSlide: 325}
	plan := &model.GenerationPlan{Slides: []model.SlideRequest{
		{SourceSlide: 15, Modifications: []model.TextModification{{VariableName: "t", NewText: "x"}}},
		{SourceSlide: 50, Modifications: []model.TextModification{{VariableName: "t", NewText: "y"}}},
	}}

	issues := ValidateDeckInvariants(plan, inv)
	if len(issues) != 2 {
		t.Fatalf("expected 2 invariant violations (cover + closing), got %d: %+v", len(issues), issues)
	}
	for _, issue := range issues {
		if issue.IssueType != "deck_invariant" {
			t.Errorf("issueType = %q, want deck_invariant", issue.IssueType)
		}
		if issue.SlideIndex != -1 {
			t.Errorf("SlideIndex = %d, want -1 (structural, never routed to writers)", issue.SlideIndex)
		}
	}

	// Conforming deck: cover first, closing last, no violation.
	plan.Slides = append([]model.SlideRequest{{SourceSlide: 1}}, plan.Slides...)
	plan.Slides = append(plan.Slides, model.SlideRequest{SourceSlide: 325})
	if issues := ValidateDeckInvariants(plan, inv); len(issues) != 0 {
		t.Fatalf("conforming deck should pass, got %+v", issues)
	}

	// Closing slide duplicated in the middle is a violation.
	plan.Slides[1].SourceSlide = 325
	if issues := ValidateDeckInvariants(plan, inv); len(issues) == 0 {
		t.Fatal("closing slide in the middle of the deck should be flagged")
	}
}

func TestPreReviewValidation_InvariantSlidesExempt(t *testing.T) {
	catalog := `SLIDE 15 [1 titre]: Intercalaire
  champs: titleShape (titre_principal ~50)
`
	inv := DeckInvariants{CoverSlide: 1, ClosingSlide: 325}
	// Cover and closing are configured invariants: absent from the catalog
	// and without modifications, they must NOT raise wrong_template or
	// missing_content (they are decorative, applied by construction).
	plan := &model.GenerationPlan{Slides: []model.SlideRequest{
		{SourceSlide: 1, Modifications: []model.TextModification{{VariableName: "maintitleShape", NewText: "Titre"}}},
		{SourceSlide: 15, Modifications: []model.TextModification{{VariableName: "titleShape", NewText: "Section"}}},
		{SourceSlide: 325},
	}}
	issues := PreReviewValidation(plan, catalog, inv)
	if len(issues) != 0 {
		t.Fatalf("invariant slides must be exempt from catalog/content checks, got %+v", issues)
	}

	// Without the invariants configured, the same plan is flagged.
	issues = PreReviewValidation(plan, catalog, DeckInvariants{})
	if len(issues) == 0 {
		t.Fatal("expected wrong_template/missing_content without configured invariants")
	}
}

func TestCrossCheckReviewIssues(t *testing.T) {
	catalogText := `SLIDE 35 [1 titre, 1 contenu]: Présentation OCTO
  champs: titleShape (titre_principal ~50) | bodyShape (texte ~240 6Lx40C)
SLIDE 325 [AUCUN CHAMP MODIFIABLE]: Conclusion officielle
`
	catalog := ParseCatalog(catalogText)
	inv := DeckInvariants{CoverSlide: 1, ClosingSlide: 325}
	plan := &model.GenerationPlan{Slides: []model.SlideRequest{
		{SourceSlide: 1},
		{SourceSlide: 35, Modifications: []model.TextModification{
			{VariableName: "bodyShape", NewText: strings.Repeat("a", 100)},
		}},
		{SourceSlide: 325},
	}}

	issues := []ReviewIssue{
		// False positive reproduced from edito-trace-v3: the reviewer claimed
		// SLIDE 325 was absent from the catalog while it exists (and is the
		// configured closing slide).
		{SlideIndex: 2, IssueType: "wrong_template", Description: "SLIDE 325 n'existe pas dans le catalogue"},
		// False positive: 100 chars in a 240-char field is not an overflow.
		{SlideIndex: 1, Field: "bodyShape", IssueType: "overflow", Description: "dépassement"},
		// Genuine semantic finding: must be kept.
		{SlideIndex: 1, IssueType: "invented_content", Description: "contenu générique inventé"},
	}

	kept, dropped := CrossCheckReviewIssues(issues, plan, catalog, inv)
	if len(dropped) != 2 {
		t.Fatalf("expected 2 dropped false positives, got %d: %+v", len(dropped), dropped)
	}
	if len(kept) != 1 || kept[0].IssueType != "invented_content" {
		t.Fatalf("expected only the semantic finding kept, got %+v", kept)
	}

	// A real overflow must be kept.
	plan.Slides[1].Modifications[0].NewText = strings.Repeat("a", 300)
	kept, _ = CrossCheckReviewIssues(issues[1:2], plan, catalog, inv)
	if len(kept) != 1 {
		t.Fatalf("real overflow must be kept, got %+v", kept)
	}
}

func TestCheckTextHeuristics(t *testing.T) {
	catalogText := `SLIDE 7 [1 titre, 1 contenu]: Test
  champs: titleShape (titre_principal ~50) | bodyShape (texte ~120 4Lx39C)
`
	catalog := ParseCatalog(catalogText)

	t.Run("bullets in title field", func(t *testing.T) {
		plan := &model.GenerationPlan{Slides: []model.SlideRequest{{
			SourceSlide: 7,
			Modifications: []model.TextModification{
				{VariableName: "titleShape", NewText: "- premier\n- second"},
			},
		}}}
		issues := CheckTextHeuristics(plan, catalog)
		if len(issues) != 1 || issues[0].IssueType != "inappropriate_bullets" {
			t.Fatalf("expected inappropriate_bullets, got %+v", issues)
		}
	})

	t.Run("text exceeding line geometry", func(t *testing.T) {
		// 6 hard lines in a 4-line box: physically cannot fit.
		plan := &model.GenerationPlan{Slides: []model.SlideRequest{{
			SourceSlide: 7,
			Modifications: []model.TextModification{
				{VariableName: "bodyShape", NewText: "a\nb\nc\nd\ne\nf"},
			},
		}}}
		issues := CheckTextHeuristics(plan, catalog)
		if len(issues) != 1 || issues[0].IssueType != "text_density" {
			t.Fatalf("expected text_density, got %+v", issues)
		}
	})

	t.Run("fitting text passes", func(t *testing.T) {
		plan := &model.GenerationPlan{Slides: []model.SlideRequest{{
			SourceSlide: 7,
			Modifications: []model.TextModification{
				{VariableName: "titleShape", NewText: "Un titre simple"},
				{VariableName: "bodyShape", NewText: "Deux phrases courtes. Sans saut de ligne."},
			},
		}}}
		if issues := CheckTextHeuristics(plan, catalog); len(issues) != 0 {
			t.Fatalf("expected no issues, got %+v", issues)
		}
	})
}
