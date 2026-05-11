package input

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileCompleterNoAt(t *testing.T) {
	fc := &fileCompleter{}
	line := []rune("hello world")
	candidates, length := fc.Do(line, len(line))
	if len(candidates) != 0 {
		t.Errorf("expected no candidates without @, got %d", len(candidates))
	}
	if length != 0 {
		t.Errorf("expected length 0, got %d", length)
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Fatal(err)
		}
	})
}

func TestFileCompleterAtCwd(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file2.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	chdir(t, dir)

	fc := &fileCompleter{}

	// Complete @f -> should match file1.txt, file2.md
	line := []rune("@f")
	candidates, length := fc.Do(line, len(line))
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates for @f, got %d", len(candidates))
	}
	if length != 1 { // "f" is the partial
		t.Errorf("expected length 1, got %d", length)
	}
}

func TestFileCompleterSubdir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "docs")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "readme.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	chdir(t, dir)

	fc := &fileCompleter{}

	line := []rune("@docs/r")
	candidates, length := fc.Do(line, len(line))
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate for @docs/r, got %d", len(candidates))
	}
	if length != 6 { // "docs/r"
		t.Errorf("expected length 6, got %d", length)
	}
	if string(candidates[0]) != "eadme.md" {
		t.Errorf("expected suffix 'eadme.md', got %q", string(candidates[0]))
	}
}

func TestFileCompleterDirSuffix(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "mydir"), 0o755); err != nil {
		t.Fatal(err)
	}

	chdir(t, dir)

	fc := &fileCompleter{}
	line := []rune("@my")
	candidates, length := fc.Do(line, len(line))
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if length != 2 {
		t.Errorf("expected length 2, got %d", length)
	}
	got := string(candidates[0])
	if got != "dir/" {
		t.Errorf("expected 'dir/' suffix for directory, got %q", got)
	}
}

func TestFileCompleterMidLine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	chdir(t, dir)

	fc := &fileCompleter{}
	line := []rune("hello @no")
	candidates, length := fc.Do(line, len(line))
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if length != 2 { // "no"
		t.Errorf("expected length 2, got %d", length)
	}
}
