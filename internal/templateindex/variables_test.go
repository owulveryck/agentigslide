package templateindex

import (
	"testing"

	"github.com/owulveryck/agentigslide/internal/model"
)

func TestToCamelCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"title_main", "titleMain"},
		{"body-text", "bodyText"},
		{"TITLE MAIN", "titleMain"},
		{"simple", "simple"},
		{"a_b_c", "aBC"},
		{"already", "already"},
		{"", ""},
		{"titleMain", "titlemain"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ToCamelCase(tt.input)
			if got != tt.want {
				t.Errorf("ToCamelCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDeduplicateVariableNames(t *testing.T) {
	fields := []model.EditableFieldSummary{
		{VariableName: "textShape"},
		{VariableName: "titleShape"},
		{VariableName: "textShape"},
		{VariableName: "textShape"},
	}

	DeduplicateVariableNames(fields)

	expected := []string{"textShape", "titleShape", "text2Shape", "text3Shape"}
	for i, want := range expected {
		if fields[i].VariableName != want {
			t.Errorf("fields[%d].VariableName = %q, want %q", i, fields[i].VariableName, want)
		}
	}
}

func TestDeduplicateVariableNames_NoDuplicates(t *testing.T) {
	fields := []model.EditableFieldSummary{
		{VariableName: "titleShape"},
		{VariableName: "bodyShape"},
		{VariableName: "yearShape"},
	}

	DeduplicateVariableNames(fields)

	expected := []string{"titleShape", "bodyShape", "yearShape"}
	for i, want := range expected {
		if fields[i].VariableName != want {
			t.Errorf("fields[%d].VariableName = %q, want %q", i, fields[i].VariableName, want)
		}
	}
}

func TestGenerateVariableName_SingleRole(t *testing.T) {
	elem := model.EditableElement{
		ObjectID:    "obj1",
		Description: "titre principal de la slide",
	}
	analysis := &model.SlideAnalysis{
		EditableElements: []model.EditableElement{elem},
	}

	got := GenerateVariableName(elem, nil, analysis)
	if got != "titlemainShape" {
		t.Errorf("GenerateVariableName() = %q, want %q", got, "titlemainShape")
	}
}
