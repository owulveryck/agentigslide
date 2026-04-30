package plan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"example.com/internal/model"
)

func TestSizeLabel(t *testing.T) {
	tests := []struct {
		name     string
		maxChars int
		want     string
	}{
		{"zero", 0, "petit"},
		{"ten", 10, "petit"},
		{"thirty boundary", 30, "petit"},
		{"thirty-one", 31, "moyen"},
		{"hundred", 100, "moyen"},
		{"one-fifty boundary", 150, "moyen"},
		{"one-fifty-one", 151, "grand"},
		{"five-hundred", 500, "grand"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SizeLabel(tt.maxChars)
			if got != tt.want {
				t.Errorf("SizeLabel(%d) = %q, want %q", tt.maxChars, got, tt.want)
			}
		})
	}
}

func TestIsContentField(t *testing.T) {
	tests := []struct {
		role string
		want bool
	}{
		{"annee", false},
		{"copyright", false},
		{"entreprise", false},
		{"numero_page", false},
		{"page", false},
		{"titre", true},
		{"sous_titre", true},
		{"contenu", true},
		{"description", true},
		{"", true},
	}
	for _, tt := range tests {
		name := tt.role
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			got := IsContentField(tt.role)
			if got != tt.want {
				t.Errorf("IsContentField(%q) = %v, want %v", tt.role, got, tt.want)
			}
		})
	}
}

func TestBuildCompactIndex(t *testing.T) {
	t.Run("empty index", func(t *testing.T) {
		index := &model.TemplateIndex{Slides: nil}
		got := BuildCompactIndex(index)
		if got != "" {
			t.Errorf("expected empty string for empty index, got %q", got)
		}
	})

	t.Run("single slide no fields", func(t *testing.T) {
		index := &model.TemplateIndex{
			Slides: []model.TemplateSlide{
				{
					SlideNumber: 1,
					Intention:   "Title slide",
				},
			},
		}
		got := BuildCompactIndex(index)
		want := "SLIDE 1 [0 champs de contenu]: Title slide\n"
		if got != want {
			t.Errorf("got:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("single slide with content fields keywords and maxChars", func(t *testing.T) {
		index := &model.TemplateIndex{
			Slides: []model.TemplateSlide{
				{
					SlideNumber: 5,
					Intention:   "Agenda slide",
					Keywords:    []string{"agenda", "plan"},
					EditableFields: []model.EditableFieldSummary{
						{
							ObjectID:     "obj1",
							Role:         "titre",
							VariableName: "titleShape",
							MaxChars:     25,
							Content:      "Default Title",
						},
						{
							ObjectID:     "obj2",
							Role:         "contenu",
							VariableName: "contentShape",
							MaxChars:     200,
							Content:      "Some content here",
						},
						{
							ObjectID:     "obj3",
							Role:         "annee",
							VariableName: "yearShape",
							MaxChars:     4,
							Content:      "2026",
						},
					},
				},
			},
		}
		got := BuildCompactIndex(index)
		// 2 content fields (titre + contenu), annee is excluded
		if !strings.Contains(got, "SLIDE 5 [2 champs de contenu]: Agenda slide") {
			t.Errorf("header mismatch, got:\n%s", got)
		}
		if !strings.Contains(got, "mots-clés: agenda, plan") {
			t.Errorf("keywords mismatch, got:\n%s", got)
		}
		if !strings.Contains(got, "titleShape (role: titre, taille: petit ~25 car.") {
			t.Errorf("title field mismatch, got:\n%s", got)
		}
		if !strings.Contains(got, "contentShape (role: contenu, taille: grand ~200 car.") {
			t.Errorf("content field mismatch, got:\n%s", got)
		}
		if !strings.Contains(got, "yearShape (role: annee, taille: petit ~4 car.") {
			t.Errorf("year field mismatch, got:\n%s", got)
		}
	})

	t.Run("multiple slides", func(t *testing.T) {
		index := &model.TemplateIndex{
			Slides: []model.TemplateSlide{
				{SlideNumber: 1, Intention: "First"},
				{SlideNumber: 2, Intention: "Second"},
			},
		}
		got := BuildCompactIndex(index)
		if !strings.Contains(got, "SLIDE 1") || !strings.Contains(got, "SLIDE 2") {
			t.Errorf("expected both slides, got:\n%s", got)
		}
	})

	t.Run("keywords limited to 8", func(t *testing.T) {
		keywords := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
		index := &model.TemplateIndex{
			Slides: []model.TemplateSlide{
				{SlideNumber: 1, Intention: "Test", Keywords: keywords},
			},
		}
		got := BuildCompactIndex(index)
		// Should include first 8, not 9th or 10th
		if !strings.Contains(got, "a, b, c, d, e, f, g, h") {
			t.Errorf("expected 8 keywords joined, got:\n%s", got)
		}
		if strings.Contains(got, ", i") {
			t.Errorf("should not contain 9th keyword, got:\n%s", got)
		}
	})

	t.Run("content truncation at 50 chars", func(t *testing.T) {
		longContent := strings.Repeat("x", 60)
		index := &model.TemplateIndex{
			Slides: []model.TemplateSlide{
				{
					SlideNumber: 1,
					Intention:   "Test",
					EditableFields: []model.EditableFieldSummary{
						{
							VariableName: "field1",
							Role:         "titre",
							Content:      longContent,
						},
					},
				},
			},
		}
		got := BuildCompactIndex(index)
		truncated := strings.Repeat("x", 50) + "..."
		if !strings.Contains(got, truncated) {
			t.Errorf("expected truncated content with '...', got:\n%s", got)
		}
	})

	t.Run("content exactly 50 chars not truncated", func(t *testing.T) {
		content50 := strings.Repeat("y", 50)
		index := &model.TemplateIndex{
			Slides: []model.TemplateSlide{
				{
					SlideNumber: 1,
					Intention:   "Test",
					EditableFields: []model.EditableFieldSummary{
						{
							VariableName: "field1",
							Role:         "titre",
							Content:      content50,
						},
					},
				},
			},
		}
		got := BuildCompactIndex(index)
		if strings.Contains(got, "...") {
			t.Errorf("50-char content should not be truncated, got:\n%s", got)
		}
	})

	t.Run("non-content roles excluded from count", func(t *testing.T) {
		index := &model.TemplateIndex{
			Slides: []model.TemplateSlide{
				{
					SlideNumber: 1,
					Intention:   "Test",
					EditableFields: []model.EditableFieldSummary{
						{VariableName: "a", Role: "annee"},
						{VariableName: "b", Role: "copyright"},
						{VariableName: "c", Role: "entreprise"},
						{VariableName: "d", Role: "numero_page"},
						{VariableName: "e", Role: "page"},
						{VariableName: "f", Role: "titre"},
					},
				},
			},
		}
		got := BuildCompactIndex(index)
		if !strings.Contains(got, "[1 champs de contenu]") {
			t.Errorf("expected 1 content field, got:\n%s", got)
		}
	})
}

func TestDeduplicateModifications(t *testing.T) {
	strPtr := func(s string) *string { return &s }

	t.Run("no modifications", func(t *testing.T) {
		spec := &model.SlideSpec{
			EditableObjects: []model.EditableObject{
				{ObjectID: "a", VariableName: "v1", Modified: false},
			},
		}
		DeduplicateModifications(spec)
		if spec.EditableObjects[0].Modified {
			t.Error("unmodified object should remain unmodified")
		}
	})

	t.Run("all unique texts", func(t *testing.T) {
		spec := &model.SlideSpec{
			EditableObjects: []model.EditableObject{
				{ObjectID: "a", VariableName: "v1", Modified: true, NewValue: strPtr("Hello World")},
				{ObjectID: "b", VariableName: "v2", Modified: true, NewValue: strPtr("Different Text")},
			},
		}
		DeduplicateModifications(spec)
		if !spec.EditableObjects[0].Modified || spec.EditableObjects[0].NewValue == nil {
			t.Error("first unique object should remain modified")
		}
		if !spec.EditableObjects[1].Modified || spec.EditableObjects[1].NewValue == nil {
			t.Error("second unique object should remain modified")
		}
	})

	t.Run("duplicate text longer than 3 chars clears second", func(t *testing.T) {
		spec := &model.SlideSpec{
			SourceSlideNumber: 1,
			EditableObjects: []model.EditableObject{
				{ObjectID: "a", VariableName: "v1", Modified: true, NewValue: strPtr("Duplicate Content")},
				{ObjectID: "b", VariableName: "v2", Modified: true, NewValue: strPtr("Duplicate Content")},
			},
		}
		DeduplicateModifications(spec)
		if !spec.EditableObjects[0].Modified || spec.EditableObjects[0].NewValue == nil {
			t.Error("first occurrence should be kept")
		}
		if spec.EditableObjects[1].Modified {
			t.Error("second occurrence should have Modified=false")
		}
		if spec.EditableObjects[1].NewValue != nil {
			t.Error("second occurrence should have NewValue=nil")
		}
	})

	t.Run("duplicate short text 3 chars or less kept", func(t *testing.T) {
		spec := &model.SlideSpec{
			EditableObjects: []model.EditableObject{
				{ObjectID: "a", VariableName: "v1", Modified: true, NewValue: strPtr("Hi")},
				{ObjectID: "b", VariableName: "v2", Modified: true, NewValue: strPtr("Hi")},
			},
		}
		DeduplicateModifications(spec)
		if !spec.EditableObjects[0].Modified || spec.EditableObjects[0].NewValue == nil {
			t.Error("first short dup should be kept")
		}
		if !spec.EditableObjects[1].Modified || spec.EditableObjects[1].NewValue == nil {
			t.Error("second short dup should also be kept")
		}
	})

	t.Run("exactly 3 chars kept", func(t *testing.T) {
		spec := &model.SlideSpec{
			EditableObjects: []model.EditableObject{
				{ObjectID: "a", VariableName: "v1", Modified: true, NewValue: strPtr("abc")},
				{ObjectID: "b", VariableName: "v2", Modified: true, NewValue: strPtr("abc")},
			},
		}
		DeduplicateModifications(spec)
		if !spec.EditableObjects[1].Modified {
			t.Error("3-char duplicate should be kept")
		}
	})

	t.Run("exactly 4 chars deduplicated", func(t *testing.T) {
		spec := &model.SlideSpec{
			SourceSlideNumber: 1,
			EditableObjects: []model.EditableObject{
				{ObjectID: "a", VariableName: "v1", Modified: true, NewValue: strPtr("abcd")},
				{ObjectID: "b", VariableName: "v2", Modified: true, NewValue: strPtr("abcd")},
			},
		}
		DeduplicateModifications(spec)
		if spec.EditableObjects[1].Modified {
			t.Error("4-char duplicate should be deduplicated")
		}
	})

	t.Run("unmodified objects skipped", func(t *testing.T) {
		spec := &model.SlideSpec{
			SourceSlideNumber: 1,
			EditableObjects: []model.EditableObject{
				{ObjectID: "a", VariableName: "v1", Modified: false, NewValue: nil},
				{ObjectID: "b", VariableName: "v2", Modified: true, NewValue: strPtr("Some Text")},
				{ObjectID: "c", VariableName: "v3", Modified: true, NewValue: strPtr("Some Text")},
			},
		}
		DeduplicateModifications(spec)
		// First unmodified object untouched
		if spec.EditableObjects[0].Modified {
			t.Error("unmodified object should stay unmodified")
		}
		// v2 kept, v3 deduplicated
		if !spec.EditableObjects[1].Modified {
			t.Error("first modified occurrence should be kept")
		}
		if spec.EditableObjects[2].Modified {
			t.Error("second modified occurrence should be cleared")
		}
	})

	t.Run("whitespace trimmed for comparison", func(t *testing.T) {
		spec := &model.SlideSpec{
			SourceSlideNumber: 1,
			EditableObjects: []model.EditableObject{
				{ObjectID: "a", VariableName: "v1", Modified: true, NewValue: strPtr("  Hello World  ")},
				{ObjectID: "b", VariableName: "v2", Modified: true, NewValue: strPtr("Hello World")},
			},
		}
		DeduplicateModifications(spec)
		if spec.EditableObjects[1].Modified {
			t.Error("trimmed duplicate should be cleared")
		}
	})
}

func TestLoadTemplateIndex(t *testing.T) {
	t.Run("valid JSON file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "template_index.json")
		index := model.TemplateIndex{
			TemplateID: "test-id",
			Slides: []model.TemplateSlide{
				{
					SlideNumber: 1,
					SlideID:     "s1",
					Intention:   "Title",
					Keywords:    []string{"intro"},
					EditableFields: []model.EditableFieldSummary{
						{ObjectID: "o1", VariableName: "titleShape", Role: "titre"},
					},
				},
				{
					SlideNumber: 2,
					SlideID:     "s2",
					Intention:   "Content",
				},
			},
		}
		data, err := json.Marshal(index)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}

		got, err := LoadTemplateIndex(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.TemplateID != "test-id" {
			t.Errorf("TemplateID = %q, want %q", got.TemplateID, "test-id")
		}
		if len(got.Slides) != 2 {
			t.Fatalf("len(Slides) = %d, want 2", len(got.Slides))
		}
		if got.Slides[0].SlideNumber != 1 {
			t.Errorf("Slides[0].SlideNumber = %d, want 1", got.Slides[0].SlideNumber)
		}
		if got.Slides[0].Intention != "Title" {
			t.Errorf("Slides[0].Intention = %q, want %q", got.Slides[0].Intention, "Title")
		}
		if len(got.Slides[0].EditableFields) != 1 {
			t.Fatalf("len(EditableFields) = %d, want 1", len(got.Slides[0].EditableFields))
		}
		if got.Slides[0].EditableFields[0].VariableName != "titleShape" {
			t.Errorf("VariableName = %q, want %q", got.Slides[0].EditableFields[0].VariableName, "titleShape")
		}
	})

	t.Run("non-existent file", func(t *testing.T) {
		_, err := LoadTemplateIndex("/nonexistent/path/template_index.json")
		if err == nil {
			t.Fatal("expected error for non-existent file")
		}
		if !strings.Contains(err.Error(), "failed to read") {
			t.Errorf("error should mention 'failed to read', got: %v", err)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.json")
		if err := os.WriteFile(path, []byte("{invalid json}"), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadTemplateIndex(path)
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
		if !strings.Contains(err.Error(), "failed to parse") {
			t.Errorf("error should mention 'failed to parse', got: %v", err)
		}
	})
}

func TestLoadAnalysis(t *testing.T) {
	t.Run("valid analysis file", func(t *testing.T) {
		dir := t.TempDir()
		origDir, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(origDir) })
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}

		templateID := "tmpl1"
		slideNum := 3
		slideDir := filepath.Join(dir, "template", templateID, "3")
		if err := os.MkdirAll(slideDir, 0755); err != nil {
			t.Fatal(err)
		}

		analysis := model.SlideAnalysis{
			SlideNumber: slideNum,
			SlideID:     "slide3",
			Intention:   "Content slide",
			Description: "A content slide with text areas",
			EditableElements: []model.EditableElement{
				{ObjectID: "e1", Type: "text", Content: "Hello", Description: "Main title", Location: "top"},
			},
			VisualElements: []model.VisualElement{
				{Type: "icon", Description: "Arrow icon", Purpose: "decoration"},
			},
		}
		data, err := json.Marshal(analysis)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(slideDir, "analysis.json"), data, 0644); err != nil {
			t.Fatal(err)
		}

		got := LoadAnalysis(templateID, slideNum)
		if got == nil {
			t.Fatal("expected non-nil analysis")
		}
		if got.SlideNumber != slideNum {
			t.Errorf("SlideNumber = %d, want %d", got.SlideNumber, slideNum)
		}
		if got.Description != "A content slide with text areas" {
			t.Errorf("Description = %q, want %q", got.Description, "A content slide with text areas")
		}
		if len(got.EditableElements) != 1 {
			t.Fatalf("len(EditableElements) = %d, want 1", len(got.EditableElements))
		}
		if got.EditableElements[0].ObjectID != "e1" {
			t.Errorf("EditableElements[0].ObjectID = %q, want %q", got.EditableElements[0].ObjectID, "e1")
		}
		if len(got.VisualElements) != 1 {
			t.Fatalf("len(VisualElements) = %d, want 1", len(got.VisualElements))
		}
	})

	t.Run("missing file returns nil", func(t *testing.T) {
		dir := t.TempDir()
		origDir, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(origDir) })
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}

		got := LoadAnalysis("nonexistent", 99)
		if got != nil {
			t.Error("expected nil for missing file")
		}
	})

	t.Run("invalid JSON returns nil", func(t *testing.T) {
		dir := t.TempDir()
		origDir, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(origDir) })
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}

		slideDir := filepath.Join(dir, "template", "tmpl", "1")
		if err := os.MkdirAll(slideDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(slideDir, "analysis.json"), []byte("not json"), 0644); err != nil {
			t.Fatal(err)
		}

		got := LoadAnalysis("tmpl", 1)
		if got != nil {
			t.Error("expected nil for invalid JSON")
		}
	})
}

func TestEnrichPlan(t *testing.T) {
	// Helper to set up temp dir with analysis files and chdir into it.
	setupTempDir := func(t *testing.T, templateID string, analyses map[int]*model.SlideAnalysis) string {
		t.Helper()
		dir := t.TempDir()
		origDir, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(origDir) })
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}

		for slideNum, analysis := range analyses {
			slideDir := filepath.Join(dir, "template", templateID, fmt.Sprintf("%d", slideNum))
			if err := os.MkdirAll(slideDir, 0755); err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(analysis)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(slideDir, "analysis.json"), data, 0644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}

	t.Run("basic enrichment with modifications", func(t *testing.T) {
		templateID := "template123"
		analyses := map[int]*model.SlideAnalysis{
			1: {
				SlideNumber: 1,
				SlideID:     "s1",
				Intention:   "Title slide",
				Description: "A title slide for the presentation",
				EditableElements: []model.EditableElement{
					{ObjectID: "obj1", Type: "title", Content: "Placeholder", Description: "Main title", Location: "center"},
				},
				VisualElements: []model.VisualElement{
					{Type: "logo", Description: "Company logo", Purpose: "branding", Reusable: true},
				},
			},
		}
		setupTempDir(t, templateID, analyses)

		index := &model.TemplateIndex{
			TemplateID: templateID,
			Slides: []model.TemplateSlide{
				{
					SlideNumber: 1,
					SlideID:     "s1",
					Intention:   "Title slide",
					EditableFields: []model.EditableFieldSummary{
						{ObjectID: "obj1", VariableName: "titleShape", Role: "titre", Content: "Placeholder"},
					},
					VisualElements: []model.VisualElementSummary{
						{Type: "logo", Purpose: "branding"},
					},
				},
			},
		}

		genPlan := &model.GenerationPlan{
			PresentationTitle: "My Presentation",
			Slides: []model.SlideRequest{
				{
					SourceSlide: 1,
					Modifications: []model.TextModification{
						{VariableName: "titleShape", NewText: "New Title"},
					},
				},
			},
		}

		result := EnrichPlan(genPlan, index, templateID, "Create a presentation")

		if result.PresentationTitle != "My Presentation" {
			t.Errorf("PresentationTitle = %q, want %q", result.PresentationTitle, "My Presentation")
		}
		if result.TemplateID != templateID {
			t.Errorf("TemplateID = %q, want %q", result.TemplateID, templateID)
		}
		if result.SourceRequest != "Create a presentation" {
			t.Errorf("SourceRequest = %q, want %q", result.SourceRequest, "Create a presentation")
		}
		if result.GeneratedAt == "" {
			t.Error("GeneratedAt should not be empty")
		}
		if len(result.Slides) != 1 {
			t.Fatalf("len(Slides) = %d, want 1", len(result.Slides))
		}

		slide := result.Slides[0]
		if slide.Position != 1 {
			t.Errorf("Position = %d, want 1", slide.Position)
		}
		if slide.SourceSlideNumber != 1 {
			t.Errorf("SourceSlideNumber = %d, want 1", slide.SourceSlideNumber)
		}
		if slide.SourceSlideID != "s1" {
			t.Errorf("SourceSlideID = %q, want %q", slide.SourceSlideID, "s1")
		}
		if slide.Description != "A title slide for the presentation" {
			t.Errorf("Description = %q, want analysis description", slide.Description)
		}
		expectedPreview := "template/template123/1/slide.png"
		if slide.PreviewImage != expectedPreview {
			t.Errorf("PreviewImage = %q, want %q", slide.PreviewImage, expectedPreview)
		}

		if len(slide.EditableObjects) != 1 {
			t.Fatalf("len(EditableObjects) = %d, want 1", len(slide.EditableObjects))
		}
		obj := slide.EditableObjects[0]
		if obj.ObjectID != "obj1" {
			t.Errorf("ObjectID = %q, want %q", obj.ObjectID, "obj1")
		}
		if !obj.Modified {
			t.Error("object should be modified")
		}
		if obj.NewValue == nil || *obj.NewValue != "New Title" {
			t.Errorf("NewValue = %v, want %q", obj.NewValue, "New Title")
		}
		if obj.Description != "Main title" {
			t.Errorf("Description = %q, want %q (from analysis)", obj.Description, "Main title")
		}
		if obj.Location != "center" {
			t.Errorf("Location = %q, want %q (from analysis)", obj.Location, "center")
		}
		if obj.ElementType != "title" {
			t.Errorf("ElementType = %q, want %q (from analysis)", obj.ElementType, "title")
		}

		// Visual elements should come from analysis
		if len(slide.VisualObjects) != 1 {
			t.Fatalf("len(VisualObjects) = %d, want 1", len(slide.VisualObjects))
		}
		ve := slide.VisualObjects[0]
		if ve.Type != "logo" {
			t.Errorf("VisualObject.Type = %q, want %q", ve.Type, "logo")
		}
		if ve.Description != "Company logo" {
			t.Errorf("VisualObject.Description = %q, want %q", ve.Description, "Company logo")
		}
	})

	t.Run("unknown slide number skipped", func(t *testing.T) {
		templateID := "tmplSkip"
		setupTempDir(t, templateID, nil)

		index := &model.TemplateIndex{
			TemplateID: templateID,
			Slides: []model.TemplateSlide{
				{SlideNumber: 1, SlideID: "s1", Intention: "Title"},
			},
		}

		genPlan := &model.GenerationPlan{
			PresentationTitle: "Test",
			Slides: []model.SlideRequest{
				{SourceSlide: 99}, // does not exist in index
			},
		}

		result := EnrichPlan(genPlan, index, templateID, "req")
		if len(result.Slides) != 0 {
			t.Errorf("expected 0 slides for unknown source, got %d", len(result.Slides))
		}
	})

	t.Run("visual elements from template when no analysis", func(t *testing.T) {
		templateID := "tmplNoAnalysis"
		// Do NOT create analysis files
		dir := t.TempDir()
		origDir, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(origDir) })
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}

		objID := "vis1"
		index := &model.TemplateIndex{
			TemplateID: templateID,
			Slides: []model.TemplateSlide{
				{
					SlideNumber: 1,
					SlideID:     "s1",
					Intention:   "Content",
					VisualElements: []model.VisualElementSummary{
						{ObjectID: &objID, Type: "icon", Purpose: "decoration"},
					},
				},
			},
		}

		genPlan := &model.GenerationPlan{
			PresentationTitle: "Test",
			Slides: []model.SlideRequest{
				{SourceSlide: 1},
			},
		}

		result := EnrichPlan(genPlan, index, templateID, "req")
		if len(result.Slides) != 1 {
			t.Fatalf("expected 1 slide, got %d", len(result.Slides))
		}
		slide := result.Slides[0]
		if slide.Description != "" {
			t.Errorf("Description should be empty without analysis, got %q", slide.Description)
		}
		if len(slide.VisualObjects) != 1 {
			t.Fatalf("expected 1 visual object from template, got %d", len(slide.VisualObjects))
		}
		vo := slide.VisualObjects[0]
		if vo.Type != "icon" {
			t.Errorf("VisualObject.Type = %q, want %q", vo.Type, "icon")
		}
		if vo.Purpose != "decoration" {
			t.Errorf("VisualObject.Purpose = %q, want %q", vo.Purpose, "decoration")
		}
	})

	t.Run("visual elements from analysis take priority", func(t *testing.T) {
		templateID := "tmplPriority"
		analyses := map[int]*model.SlideAnalysis{
			1: {
				SlideNumber: 1,
				SlideID:     "s1",
				Description: "Analyzed",
				VisualElements: []model.VisualElement{
					{Type: "image", Description: "Photo", Purpose: "illustration", Reusable: true},
					{Type: "icon", Description: "Star", Purpose: "emphasis"},
				},
			},
		}
		setupTempDir(t, templateID, analyses)

		objID := "old"
		index := &model.TemplateIndex{
			TemplateID: templateID,
			Slides: []model.TemplateSlide{
				{
					SlideNumber: 1,
					SlideID:     "s1",
					Intention:   "Content",
					VisualElements: []model.VisualElementSummary{
						{ObjectID: &objID, Type: "stale", Purpose: "old"},
					},
				},
			},
		}

		genPlan := &model.GenerationPlan{
			PresentationTitle: "Test",
			Slides: []model.SlideRequest{
				{SourceSlide: 1},
			},
		}

		result := EnrichPlan(genPlan, index, templateID, "req")
		slide := result.Slides[0]
		// Should have 2 visual objects from analysis, not 1 from template
		if len(slide.VisualObjects) != 2 {
			t.Fatalf("expected 2 visual objects from analysis, got %d", len(slide.VisualObjects))
		}
		if slide.VisualObjects[0].Type != "image" {
			t.Errorf("first visual should be from analysis (image), got %q", slide.VisualObjects[0].Type)
		}
		if slide.VisualObjects[1].Type != "icon" {
			t.Errorf("second visual should be from analysis (icon), got %q", slide.VisualObjects[1].Type)
		}
	})

	t.Run("rawContent takes priority over content for currentValue", func(t *testing.T) {
		templateID := "tmplRaw"
		setupTempDir(t, templateID, nil)

		index := &model.TemplateIndex{
			TemplateID: templateID,
			Slides: []model.TemplateSlide{
				{
					SlideNumber: 1,
					SlideID:     "s1",
					Intention:   "Test",
					EditableFields: []model.EditableFieldSummary{
						{
							ObjectID:     "obj1",
							VariableName: "v1",
							Role:         "titre",
							Content:      "cleaned",
							RawContent:   "raw value",
						},
					},
				},
			},
		}

		genPlan := &model.GenerationPlan{
			PresentationTitle: "Test",
			Slides: []model.SlideRequest{
				{SourceSlide: 1},
			},
		}

		result := EnrichPlan(genPlan, index, templateID, "req")
		obj := result.Slides[0].EditableObjects[0]
		if obj.CurrentValue != "raw value" {
			t.Errorf("CurrentValue = %q, want %q (rawContent should take priority)", obj.CurrentValue, "raw value")
		}
	})

	t.Run("content used when rawContent is empty", func(t *testing.T) {
		templateID := "tmplContent"
		setupTempDir(t, templateID, nil)

		index := &model.TemplateIndex{
			TemplateID: templateID,
			Slides: []model.TemplateSlide{
				{
					SlideNumber: 1,
					SlideID:     "s1",
					Intention:   "Test",
					EditableFields: []model.EditableFieldSummary{
						{
							ObjectID:     "obj1",
							VariableName: "v1",
							Role:         "titre",
							Content:      "content value",
							RawContent:   "",
						},
					},
				},
			},
		}

		genPlan := &model.GenerationPlan{
			PresentationTitle: "Test",
			Slides: []model.SlideRequest{
				{SourceSlide: 1},
			},
		}

		result := EnrichPlan(genPlan, index, templateID, "req")
		obj := result.Slides[0].EditableObjects[0]
		if obj.CurrentValue != "content value" {
			t.Errorf("CurrentValue = %q, want %q", obj.CurrentValue, "content value")
		}
	})

	t.Run("multiple slides with positions", func(t *testing.T) {
		templateID := "tmplMulti"
		setupTempDir(t, templateID, nil)

		index := &model.TemplateIndex{
			TemplateID: templateID,
			Slides: []model.TemplateSlide{
				{SlideNumber: 1, SlideID: "s1", Intention: "Title"},
				{SlideNumber: 2, SlideID: "s2", Intention: "Content"},
				{SlideNumber: 3, SlideID: "s3", Intention: "End"},
			},
		}

		genPlan := &model.GenerationPlan{
			PresentationTitle: "Multi",
			Slides: []model.SlideRequest{
				{SourceSlide: 3},
				{SourceSlide: 1},
				{SourceSlide: 2},
			},
		}

		result := EnrichPlan(genPlan, index, templateID, "req")
		if len(result.Slides) != 3 {
			t.Fatalf("expected 3 slides, got %d", len(result.Slides))
		}
		// Positions should be sequential 1, 2, 3
		for i, slide := range result.Slides {
			expectedPos := i + 1
			if slide.Position != expectedPos {
				t.Errorf("Slides[%d].Position = %d, want %d", i, slide.Position, expectedPos)
			}
		}
		// Source slides should match request order
		if result.Slides[0].SourceSlideNumber != 3 {
			t.Errorf("first slide source = %d, want 3", result.Slides[0].SourceSlideNumber)
		}
		if result.Slides[1].SourceSlideNumber != 1 {
			t.Errorf("second slide source = %d, want 1", result.Slides[1].SourceSlideNumber)
		}
		if result.Slides[2].SourceSlideNumber != 2 {
			t.Errorf("third slide source = %d, want 2", result.Slides[2].SourceSlideNumber)
		}
	})

	t.Run("cell location preserved", func(t *testing.T) {
		templateID := "tmplCell"
		setupTempDir(t, templateID, nil)

		cellLoc := &model.CellLocation{RowIndex: 2, ColumnIndex: 3}
		placeholder := "Enter text"
		index := &model.TemplateIndex{
			TemplateID: templateID,
			Slides: []model.TemplateSlide{
				{
					SlideNumber: 1,
					SlideID:     "s1",
					Intention:   "Table",
					EditableFields: []model.EditableFieldSummary{
						{
							ObjectID:     "tbl1",
							VariableName: "cellShape",
							Role:         "contenu",
							Placeholder:  &placeholder,
							CellLocation: cellLoc,
						},
					},
				},
			},
		}

		genPlan := &model.GenerationPlan{
			PresentationTitle: "Test",
			Slides: []model.SlideRequest{
				{SourceSlide: 1},
			},
		}

		result := EnrichPlan(genPlan, index, templateID, "req")
		obj := result.Slides[0].EditableObjects[0]
		if obj.CellLocation == nil {
			t.Fatal("CellLocation should not be nil")
		}
		if obj.CellLocation.RowIndex != 2 || obj.CellLocation.ColumnIndex != 3 {
			t.Errorf("CellLocation = {%d,%d}, want {2,3}", obj.CellLocation.RowIndex, obj.CellLocation.ColumnIndex)
		}
		if obj.Placeholder == nil || *obj.Placeholder != "Enter text" {
			t.Errorf("Placeholder = %v, want %q", obj.Placeholder, "Enter text")
		}
	})

	t.Run("deduplication applied within enriched slide", func(t *testing.T) {
		templateID := "tmplDedup"
		setupTempDir(t, templateID, nil)

		index := &model.TemplateIndex{
			TemplateID: templateID,
			Slides: []model.TemplateSlide{
				{
					SlideNumber: 1,
					SlideID:     "s1",
					Intention:   "Test",
					EditableFields: []model.EditableFieldSummary{
						{ObjectID: "o1", VariableName: "v1", Role: "titre"},
						{ObjectID: "o2", VariableName: "v2", Role: "sous_titre"},
					},
				},
			},
		}

		genPlan := &model.GenerationPlan{
			PresentationTitle: "Test",
			Slides: []model.SlideRequest{
				{
					SourceSlide: 1,
					Modifications: []model.TextModification{
						{VariableName: "v1", NewText: "Same Long Text"},
						{VariableName: "v2", NewText: "Same Long Text"},
					},
				},
			},
		}

		result := EnrichPlan(genPlan, index, templateID, "req")
		slide := result.Slides[0]
		// First should be kept
		if !slide.EditableObjects[0].Modified {
			t.Error("first object should remain modified")
		}
		// Second should be deduplicated
		if slide.EditableObjects[1].Modified {
			t.Error("second object should be deduplicated (Modified=false)")
		}
		if slide.EditableObjects[1].NewValue != nil {
			t.Error("second object should have NewValue=nil after dedup")
		}
	})
}

// fmt is needed by setupTempDir helper in TestEnrichPlan.
var _ = fmt.Sprintf
