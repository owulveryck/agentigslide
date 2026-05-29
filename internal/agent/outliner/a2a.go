package outliner

import (
	"context"
	"fmt"
	"iter"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	agentpkg "github.com/owulveryck/agentigslide/internal/agent"
)

var _ a2asrv.AgentExecutor = (*Agent)(nil)

func (ag *Agent) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return agentpkg.ExecuteA2A(ctx, execCtx, "outliner",
		func(msg *a2a.Message) (string, error) {
			text := agentpkg.ExtractTextInput(msg)
			if text == "" {
				return "", fmt.Errorf("empty user request")
			}
			return text, nil
		},
		func(ctx context.Context, userRequest string) (*agentpkg.PresentationOutline, error) {
			outline, _, err := ag.Run(ctx, userRequest, "", "")
			return outline, err
		},
		nil,
	)
}

func (ag *Agent) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return agentpkg.CancelA2A(ctx, execCtx)
}
