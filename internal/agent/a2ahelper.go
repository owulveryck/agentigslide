package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// ExecuteA2A provides common A2A Execute boilerplate for agents.
// It handles task submission, status updates, input extraction, result
// marshaling, and artifact emission. Each agent only needs to provide
// its extraction and run functions.
func ExecuteA2A[I, O any](
	ctx context.Context,
	execCtx *a2asrv.ExecutorContext,
	agentName string,
	extractFn func(*a2a.Message) (I, error),
	runFn func(context.Context, I) (O, error),
	completionMsgFn func(O) *a2a.Message,
) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		if execCtx.StoredTask == nil {
			if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
				return
			}
		}

		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, nil), nil) {
			return
		}

		input, err := extractFn(execCtx.Message)
		if err != nil {
			msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(agentName+" input error: "+err.Error()))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, msg), nil)
			return
		}

		result, err := runFn(ctx, input)
		if err != nil {
			msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(agentName+" failed: "+err.Error()))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, msg), nil)
			return
		}

		resultJSON, err := json.Marshal(result)
		if err != nil {
			msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("failed to marshal "+agentName+" result: "+err.Error()))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, msg), nil)
			return
		}
		var resultData any
		if err := json.Unmarshal(resultJSON, &resultData); err != nil {
			msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("failed to prepare "+agentName+" data: "+err.Error()))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, msg), nil)
			return
		}

		if !yield(a2a.NewArtifactEvent(execCtx, a2a.NewDataPart(resultData)), nil) {
			return
		}

		var completionMsg *a2a.Message
		if completionMsgFn != nil {
			completionMsg = completionMsgFn(result)
		}
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, completionMsg), nil)
	}
}

// CancelA2A provides common A2A Cancel boilerplate.
func CancelA2A(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
	}
}

// ExtractDataInput extracts a typed struct from the first data part of an A2A
// message. The validate function should return true if the extracted value
// looks valid (e.g. a required field is non-nil).
func ExtractDataInput[T any](msg *a2a.Message, validate func(*T) bool) (*T, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil message")
	}
	for _, part := range msg.Parts {
		if data, ok := part.Content.(a2a.Data); ok {
			raw, err := json.Marshal(data.Value)
			if err != nil {
				continue
			}
			var input T
			if err := json.Unmarshal(raw, &input); err != nil {
				continue
			}
			if validate(&input) {
				return &input, nil
			}
		}
	}
	return nil, fmt.Errorf("no valid input found in message parts")
}

// ExtractTextInput extracts concatenated text from an A2A message's text parts.
func ExtractTextInput(msg *a2a.Message) string {
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
