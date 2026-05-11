package metrics

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCollectorRecord(t *testing.T) {
	c := NewCollector()
	c.Record(AgentCall{Agent: "outliner", Model: "claude-sonnet-4-6", InputTokens: 100, OutputTokens: 50})
	c.Record(AgentCall{Agent: "writer", Model: "claude-haiku-4-5", InputTokens: 200, OutputTokens: 80})

	s := c.Summary()
	if len(s.AgentRows) != 2 {
		t.Fatalf("expected 2 agent rows, got %d", len(s.AgentRows))
	}
	if s.GrandTotal.Calls != 2 {
		t.Fatalf("expected 2 total calls, got %d", s.GrandTotal.Calls)
	}
	if s.GrandTotal.InputTokens != 300 {
		t.Fatalf("expected 300 input tokens, got %d", s.GrandTotal.InputTokens)
	}
}

func TestCollectorConcurrency(t *testing.T) {
	c := NewCollector()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			c.Record(AgentCall{
				Agent:       "writer",
				Model:       "claude-sonnet-4-6",
				InputTokens: n,
			})
		}(i)
	}
	wg.Wait()

	s := c.Summary()
	if s.GrandTotal.Calls != 100 {
		t.Fatalf("expected 100 calls, got %d", s.GrandTotal.Calls)
	}
}

func TestSummaryAggregation(t *testing.T) {
	c := NewCollector()
	c.Record(AgentCall{Agent: "writer", Model: "claude-sonnet-4-6", InputTokens: 100, OutputTokens: 50})
	c.Record(AgentCall{Agent: "writer", Model: "claude-sonnet-4-6", InputTokens: 200, OutputTokens: 80})
	c.Record(AgentCall{Agent: "writer", Model: "claude-haiku-4-5", InputTokens: 50, OutputTokens: 20})
	c.Record(AgentCall{Agent: "reviewer", Model: "claude-opus-4-6", InputTokens: 500, OutputTokens: 100})

	s := c.Summary()

	if len(s.AgentRows) != 3 {
		t.Fatalf("expected 3 agent rows (writer-sonnet, writer-haiku, reviewer-opus), got %d", len(s.AgentRows))
	}

	writerSonnet := s.AgentRows[0]
	if writerSonnet.Agent != "writer" || writerSonnet.Calls != 2 || writerSonnet.InputTokens != 300 {
		t.Errorf("writer-sonnet: got %+v", writerSonnet)
	}

	writerHaiku := s.AgentRows[1]
	if writerHaiku.Agent != "writer" || writerHaiku.Calls != 1 || writerHaiku.InputTokens != 50 {
		t.Errorf("writer-haiku: got %+v", writerHaiku)
	}

	reviewer := s.AgentRows[2]
	if reviewer.Agent != "reviewer" || reviewer.Calls != 1 || reviewer.InputTokens != 500 {
		t.Errorf("reviewer: got %+v", reviewer)
	}

	if s.GrandTotal.Calls != 4 || s.GrandTotal.InputTokens != 850 {
		t.Errorf("grand total: got %+v", s.GrandTotal)
	}
}

func TestSummaryEmpty(t *testing.T) {
	c := NewCollector()
	s := c.Summary()
	if len(s.AgentRows) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(s.AgentRows))
	}
	if s.GrandTotal.Calls != 0 {
		t.Fatalf("expected 0 calls, got %d", s.GrandTotal.Calls)
	}
}

func TestSummaryMetadata(t *testing.T) {
	c := NewCollector()
	c.SetSelectorRetries(1)
	c.SetReviewerRetries(2)
	c.SetSlidesGenerated(12)
	c.SetPipelineDuration(42 * time.Second)

	s := c.Summary()
	if s.SelectorRetries != 1 {
		t.Errorf("selector retries: got %d, want 1", s.SelectorRetries)
	}
	if s.ReviewerRetries != 2 {
		t.Errorf("reviewer retries: got %d, want 2", s.ReviewerRetries)
	}
	if s.SlidesGenerated != 12 {
		t.Errorf("slides generated: got %d, want 12", s.SlidesGenerated)
	}
	if s.PipelineDuration != 42*time.Second {
		t.Errorf("duration: got %v, want 42s", s.PipelineDuration)
	}
}

func TestPrintTable(t *testing.T) {
	c := NewCollector()
	c.Record(AgentCall{Agent: "outliner", Model: "claude-sonnet-4-6", InputTokens: 12340, OutputTokens: 2100, CacheReadInputTokens: 8900, CacheCreationInputTokens: 3400})
	c.Record(AgentCall{Agent: "writer", Model: "claude-haiku-4-5", InputTokens: 4500, OutputTokens: 1200, CacheReadInputTokens: 3000})
	c.SetSlidesGenerated(5)
	c.SetPipelineDuration(10 * time.Second)

	var buf bytes.Buffer
	PrintTable(&buf, c.Summary())

	output := buf.String()

	for _, expected := range []string{
		"PIPELINE EXECUTION SUMMARY",
		"outliner",
		"writer",
		"TOTAL",
		"Slides generated:",
		"Pipeline duration:",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("output missing %q", expected)
		}
	}
}

func TestLookupPricing(t *testing.T) {
	tests := []struct {
		model   string
		wantInp float64
	}{
		{"claude-opus-4-6", 15.0},
		{"claude-sonnet-4-6", 3.0},
		{"claude-haiku-4-5@20251001", 0.80},
		{"unknown-model", 0},
	}
	for _, tt := range tests {
		p := LookupPricing(tt.model)
		if p.InputPerMTok != tt.wantInp {
			t.Errorf("LookupPricing(%q).InputPerMTok = %f, want %f", tt.model, p.InputPerMTok, tt.wantInp)
		}
	}
}

func TestEstimateRowCost(t *testing.T) {
	row := &AgentRow{
		Model:                    "claude-sonnet-4-6",
		InputTokens:              1_000_000,
		OutputTokens:             1_000_000,
		CacheReadInputTokens:     0,
		CacheCreationInputTokens: 0,
	}
	cost := EstimateRowCost(row)
	expected := 3.0 + 15.0
	if cost != expected {
		t.Errorf("cost = %f, want %f", cost, expected)
	}
}
