package reviewer

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
	if card.Name != "Reviewer" {
		t.Errorf("name = %q, want Reviewer", card.Name)
	}
	if len(card.Skills) == 0 {
		t.Error("expected at least one skill")
	}
}

func TestExtractReviewerInput(t *testing.T) {
	t.Run("nil message", func(t *testing.T) {
		_, err := extractReviewerInput(nil)
		if err == nil {
			t.Error("expected error for nil message")
		}
	})

	t.Run("valid data part", func(t *testing.T) {
		input := map[string]any{
			"plan": map[string]any{
				"presentationTitle": "Test",
				"slides":            []any{},
			},
			"userRequest":    "Create a presentation about AI",
			"compactCatalog": "SLIDE 1 [cover]",
		}
		msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewDataPart(input))
		result, err := extractReviewerInput(msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Plan == nil {
			t.Error("plan should not be nil")
		}
		if result.UserRequest != "Create a presentation about AI" {
			t.Errorf("userRequest = %q", result.UserRequest)
		}
	})

	t.Run("text part only fails", func(t *testing.T) {
		msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("just text"))
		_, err := extractReviewerInput(msg)
		if err == nil {
			t.Error("expected error when only text parts")
		}
	})
}
