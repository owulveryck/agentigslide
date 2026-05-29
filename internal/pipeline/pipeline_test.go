package pipeline

import (
	"encoding/json"
	"testing"

	"github.com/owulveryck/agentigslide/internal/model"
)

func TestBuildAmendPrompt(t *testing.T) {
	data := AmendPromptData{
		ExistingPlan:     `{"presentationTitle":"Test"}`,
		TemplateIndex:    `[{"id":"1"}]`,
		AmendmentRequest: "Add a conclusion slide",
	}
	result, err := BuildAmendPrompt(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !contains(result, "Add a conclusion slide") {
		t.Error("result should contain the amendment request")
	}
	if !contains(result, `{"presentationTitle":"Test"}`) {
		t.Error("result should contain the existing plan")
	}
}

func TestBuildAmendPromptCustom_InvalidTemplate(t *testing.T) {
	_, err := BuildAmendPromptCustom("no fields here", AmendPromptData{})
	if err == nil {
		t.Fatal("expected error for template missing required fields")
	}
}

func TestPlanToGenerationSummary(t *testing.T) {
	newText := "Hello World"
	plan := &model.PresentationPlan{
		PresentationTitle: "My Deck",
		Slides: []model.SlideSpec{
			{
				SourceSlideNumber: 3,
				EditableObjects: []model.EditableObject{
					{VariableName: "titleMain", Modified: true, NewValue: &newText},
					{VariableName: "subtitle", Modified: false},
				},
			},
		},
	}

	result := PlanToGenerationSummary(plan)
	if result == "" {
		t.Fatal("expected non-empty result")
	}

	var parsed model.GenerationPlan
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if parsed.PresentationTitle != "My Deck" {
		t.Errorf("title = %q, want %q", parsed.PresentationTitle, "My Deck")
	}
	if len(parsed.Slides) != 1 {
		t.Fatalf("slides = %d, want 1", len(parsed.Slides))
	}
	if len(parsed.Slides[0].Modifications) != 1 {
		t.Fatalf("modifications = %d, want 1 (unmodified should be excluded)", len(parsed.Slides[0].Modifications))
	}
	if parsed.Slides[0].Modifications[0].NewText != "Hello World" {
		t.Errorf("NewText = %q, want %q", parsed.Slides[0].Modifications[0].NewText, "Hello World")
	}
}

func TestPlanToGenerationSummary_Empty(t *testing.T) {
	plan := &model.PresentationPlan{PresentationTitle: "Empty"}
	result := PlanToGenerationSummary(plan)

	var parsed model.GenerationPlan
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if parsed.PresentationTitle != "Empty" {
		t.Errorf("title = %q, want %q", parsed.PresentationTitle, "Empty")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
