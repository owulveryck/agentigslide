package selector

import (
	"context"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

var _ a2asrv.AgentExecutor = (*Agent)(nil)

func TestExecute_InvalidInput(t *testing.T) {
	ag := &Agent{model: "test-model"}
	execCtx := &a2asrv.ExecutorContext{
		TaskID:    "test-task-1",
		ContextID: "test-ctx-1",
		Message:   a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("not json")),
	}

	var events []a2a.Event
	for event, err := range ag.Execute(context.Background(), execCtx) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		events = append(events, event)
	}

	lastEvent := events[len(events)-1]
	statusEvent, ok := lastEvent.(*a2a.TaskStatusUpdateEvent)
	if !ok {
		t.Fatalf("last event should be TaskStatusUpdateEvent, got %T", lastEvent)
	}
	if statusEvent.Status.State != a2a.TaskStateFailed {
		t.Errorf("expected failed state, got %s", statusEvent.Status.State)
	}
}

func TestCancel(t *testing.T) {
	ag := &Agent{model: "test-model"}
	execCtx := &a2asrv.ExecutorContext{
		TaskID:    "test-task-1",
		ContextID: "test-ctx-1",
	}

	var events []a2a.Event
	for event, err := range ag.Cancel(context.Background(), execCtx) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		events = append(events, event)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	statusEvent := events[0].(*a2a.TaskStatusUpdateEvent)
	if statusEvent.Status.State != a2a.TaskStateCanceled {
		t.Errorf("expected canceled state, got %s", statusEvent.Status.State)
	}
}

func TestCard(t *testing.T) {
	card := Card()
	if card.Name != "Selector" {
		t.Errorf("name = %q, want Selector", card.Name)
	}
	if len(card.Skills) == 0 {
		t.Error("expected at least one skill")
	}
}

func TestExtractSelectorInput(t *testing.T) {
	t.Run("nil message", func(t *testing.T) {
		_, err := extractSelectorInput(nil)
		if err == nil {
			t.Error("expected error for nil message")
		}
	})

	t.Run("valid data part", func(t *testing.T) {
		input := map[string]any{
			"outline": map[string]any{
				"presentationTitle": "Test",
				"sections":         []any{},
			},
			"compactCatalog": "SLIDE 1 [cover]",
		}
		msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewDataPart(input))
		result, err := extractSelectorInput(msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Outline == nil {
			t.Error("outline should not be nil")
		}
		if result.Outline.PresentationTitle != "Test" {
			t.Errorf("title = %q, want Test", result.Outline.PresentationTitle)
		}
		if result.CompactCatalog != "SLIDE 1 [cover]" {
			t.Errorf("catalog = %q", result.CompactCatalog)
		}
	})

	t.Run("text part only fails", func(t *testing.T) {
		msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("just text"))
		_, err := extractSelectorInput(msg)
		if err == nil {
			t.Error("expected error when only text parts")
		}
	})
}
