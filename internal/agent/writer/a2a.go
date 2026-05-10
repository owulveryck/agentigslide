package writer

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

type writerInput struct {
	SourceSlide    int                     `json:"sourceSlide"`
	SlideNeed      agentpkg.SlideNeed      `json:"slideNeed"`
	TemplateFields []agentpkg.TemplateField `json:"templateFields"`
}

// Execute implements a2asrv.AgentExecutor. It expects a JSON data part
// with the slide need and template fields, writes the content, and emits
// the SlideContent as an artifact.
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

		input, err := extractWriterInput(execCtx.Message)
		if err != nil {
			msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("invalid input: "+err.Error()))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, msg), nil)
			return
		}

		content, err := ag.WriteSlide(ctx, input.SourceSlide, input.SlideNeed, input.TemplateFields, "")
		if err != nil {
			msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("writer failed: "+err.Error()))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, msg), nil)
			return
		}

		contentJSON, err := json.Marshal(content)
		if err != nil {
			msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("failed to marshal content: "+err.Error()))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, msg), nil)
			return
		}
		var contentData any
		if err := json.Unmarshal(contentJSON, &contentData); err != nil {
			msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("failed to prepare content data: "+err.Error()))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, msg), nil)
			return
		}

		if !yield(a2a.NewArtifactEvent(execCtx, a2a.NewDataPart(contentData)), nil) {
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

func extractWriterInput(msg *a2a.Message) (*writerInput, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil message")
	}
	for _, part := range msg.Parts {
		if data, ok := part.Content.(a2a.Data); ok {
			raw, err := json.Marshal(data.Value)
			if err != nil {
				continue
			}
			var input writerInput
			if err := json.Unmarshal(raw, &input); err != nil {
				continue
			}
			if len(input.TemplateFields) > 0 {
				return &input, nil
			}
		}
	}
	return nil, fmt.Errorf("no valid writer input found in message parts")
}
