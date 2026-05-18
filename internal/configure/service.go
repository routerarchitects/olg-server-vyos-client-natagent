package configure

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/routerarchitects/nats-agent-core/agentcore"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/state"
)

const (
	wireVersion = "1.0"
)

type Service struct {
	client      AgentCoreClient
	stateStore  StateStore
	renderer    Renderer
	applyEngine ApplyEngine
	logger      agentcore.Logger
	now         func() time.Time
	mu          sync.Mutex
}

type Dependencies struct {
	Client      AgentCoreClient
	StateStore  StateStore
	Renderer    Renderer
	ApplyEngine ApplyEngine
	Logger      agentcore.Logger
	Now         func() time.Time
}

func NewService(deps Dependencies) (*Service, error) {
	if deps.Client == nil {
		return nil, errors.New("configure service: client is required")
	}
	if deps.StateStore == nil {
		return nil, errors.New("configure service: state store is required")
	}
	if deps.Renderer == nil {
		return nil, errors.New("configure service: renderer is required")
	}
	if deps.ApplyEngine == nil {
		return nil, errors.New("configure service: apply engine is required")
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}

	return &Service{
		client:      deps.Client,
		stateStore:  deps.StateStore,
		renderer:    deps.Renderer,
		applyEngine: deps.ApplyEngine,
		logger:      deps.Logger,
		now:         deps.Now,
	}, nil
}

func (s *Service) Handle(ctx context.Context, msg agentcore.ConfigureNotification) error {
	if ctx == nil {
		return errors.New("configure handle: context is nil")
	}

	// Serialize configure processing so local applied UUID load/apply/save remains ordered.
	// Future multi-target support can replace this with per-target locking.
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.publishStatus(ctx, msg, "running", "received", "configure notification received"); err != nil {
		return s.fail(ctx, msg, "status_publish_failed", "configure processing failed", fmt.Errorf("publish configure status received: %w", err))
	}

	s.logInfo("configure desired loading", "target", msg.Target, "rpc_id", msg.RPCID, "uuid", msg.UUID, "stage", "loading_desired")
	if err := s.publishStatus(ctx, msg, "running", "loading_desired", "loading desired config"); err != nil {
		return s.fail(ctx, msg, "status_publish_failed", "configure processing failed", fmt.Errorf("publish configure status loading_desired: %w", err))
	}

	desired, err := s.client.LoadDesiredConfig(ctx, msg.Target)
	if err != nil {
		return s.fail(ctx, msg, "load_desired_failed", "failed to load desired config", fmt.Errorf("load desired config: %w", err))
	}
	if desired == nil {
		return s.fail(ctx, msg, "desired_config_missing", "desired config missing", errors.New("desired config is nil"))
	}
	if desired.Record.Target != msg.Target {
		return s.fail(ctx, msg, "desired_target_mismatch", "desired target mismatch", fmt.Errorf("desired target %q does not match notification target %q", desired.Record.Target, msg.Target))
	}
	if desired.Record.UUID != msg.UUID {
		return s.fail(ctx, msg, "desired_uuid_mismatch", "desired uuid mismatch", fmt.Errorf("desired uuid %q does not match notification uuid %q", desired.Record.UUID, msg.UUID))
	}
	s.logInfo("configure desired loaded", "target", msg.Target, "rpc_id", msg.RPCID, "uuid", msg.UUID)

	localState, err := s.stateStore.Load(ctx)
	if err != nil {
		return s.fail(ctx, msg, "state_load_failed", "failed to load local state", fmt.Errorf("load local state: %w", err))
	}
	s.logInfo("configure state loaded", "target", msg.Target, "rpc_id", msg.RPCID, "uuid", msg.UUID)

	if localState.AppliedUUID == desired.Record.UUID {
		s.logInfo("configure already in sync", "target", msg.Target, "rpc_id", msg.RPCID, "uuid", msg.UUID, "stage", "already_in_sync", "status", "success")
		if err := s.publishStatus(ctx, msg, "success", "already_in_sync", "desired config already applied"); err != nil {
			return s.fail(ctx, msg, "status_publish_failed", "configure processing failed", fmt.Errorf("publish configure status already_in_sync: %w", err))
		}
		if err := s.publishSuccessResult(ctx, msg, "desired config already applied"); err != nil {
			return s.fail(ctx, msg, "result_publish_failed", "failed to publish configure result", fmt.Errorf("publish configure already-in-sync result: %w", err))
		}
		return nil
	}

	s.logInfo("configure rendering", "target", msg.Target, "rpc_id", msg.RPCID, "uuid", msg.UUID, "stage", "rendering")
	if err := s.publishStatus(ctx, msg, "running", "rendering", "rendering desired config"); err != nil {
		return s.fail(ctx, msg, "status_publish_failed", "configure processing failed", fmt.Errorf("publish configure status rendering: %w", err))
	}

	rendered, err := s.renderer.Render(ctx, *desired)
	if err != nil {
		return s.fail(ctx, msg, "render_failed", "failed to render desired config", fmt.Errorf("render desired config: %w", err))
	}

	s.logInfo("configure rendered", "target", msg.Target, "rpc_id", msg.RPCID, "uuid", msg.UUID, "stage", "rendered")
	if err := s.publishStatus(ctx, msg, "running", "rendered", "desired config rendered"); err != nil {
		return s.fail(ctx, msg, "status_publish_failed", "configure processing failed", fmt.Errorf("publish configure status rendered: %w", err))
	}

	s.logInfo("configure applying", "target", msg.Target, "rpc_id", msg.RPCID, "uuid", msg.UUID, "stage", "applying")
	if err := s.publishStatus(ctx, msg, "running", "applying", "applying rendered config"); err != nil {
		return s.fail(ctx, msg, "status_publish_failed", "configure processing failed", fmt.Errorf("publish configure status applying: %w", err))
	}

	if err := s.applyEngine.Apply(ctx, rendered); err != nil {
		return s.fail(ctx, msg, "apply_failed", "failed to apply rendered config", fmt.Errorf("apply rendered config: %w", err))
	}

	s.logInfo("configure applied", "target", msg.Target, "rpc_id", msg.RPCID, "uuid", msg.UUID, "stage", "applied")
	if err := s.publishStatus(ctx, msg, "success", "applied", "placeholder configure apply completed"); err != nil {
		return s.fail(ctx, msg, "status_publish_failed", "configure processing failed", fmt.Errorf("publish configure status applied: %w", err))
	}

	nextState := state.State{
		Target:      msg.Target,
		AppliedUUID: desired.Record.UUID,
		AppliedAt:   s.now().UTC(),
	}
	if err := s.stateStore.Save(ctx, nextState); err != nil {
		return s.fail(ctx, msg, "state_save_failed", "failed to save local state", fmt.Errorf("save local state: %w", err))
	}
	s.logInfo("configure state saved", "target", msg.Target, "rpc_id", msg.RPCID, "uuid", msg.UUID)

	s.logInfo("configure result publishing", "target", msg.Target, "rpc_id", msg.RPCID, "uuid", msg.UUID, "status", "success")
	if err := s.publishSuccessResult(ctx, msg, "placeholder configure apply completed"); err != nil {
		return s.fail(ctx, msg, "result_publish_failed", "failed to publish configure result", fmt.Errorf("publish configure success result: %w", err))
	}
	s.logInfo("configure result published", "target", msg.Target, "rpc_id", msg.RPCID, "uuid", msg.UUID, "status", "success")

	return nil
}

func (s *Service) fail(ctx context.Context, msg agentcore.ConfigureNotification, code, safeMessage string, originalErr error) error {
	s.logError("configure failed", "target", msg.Target, "rpc_id", msg.RPCID, "uuid", msg.UUID, "stage", "failed", "status", "failure", "error", originalErr)

	var statusErr error
	if err := s.publishStatus(ctx, msg, "failure", "failed", "configure processing failed"); err != nil {
		statusErr = fmt.Errorf("publish configure failure status: %w", err)
	}

	var resultErr error
	if err := s.publishFailureResult(ctx, msg, code, safeMessage); err != nil {
		resultErr = fmt.Errorf("publish configure failure result: %w", err)
	}

	return errors.Join(originalErr, statusErr, resultErr)
}

func (s *Service) publishStatus(ctx context.Context, msg agentcore.ConfigureNotification, status, stage, message string) error {
	return s.client.PublishStatus(ctx, agentcore.StatusEnvelope{
		Version:   wireVersion,
		RPCID:     msg.RPCID,
		Target:    msg.Target,
		UUID:      msg.UUID,
		Status:    status,
		Stage:     stage,
		Message:   message,
		Timestamp: s.now().UTC(),
	})
}

func (s *Service) publishSuccessResult(ctx context.Context, msg agentcore.ConfigureNotification, message string) error {
	return s.client.PublishResult(ctx, agentcore.ResultEnvelope{
		Version:     wireVersion,
		RPCID:       msg.RPCID,
		Target:      msg.Target,
		CommandType: "configure",
		UUID:        msg.UUID,
		Result:      "success",
		Message:     message,
		Timestamp:   s.now().UTC(),
	})
}

func (s *Service) publishFailureResult(ctx context.Context, msg agentcore.ConfigureNotification, code, message string) error {
	return s.client.PublishResult(ctx, agentcore.ResultEnvelope{
		Version:     wireVersion,
		RPCID:       msg.RPCID,
		Target:      msg.Target,
		CommandType: "configure",
		UUID:        msg.UUID,
		Result:      "failure",
		ErrorCode:   code,
		Message:     message,
		Timestamp:   s.now().UTC(),
	})
}

func (s *Service) logInfo(msg string, kv ...any) {
	if s.logger == nil {
		return
	}
	s.logger.Info(msg, kv...)
}

func (s *Service) logError(msg string, kv ...any) {
	if s.logger == nil {
		return
	}
	s.logger.Error(msg, kv...)
}
