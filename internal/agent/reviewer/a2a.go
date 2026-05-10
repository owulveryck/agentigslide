package reviewer

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/owulveryck/agentigslide/internal/model"
)

var _ a2asrv.AgentExecutor = (*Agent)(nil)

type reviewerInput struct {
	Plan           *model.GenerationPlan `json:"plan"`
	UserRequest    string                `json:"userRequest"`
	CompactCatalog string                `json:"compactCatalog"`
}

// Execute implements a2asrv.AgentExecutor. It expects a JSON data part
// containing the plan, user request, and catalog, runs the review, and
// emits the ReviewResult as an artifact.
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

		input, err := extractReviewerInput(execCtx.Message)
		if err != nil {
			msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("invalid input: "+err.Error()))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, msg), nil)
			return
		}

		result, err := ag.Run(ctx, input.Plan, input.UserRequest, input.CompactCatalog, "", 0)
		if err != nil {
			msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("reviewer failed: "+err.Error()))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, msg), nil)
			return
		}

		resultJSON, err := json.Marshal(result)
		if err != nil {
			msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("failed to marshal result: "+err.Error()))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, msg), nil)
			return
		}
		var resultData any
		if err := json.Unmarshal(resultJSON, &resultData); err != nil {
			msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("failed to prepare result data: "+err.Error()))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, msg), nil)
			return
		}

		if !yield(a2a.NewArtifactEvent(execCtx, a2a.NewDataPart(resultData)), nil) {
			return
		}

		var completionMsg *a2a.Message
		if !result.Approved {
			completionMsg = a2a.NewMessage(a2a.MessageRoleAgent,
				a2a.NewTextPart(fmt.Sprintf("Review completed with %d issues", len(result.Issues))),
			)
		}
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, completionMsg), nil)
	}
}

// Cancel implements a2asrv.AgentExecutor.
func (ag *Agent) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
	}
}

func extractReviewerInput(msg *a2a.Message) (*reviewerInput, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil message")
	}
	for _, part := range msg.Parts {
		if data, ok := part.Content.(a2a.Data); ok {
			raw, err := json.Marshal(data.Value)
			if err != nil {
				continue
			}
			var input reviewerInput
			if err := json.Unmarshal(raw, &input); err != nil {
				continue
			}
			if input.Plan != nil {
				return &input, nil
			}
		}
	}
	return nil, fmt.Errorf("no valid reviewer input found in message parts")
}
