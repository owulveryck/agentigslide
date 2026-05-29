package writer

import (
	"context"
	"iter"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	agentpkg "github.com/owulveryck/agentigslide/internal/agent"
)

var _ a2asrv.AgentExecutor = (*Agent)(nil)

type writerInput struct {
	SourceSlide    int                      `json:"sourceSlide"`
	SlideNeed      agentpkg.SlideNeed       `json:"slideNeed"`
	TemplateFields []agentpkg.TemplateField `json:"templateFields"`
}

func (ag *Agent) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return agentpkg.ExecuteA2A(ctx, execCtx, "writer",
		func(msg *a2a.Message) (*writerInput, error) {
			return agentpkg.ExtractDataInput(msg, func(w *writerInput) bool {
				return len(w.TemplateFields) > 0
			})
		},
		func(ctx context.Context, input *writerInput) (*agentpkg.SlideContent, error) {
			content, _, err := ag.WriteSlide(ctx, input.SourceSlide, input.SlideNeed, input.TemplateFields, "", "")
			return content, err
		},
		nil,
	)
}

func (ag *Agent) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return agentpkg.CancelA2A(ctx, execCtx)
}
