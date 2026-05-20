package revision

import (
	"testing"

	"google.golang.org/api/slides/v1"
)

func TestLogRecord(t *testing.T) {
	l := New("test-pres-id")

	resp := &slides.BatchUpdatePresentationResponse{
		WriteControl: &slides.WriteControl{
			RequiredRevisionId: "rev-abc-123",
		},
	}

	l.Record("duplicate_slides", resp, 5)

	if len(l.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(l.Entries))
	}

	e := l.Entries[0]
	if e.Step != "duplicate_slides" {
		t.Errorf("expected step 'duplicate_slides', got %q", e.Step)
	}
	if e.RevisionID != "rev-abc-123" {
		t.Errorf("expected revisionID 'rev-abc-123', got %q", e.RevisionID)
	}
	if e.RequestCount != 5 {
		t.Errorf("expected requestCount 5, got %d", e.RequestCount)
	}
}

func TestLogRecordNilWriteControl(t *testing.T) {
	l := New("test-pres-id")

	l.Record("some_step", &slides.BatchUpdatePresentationResponse{}, 3)

	if l.Entries[0].RevisionID != "" {
		t.Errorf("expected empty revisionID for nil WriteControl, got %q", l.Entries[0].RevisionID)
	}
}

func TestLogLatestRevisionID(t *testing.T) {
	l := New("test-pres-id")

	if got := l.LatestRevisionID(); got != "" {
		t.Errorf("expected empty string for empty log, got %q", got)
	}

	l.Record("step1", &slides.BatchUpdatePresentationResponse{
		WriteControl: &slides.WriteControl{RequiredRevisionId: "rev-1"},
	}, 1)
	l.Record("step2", &slides.BatchUpdatePresentationResponse{
		WriteControl: &slides.WriteControl{RequiredRevisionId: "rev-2"},
	}, 2)

	if got := l.LatestRevisionID(); got != "rev-2" {
		t.Errorf("expected 'rev-2', got %q", got)
	}
}

func TestLogSummary(t *testing.T) {
	l := New("pres-123")
	if s := l.Summary(); s != "no revisions recorded" {
		t.Errorf("expected 'no revisions recorded', got %q", s)
	}

	l.Record("step1", &slides.BatchUpdatePresentationResponse{
		WriteControl: &slides.WriteControl{RequiredRevisionId: "rev-1"},
	}, 3)

	s := l.Summary()
	if s == "no revisions recorded" {
		t.Error("expected non-empty summary after recording")
	}
}
