package agent

import (
	"context"
	"fmt"

	"github.com/routerarchitects/nats-agent-core/agentcore"
)

func (r *Runtime) registerHandlers() error {
	target := r.appConfig.Agent.Target

	if err := r.client.RegisterConfigureHandler(target, r.handleConfigure); err != nil {
		return fmt.Errorf("register configure handler: %w", err)
	}
	r.logInfo("configure handler registered", "target", target)

	for _, action := range r.appConfig.Agent.Actions.Enabled {
		if err := r.client.RegisterActionHandler(target, action, r.handleAction); err != nil {
			return fmt.Errorf("register action handler %q: %w", action, err)
		}
		r.logInfo("action handler registered", "target", target, "action", action)
	}

	return nil
}

func (r *Runtime) handleConfigure(ctx context.Context, msg agentcore.ConfigureNotification) error {
	r.logInfo(
		"configure handler invoked",
		"target", msg.Target,
		"rpc_id", msg.RPCID,
		"uuid", msg.UUID,
	)
	if r.configureService == nil {
		return fmt.Errorf("configure service is not initialized")
	}
	return r.configureService.Handle(ctx, msg)
}

func (r *Runtime) handleAction(ctx context.Context, msg agentcore.ActionCommand) error {
	r.logInfo(
		"action handler invoked",
		"target", msg.Target,
		"action", msg.Action,
		"rpc_id", msg.RPCID,
	)
	r.logInfo(
		"action handler placeholder result publishing",
		"target", msg.Target,
		"action", msg.Action,
		"rpc_id", msg.RPCID,
		"stage", "action",
		"status", "not_implemented",
	)

	result := agentcore.ResultEnvelope{
		Version:     wireVersion,
		RPCID:       msg.RPCID,
		Target:      msg.Target,
		CommandType: "action",
		Action:      msg.Action,
		Result:      "failure",
		ErrorCode:   "not_implemented",
		Message:     "action execution is not implemented yet",
		Timestamp:   r.now().UTC(),
	}
	if err := r.client.PublishResult(ctx, result); err != nil {
		r.logError(
			"action handler result publish failed",
			"target", msg.Target,
			"action", msg.Action,
			"rpc_id", msg.RPCID,
			"error", err,
		)
		return fmt.Errorf("publish action placeholder result: %w", err)
	}

	return nil
}
