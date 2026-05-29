package writer

import (
	"context"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	agentpkg "github.com/owulveryck/agentigslide/internal/agent"
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
	if card.Name != "Writer" {
		t.Errorf("name = %q, want Writer", card.Name)
	}
	if len(card.Skills) == 0 {
		t.Error("expected at least one skill")
	}
}

func TestExtractDataInput_Writer(t *testing.T) {
	t.Run("nil message", func(t *testing.T) {
		_, err := agentpkg.ExtractDataInput((*a2a.Message)(nil), func(w *writerInput) bool {
			return len(w.TemplateFields) > 0
		})
		if err == nil {
			t.Error("expected error for nil message")
		}
	})

	t.Run("valid data part", func(t *testing.T) {
		input := map[string]any{
			"sourceSlide": 42,
			"slideNeed": map[string]any{
				"intent":    "test intent",
				"slideType": "content",
			},
			"templateFields": []any{
				map[string]any{
					"variableName": "title",
					"role":         "titre",
					"maxChars":     100,
				},
			},
		}
		msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewDataPart(input))
		result, err := agentpkg.ExtractDataInput(msg, func(w *writerInput) bool {
			return len(w.TemplateFields) > 0
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.SourceSlide != 42 {
			t.Errorf("sourceSlide = %d, want 42", result.SourceSlide)
		}
		if len(result.TemplateFields) != 1 {
			t.Fatalf("expected 1 template field, got %d", len(result.TemplateFields))
		}
		if result.TemplateFields[0].VariableName != "title" {
			t.Errorf("field name = %q, want title", result.TemplateFields[0].VariableName)
		}
	})

	t.Run("text part only fails", func(t *testing.T) {
		msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("just text"))
		_, err := agentpkg.ExtractDataInput(msg, func(w *writerInput) bool {
			return len(w.TemplateFields) > 0
		})
		if err == nil {
			t.Error("expected error when only text parts")
		}
	})
}
