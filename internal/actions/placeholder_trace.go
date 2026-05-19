package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/routerarchitects/nats-agent-core/agentcore"
)

type PlaceholderTraceExecutor struct{}

func NewPlaceholderTraceExecutor() *PlaceholderTraceExecutor {
	return &PlaceholderTraceExecutor{}
}

func (e *PlaceholderTraceExecutor) Execute(ctx context.Context, msg agentcore.ActionCommand) (Output, error) {
	if ctx == nil {
		return Output{}, errors.New("execute action: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return Output{}, fmt.Errorf("execute action: %w", err)
	}
	if msg.Target == "" {
		return Output{}, errors.New("execute action: target is empty")
	}
	if msg.Action != ActionTrace {
		return Output{}, fmt.Errorf("execute action: unsupported action %q", msg.Action)
	}
	if msg.RPCID == "" {
		return Output{}, errors.New("execute action: rpc id is empty")
	}
	if len(msg.Payload) == 0 || !json.Valid(msg.Payload) {
		return Output{}, fmt.Errorf("%w: payload must be valid json", ErrInvalidActionPayload)
	}

	payload, err := json.Marshal(map[string]any{
		"executor":         "placeholder_trace",
		"action":           ActionTrace,
		"target":           msg.Target,
		"status":           "completed",
		"placeholder":      true,
		"received_payload": true,
	})
	if err != nil {
		return Output{}, fmt.Errorf("build placeholder trace payload: %w", err)
	}

	return Output{
		Payload: payload,
		Message: "placeholder trace action completed",
	}, nil
}
