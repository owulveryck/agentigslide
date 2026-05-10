package outliner

import (
	"context"
	"encoding/json"
	"iter"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

var _ a2asrv.AgentExecutor = (*Agent)(nil)

// Execute implements a2asrv.AgentExecutor. It extracts the user request
// from the incoming message's text parts, runs the outliner, and emits
// the resulting PresentationOutline as a JSON data artifact.
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

		userRequest := extractTextFromMessage(execCtx.Message)
		if userRequest == "" {
			msg := a2a.NewMessage(a2a.MessageRoleAgent,a2a.NewTextPart("error: empty user request"))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, msg), nil)
			return
		}

		outline, err := ag.Run(ctx, userRequest, "")
		if err != nil {
			msg := a2a.NewMessage(a2a.MessageRoleAgent,a2a.NewTextPart("outliner failed: " + err.Error()))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, msg), nil)
			return
		}

		outlineJSON, err := json.Marshal(outline)
		if err != nil {
			msg := a2a.NewMessage(a2a.MessageRoleAgent,a2a.NewTextPart("failed to marshal outline: " + err.Error()))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, msg), nil)
			return
		}

		var outlineData any
		json.Unmarshal(outlineJSON, &outlineData)

		if !yield(a2a.NewArtifactEvent(execCtx, a2a.NewDataPart(outlineData)), nil) {
			return
		}

		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil)
	}
}

// Cancel implements a2asrv.AgentExecutor. It emits a canceled status.
func (ag *Agent) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
	}
}

func extractTextFromMessage(msg *a2a.Message) string {
	if msg == nil {
		return ""
	}
	var text string
	for _, part := range msg.Parts {
		if t := part.Text(); t != "" {
			if text != "" {
				text += "\n"
			}
			text += t
		}
	}
	return text
}
