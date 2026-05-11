package input

import (
	"os"
	"testing"
)

func TestProcessEditorOutputNoChange(t *testing.T) {
	original := "Section 1: Intro\n  [1] Title slide"
	edited := editorHeader + original
	got := processEditorOutput(original, edited)
	if got != "" {
		t.Errorf("expected empty (no change), got %q", got)
	}
}

func TestProcessEditorOutputWithChange(t *testing.T) {
	original := "Section 1: Intro\n  [1] Title slide"
	edited := editorHeader + "Section 1: Introduction\n  [1] Cover slide"
	got := processEditorOutput(original, edited)
	if got == "" {
		t.Error("expected non-empty feedback for changed content")
	}
	if got != "Section 1: Introduction\n  [1] Cover slide" {
		t.Errorf("unexpected result: %q", got)
	}
}

func TestProcessEditorOutputStripComments(t *testing.T) {
	original := "Section 1: Intro"
	edited := "% comment\nSection 1: Updated\n% another comment"
	got := processEditorOutput(original, edited)
	if got != "Section 1: Updated" {
		t.Errorf("expected comments stripped, got %q", got)
	}
}

func TestExpandFileRefs(t *testing.T) {
	path := t.TempDir() + "/test.txt"
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := ExpandFileRefs("see @" + path + " for details")
	expected := "see hello world for details"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestExpandFileRefsMissing(t *testing.T) {
	result := ExpandFileRefs("see @/nonexistent/file.txt here")
	if result != "see @/nonexistent/file.txt here" {
		t.Errorf("expected unchanged for missing file, got %q", result)
	}
}
