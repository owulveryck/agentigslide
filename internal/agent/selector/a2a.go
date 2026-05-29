package selector

import (
	"context"
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

func (ag *Agent) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return agentpkg.ExecuteA2A(ctx, execCtx, "selector",
		func(msg *a2a.Message) (*selectorInput, error) {
			return agentpkg.ExtractDataInput(msg, func(s *selectorInput) bool {
				return s.Outline != nil
			})
		},
		func(ctx context.Context, input *selectorInput) (*agentpkg.SelectionPlan, error) {
			plan, _, err := ag.Run(ctx, input.Outline, input.CompactCatalog, "", "")
			return plan, err
		},
		nil,
	)
}

func (ag *Agent) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return agentpkg.CancelA2A(ctx, execCtx)
}
