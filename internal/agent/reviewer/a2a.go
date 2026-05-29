package reviewer

import (
	"context"
	"fmt"
	"iter"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	agentpkg "github.com/owulveryck/agentigslide/internal/agent"
	"github.com/owulveryck/agentigslide/internal/model"
)

var _ a2asrv.AgentExecutor = (*Agent)(nil)

type reviewerInput struct {
	Plan           *model.GenerationPlan `json:"plan"`
	UserRequest    string                `json:"userRequest"`
	CompactCatalog string                `json:"compactCatalog"`
}

func (ag *Agent) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return agentpkg.ExecuteA2A(ctx, execCtx, "reviewer",
		func(msg *a2a.Message) (*reviewerInput, error) {
			return agentpkg.ExtractDataInput(msg, func(r *reviewerInput) bool {
				return r.Plan != nil
			})
		},
		func(ctx context.Context, input *reviewerInput) (*agentpkg.ReviewResult, error) {
			result, _, err := ag.Run(ctx, input.Plan, input.UserRequest, input.CompactCatalog, "", 0, "")
			return result, err
		},
		func(result *agentpkg.ReviewResult) *a2a.Message {
			if !result.Approved {
				return a2a.NewMessage(a2a.MessageRoleAgent,
					a2a.NewTextPart(fmt.Sprintf("Review completed with %d issues", len(result.Issues))),
				)
			}
			return nil
		},
	)
}

func (ag *Agent) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return agentpkg.CancelA2A(ctx, execCtx)
}
