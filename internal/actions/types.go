package actions

import (
	"context"
	"encoding/json"

	"github.com/routerarchitects/nats-agent-core/agentcore"
)

type AgentCoreClient interface {
	PublishStatus(ctx context.Context, msg agentcore.StatusEnvelope) error
	PublishResult(ctx context.Context, msg agentcore.ResultEnvelope) error
}

type Executor interface {
	Execute(ctx context.Context, msg agentcore.ActionCommand) (Output, error)
}

type Output struct {
	Payload json.RawMessage
	Message string
}
