package agent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/routerarchitects/nats-agent-core/agentcore"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/actions"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/apply"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/applyvyos"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/config"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/configure"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/renderer"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/renderervyos"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/state"
)

const (
	wireVersion = "1.0"
)

// Runtime owns agent lifecycle wiring around an agentcore client and delegates configure/action handling.
type Runtime struct {
	appConfig  *config.AppConfig
	coreConfig agentcore.Config
	client     *agentcore.Client
	logger     agentcore.Logger
	now        func() time.Time

	configureService *configure.Service
	actionService    *actions.Service

	mu                      sync.Mutex
	started                 bool
	closed                  bool
	ctx                     context.Context
	cancel                  context.CancelFunc
	wg                      sync.WaitGroup
	reconnectReconcileCount atomic.Uint64
}

type runtimeOptions struct {
	logger agentcore.Logger
	now    func() time.Time
}

// Option configures Runtime construction.
type Option func(*runtimeOptions) error

// WithLogger wires a structured logger into runtime and agentcore.
func WithLogger(logger agentcore.Logger) Option {
	return func(opts *runtimeOptions) error {
		opts.logger = logger
		return nil
	}
}

// WithClock overrides the runtime clock.
func WithClock(now func() time.Time) Option {
	return func(opts *runtimeOptions) error {
		if now == nil {
			return fmt.Errorf("clock function is nil")
		}
		opts.now = now
		return nil
	}
}

// New creates a runtime and the underlying agentcore client.
func New(appCfg *config.AppConfig, coreCfg agentcore.Config, opts ...Option) (*Runtime, error) {
	if appCfg == nil {
		return nil, fmt.Errorf("app config is required")
	}

	options := runtimeOptions{
		now: time.Now,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&options); err != nil {
			return nil, err
		}
	}

	var r *Runtime

	var clientOpts []agentcore.Option
	if options.logger != nil {
		clientOpts = append(clientOpts,
			agentcore.WithLogger(options.logger),
			agentcore.WithErrorSink(func(err error) {
				if err == nil {
					return
				}
				options.logger.Error("agentcore async error", "error", err)
			}),
		)
	}

	clientOpts = append(clientOpts, agentcore.WithReconnectHandler(func() {
		if r == nil {
			return
		}
		r.mu.Lock()
		if r.closed || r.configureService == nil {
			r.mu.Unlock()
			return
		}
		ctx := r.ctx
		r.wg.Add(1)
		r.mu.Unlock()

		go func() {
			defer r.wg.Done()
			r.reconnectReconcileCount.Add(1)
			r.logInfo("reconnect detected, starting reconciliation pass", "target", r.appConfig.Agent.Target)
			if err := r.configureService.Reconcile(ctx, r.appConfig.Agent.Target); err != nil {
				r.logError("reconnection reconciliation failed", "error", err)
			}
			r.logInfo("reconnect reconciliation pass finished", "target", r.appConfig.Agent.Target)
		}()
	}))

	client, err := agentcore.New(coreCfg, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("create agentcore client: %w", err)
	}

	stateStore := state.NewFileStore(appCfg.Agent.StateFile)
	rendererEngine, applyEngine, err := newConfigureEngines(appCfg, options.logger)
	if err != nil {
		return nil, err
	}
	configureService, err := configure.NewService(configure.Dependencies{
		Client:      client,
		StateStore:  stateStore,
		Renderer:    rendererEngine,
		ApplyEngine: applyEngine,
		Logger:      options.logger,
		Debug:       configureDebugConfig(appCfg),
		Now:         options.now,
	})
	if err != nil {
		return nil, fmt.Errorf("create configure service: %w", err)
	}
	var traceExecutor actions.Executor
	if appCfg.Agent.Actions.Mode == "real" {
		traceExecutor = actions.NewVyOSTraceExecutor(actions.NewRealCommandRunner(), nil)
	} else {
		traceExecutor = actions.NewPlaceholderTraceExecutor()
	}
	actionService, err := actions.NewService(actions.Dependencies{
		Client:  client,
		Logger:  options.logger,
		Now:     options.now,
		Enabled: appCfg.Agent.Actions.Enabled,
		Executors: map[string]actions.Executor{
			actions.ActionTrace: traceExecutor,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create action service: %w", err)
	}

	rCtx, rCancel := context.WithCancel(context.Background())
	r = &Runtime{
		appConfig:        appCfg,
		coreConfig:       coreCfg,
		client:           client,
		logger:           options.logger,
		now:              options.now,
		configureService: configureService,
		actionService:    actionService,
		ctx:              rCtx,
		cancel:           rCancel,
	}
	r.logInfo("agentcore client created", "target", r.appConfig.Agent.Target)
	return r, nil
}

func newConfigureEngines(appCfg *config.AppConfig, logger agentcore.Logger) (configure.Renderer, configure.ApplyEngine, error) {
	if appCfg == nil {
		return nil, nil, fmt.Errorf("app config is required")
	}

	debug := configureDebugConfig(appCfg)
	switch appCfg.Agent.Configure.Mode {
	case "placeholder":
		return renderer.NewPlaceholder(), apply.NewPlaceholder(), nil
	case "real":
		rendererEngine, err := renderervyos.New(
			renderervyos.WithLogger(logger),
			renderervyos.WithDebugLogging(renderervyos.DebugLogging{
				LogPayloads: debug.LogPayloads,
				LogRendered: debug.LogRendered,
			}),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("create real configure renderer: %w", err)
		}
		applyEngine, err := applyvyos.New(
			appCfg.Agent.Apply.SaveAfterCommit,
			applyvyos.WithLogger(logger),
			applyvyos.WithDebugLogging(applyvyos.DebugLogging{
				LogRendered:  debug.LogRendered,
				LogApplyPlan: debug.LogApplyPlan,
			}),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("create real configure apply engine: %w", err)
		}
		return rendererEngine, applyEngine, nil
	default:
		return nil, nil, fmt.Errorf("agent.configure.mode must be one of placeholder, real")
	}
}

func configureDebugConfig(appCfg *config.AppConfig) configure.DebugLogging {
	if appCfg == nil || appCfg.Agent.Logging.Level != "debug" {
		return configure.DebugLogging{}
	}
	return configure.DebugLogging{
		LogPayloads:  appCfg.Agent.Debug.LogPayloads,
		LogRendered:  appCfg.Agent.Debug.LogRendered,
		LogApplyPlan: appCfg.Agent.Debug.LogApplyPlan,
	}
}

// ReconnectReconcileCount returns the count of reconnect-triggered reconciliation passes.
func (r *Runtime) ReconnectReconcileCount() uint64 {
	return r.reconnectReconcileCount.Load()
}
