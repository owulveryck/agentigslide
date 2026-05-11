package selector

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	agentpkg "github.com/owulveryck/agentigslide/internal/agent"
)

var _ a2asrv.AgentExecutor = (*Agent)(nil)

type selectorInput struct {
	Outline        *agentpkg.PresentationOutline `json:"outline"`
	CompactCatalog string                        `json:"compactCatalog"`
}

// Execute implements a2asrv.AgentExecutor. It expects a JSON data part
// containing the outline and catalog, runs the selector, and emits the
// SelectionPlan as an artifact.
func (ag *Agent) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		if execCtx.StoredTask == nil {
			if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
				return
			}
		}

		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, nil), nil) {
			return
		}

		input, err := extractSelectorInput(execCtx.Message)
		if err != nil {
			msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("invalid input: "+err.Error()))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, msg), nil)
			return
		}

		plan, _, err := ag.Run(ctx, input.Outline, input.CompactCatalog, "")
		if err != nil {
			msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("selector failed: "+err.Error()))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, msg), nil)
			return
		}

		planJSON, err := json.Marshal(plan)
		if err != nil {
			msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("failed to marshal plan: "+err.Error()))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, msg), nil)
			return
		}
		var planData any
		if err := json.Unmarshal(planJSON, &planData); err != nil {
			msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("failed to prepare plan data: "+err.Error()))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, msg), nil)
			return
		}

		if !yield(a2a.NewArtifactEvent(execCtx, a2a.NewDataPart(planData)), nil) {
			return
		}

		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil)
	}
}

// Cancel implements a2asrv.AgentExecutor.
func (ag *Agent) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
	}
}

func extractSelectorInput(msg *a2a.Message) (*selectorInput, error) {
	if msg == nil {
		return nil, json.Unmarshal(nil, &selectorInput{})
	}
	for _, part := range msg.Parts {
		if data, ok := part.Content.(a2a.Data); ok {
			raw, err := json.Marshal(data.Value)
			if err != nil {
				continue
			}
			var input selectorInput
			if err := json.Unmarshal(raw, &input); err != nil {
				continue
			}
			if input.Outline != nil {
				return &input, nil
			}
		}
	}
	return nil, fmt.Errorf("no valid selector input found in message parts")
}
