package metrics

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendAndLoadHistory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")

	c := NewCollector()
	c.Record(AgentCall{
		Agent: "outliner", Model: "claude-sonnet-4-6",
		InputTokens: 1000, OutputTokens: 200,
		CacheReadInputTokens: 800,
		Duration:             2 * time.Second,
	})
	c.Record(AgentCall{
		Agent: "writer", Model: "claude-haiku-4-5",
		InputTokens: 500, OutputTokens: 100,
		Duration: 1 * time.Second,
	})
	c.SetSlidesGenerated(5)
	c.SetPipelineDuration(10 * time.Second)

	s := c.Summary()

	if err := AppendHistoryTo(path, s, "generate"); err != nil {
		t.Fatal(err)
	}
	if err := AppendHistoryTo(path, s, "edit"); err != nil {
		t.Fatal(err)
	}

	records, err := LoadHistoryFrom(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	r := records[0]
	if r.Mode != "generate" {
		t.Errorf("mode: got %q, want generate", r.Mode)
	}
	if r.SlidesGenerated != 5 {
		t.Errorf("slides: got %d, want 5", r.SlidesGenerated)
	}
	if r.TotalCalls != 2 {
		t.Errorf("calls: got %d, want 2", r.TotalCalls)
	}
	if r.DurationSecs != 10 {
		t.Errorf("duration: got %f, want 10", r.DurationSecs)
	}
	if r.CacheHitRate == 0 {
		t.Error("cache hit rate should be > 0")
	}
	if len(r.AgentRows) != 2 {
		t.Errorf("agent rows: got %d, want 2", len(r.AgentRows))
	}
	if r.AgentRows[0].DurationMs != 2000 {
		t.Errorf("outliner duration: got %d ms, want 2000", r.AgentRows[0].DurationMs)
	}

	if records[1].Mode != "edit" {
		t.Errorf("second record mode: got %q, want edit", records[1].Mode)
	}
}

func TestLoadHistoryMissingFile(t *testing.T) {
	records, err := LoadHistoryFrom(filepath.Join(t.TempDir(), "nonexistent.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("expected 0 records, got %d", len(records))
	}
}

func TestLoadHistoryMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.jsonl")
	if err := os.WriteFile(path, []byte("not json\n{\"mode\":\"test\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	records, err := LoadHistoryFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 valid record, got %d", len(records))
	}
	if records[0].Mode != "test" {
		t.Errorf("mode: got %q, want test", records[0].Mode)
	}
}

func TestPrintHistoryEmpty(t *testing.T) {
	var buf bytes.Buffer
	// Override by calling PrintHistory which uses LoadHistory (default path).
	// Since we can't override the default path in PrintHistory, test the
	// formatting by constructing records directly via PrintHistoryFrom.
	// Instead, just verify no panic with the public function when no file exists.
	_ = PrintHistory(&buf, 10)
}

func TestPrintHistoryTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")

	c1 := NewCollector()
	c1.Record(AgentCall{Agent: "outliner", Model: "claude-sonnet-4-6", InputTokens: 1000, OutputTokens: 200})
	c1.SetSlidesGenerated(5)
	c1.SetPipelineDuration(10 * time.Second)
	if err := AppendHistoryTo(path, c1.Summary(), "generate"); err != nil {
		t.Fatal(err)
	}

	c2 := NewCollector()
	c2.Record(AgentCall{Agent: "outliner", Model: "claude-sonnet-4-6", InputTokens: 2000, OutputTokens: 400})
	c2.SetSlidesGenerated(10)
	c2.SetPipelineDuration(20 * time.Second)
	if err := AppendHistoryTo(path, c2.Summary(), "generate"); err != nil {
		t.Fatal(err)
	}

	records, err := LoadHistoryFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	var buf bytes.Buffer
	printHistoryRecords(&buf, records, 10)

	output := buf.String()
	for _, expected := range []string{
		"COST HISTORY",
		"generate",
		"$/SLIDE",
		"Δ COST",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("output missing %q:\n%s", expected, output)
		}
	}
}
