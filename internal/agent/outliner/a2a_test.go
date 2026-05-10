package outliner

import (
	"context"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// Compile-time check that Agent satisfies AgentExecutor.
var _ a2asrv.AgentExecutor = (*Agent)(nil)

func TestExecute_EmptyMessage(t *testing.T) {
	ag := &Agent{model: "test-model"}
	execCtx := &a2asrv.ExecutorContext{
		TaskID:    "test-task-1",
		ContextID: "test-ctx-1",
		Message:   a2a.NewMessage(a2a.MessageRoleUser),
	}

	var events []a2a.Event
	for event, err := range ag.Execute(context.Background(), execCtx) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		events = append(events, event)
	}

	if len(events) < 2 {
		t.Fatalf("expected at least 2 events (submitted + failed), got %d", len(events))
	}

	if _, ok := events[0].(*a2a.Task); !ok {
		t.Errorf("first event should be Task (submitted), got %T", events[0])
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

	statusEvent, ok := events[0].(*a2a.TaskStatusUpdateEvent)
	if !ok {
		t.Fatalf("expected TaskStatusUpdateEvent, got %T", events[0])
	}
	if statusEvent.Status.State != a2a.TaskStateCanceled {
		t.Errorf("expected canceled state, got %s", statusEvent.Status.State)
	}
}

func TestExtractTextFromMessage(t *testing.T) {
	t.Run("nil message", func(t *testing.T) {
		if got := extractTextFromMessage(nil); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("single text part", func(t *testing.T) {
		msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
		if got := extractTextFromMessage(msg); got != "hello" {
			t.Errorf("expected %q, got %q", "hello", got)
		}
	})

	t.Run("multiple text parts joined", func(t *testing.T) {
		msg := a2a.NewMessage(a2a.MessageRoleUser,
			a2a.NewTextPart("line1"),
			a2a.NewTextPart("line2"),
		)
		if got := extractTextFromMessage(msg); got != "line1\nline2" {
			t.Errorf("expected %q, got %q", "line1\nline2", got)
		}
	})

	t.Run("non-text parts ignored", func(t *testing.T) {
		msg := a2a.NewMessage(a2a.MessageRoleUser,
			a2a.NewDataPart(map[string]string{"key": "val"}),
			a2a.NewTextPart("text"),
		)
		if got := extractTextFromMessage(msg); got != "text" {
			t.Errorf("expected %q, got %q", "text", got)
		}
	})
}

func TestCard(t *testing.T) {
	card := Card()
	if card.Name != "Outliner" {
		t.Errorf("name = %q, want Outliner", card.Name)
	}
	if card.Description == "" {
		t.Error("description should not be empty")
	}
	if len(card.Skills) == 0 {
		t.Error("expected at least one skill")
	}
	if card.Provider == nil {
		t.Error("provider should not be nil")
	}
}
