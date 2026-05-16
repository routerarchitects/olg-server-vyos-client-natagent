package agent

import (
	"context"
	"fmt"

	"github.com/routerarchitects/nats-agent-core/agentcore"
)

func (r *Runtime) publishStartupStatus(ctx context.Context) error {
	r.logInfo(
		"startup status publishing",
		"target", r.appConfig.Agent.Target,
		"stage", "startup",
		"status", "running",
	)

	msg := agentcore.StatusEnvelope{
		Version:   wireVersion,
		Target:    r.appConfig.Agent.Target,
		Status:    "running",
		Stage:     "startup",
		Message:   "vyos-nats-agent started",
		Timestamp: r.now().UTC(),
	}
	if err := r.client.PublishStatus(ctx, msg); err != nil {
		return fmt.Errorf("publish startup status: %w", err)
	}

	r.logInfo(
		"startup status published",
		"target", r.appConfig.Agent.Target,
		"stage", "startup",
		"status", "running",
	)
	return nil
}
