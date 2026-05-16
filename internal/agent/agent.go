package agent

import (
	"fmt"
	"sync"
	"time"

	"github.com/routerarchitects/nats-agent-core/agentcore"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/config"
)

const (
	wireVersion = "1.0"
)

// Runtime owns the Phase 2 agent lifecycle around an agentcore.Client.
type Runtime struct {
	appConfig  *config.AppConfig
	coreConfig agentcore.Config
	client     *agentcore.Client
	logger     agentcore.Logger
	now        func() time.Time

	mu      sync.Mutex
	started bool
	closed  bool
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

	client, err := agentcore.New(coreCfg, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("create agentcore client: %w", err)
	}

	r := &Runtime{
		appConfig:  appCfg,
		coreConfig: coreCfg,
		client:     client,
		logger:     options.logger,
		now:        options.now,
	}
	r.logInfo("agentcore client created", "target", r.appConfig.Agent.Target)
	return r, nil
}
