package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/routerarchitects/nats-agent-core/agentcore"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/apply"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/applyvyos"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/config"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/configure"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/renderer"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/renderervyos"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/state"
)

/*
TC-AGENT-CONFIGURE-001
Type: Positive
Title: Placeholder mode wires placeholder engines
Summary:
Builds configure engines with placeholder mode selected.
The runtime should use internal placeholder renderer and apply
implementations for safe local and CI execution.

Validates:
  - placeholder renderer is selected
  - placeholder apply engine is selected
  - engine construction succeeds
*/
func TestNewConfigureEnginesWiresPlaceholderMode(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Agent.Configure.Mode = "placeholder"

	rndr, applier, err := newConfigureEngines(&cfg, nil)
	if err != nil {
		t.Fatalf("new configure engines: %v", err)
	}
	if _, ok := rndr.(*renderer.Placeholder); !ok {
		t.Fatalf("renderer type got=%T want *renderer.Placeholder", rndr)
	}
	if _, ok := applier.(*apply.Placeholder); !ok {
		t.Fatalf("apply type got=%T want *apply.Placeholder", applier)
	}
}

/*
TC-AGENT-CONFIGURE-002
Type: Positive
Title: Real mode wires VyOS adapters
Summary:
Builds configure engines with real mode selected.
The runtime should create adapters around the olg-renderer-vyos
renderer and apply packages.

Validates:
  - real renderer adapter is selected
  - real apply adapter is selected
  - engine construction succeeds
*/
func TestNewConfigureEnginesWiresRealMode(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Agent.Configure.Mode = "real"

	rndr, applier, err := newConfigureEngines(&cfg, nil)
	if err != nil {
		t.Fatalf("new configure engines: %v", err)
	}
	if _, ok := rndr.(*renderervyos.Adapter); !ok {
		t.Fatalf("renderer type got=%T want *renderervyos.Adapter", rndr)
	}
	if _, ok := applier.(*applyvyos.Adapter); !ok {
		t.Fatalf("apply type got=%T want *applyvyos.Adapter", applier)
	}
}

/*
TC-AGENT-CONFIGURE-003
Type: Positive
Title: Real mode accepts debug logging config
Summary:
Builds configure engines with real mode and debug log flags enabled.
Debug settings should be accepted without changing the selected
renderer and apply adapter implementations.

Validates:
  - real renderer adapter is still selected
  - real apply adapter is still selected
  - debug flags do not break construction
*/
func TestNewConfigureEnginesAcceptsDebugLoggingConfig(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Agent.Configure.Mode = "real"
	cfg.Agent.Logging.Level = "debug"
	cfg.Agent.Debug.LogPayloads = true
	cfg.Agent.Debug.LogRendered = true
	cfg.Agent.Debug.LogApplyPlan = true

	rndr, applier, err := newConfigureEngines(&cfg, nil)
	if err != nil {
		t.Fatalf("new configure engines: %v", err)
	}
	if _, ok := rndr.(*renderervyos.Adapter); !ok {
		t.Fatalf("renderer type got=%T want *renderervyos.Adapter", rndr)
	}
	if _, ok := applier.(*applyvyos.Adapter); !ok {
		t.Fatalf("apply type got=%T want *applyvyos.Adapter", applier)
	}
}

/*
TC-AGENT-CONFIGURE-004
Type: Negative
Title: Invalid configure mode fails wiring
Summary:
Builds configure engines with an unsupported mode.
Runtime construction should fail before the agent starts with a
clear configure mode error.

Validates:
  - invalid mode returns an error
  - error mentions agent.configure.mode
*/
func TestNewConfigureEnginesRejectsInvalidMode(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Agent.Configure.Mode = "invalid"

	_, _, err := newConfigureEngines(&cfg, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "agent.configure.mode") {
		t.Fatalf("error %q does not mention agent.configure.mode", err.Error())
	}
}

type mockAgentCoreClient struct {
	desired *agentcore.StoredDesiredConfig
	err     error
}

func (m *mockAgentCoreClient) LoadDesiredConfig(ctx context.Context, target string) (*agentcore.StoredDesiredConfig, error) {
	return m.desired, m.err
}

func (m *mockAgentCoreClient) PublishStatus(ctx context.Context, msg agentcore.StatusEnvelope) error {
	return nil
}

func (m *mockAgentCoreClient) PublishResult(ctx context.Context, msg agentcore.ResultEnvelope) error {
	return nil
}

type mockStateStore struct {
	loadFunc func(ctx context.Context) (state.State, error)
}

func (m *mockStateStore) Load(ctx context.Context) (state.State, error) {
	if m.loadFunc != nil {
		return m.loadFunc(ctx)
	}
	return state.State{}, nil
}

func (m *mockStateStore) Save(ctx context.Context, s state.State) error {
	return nil
}

type mockRenderer struct{}

func (m *mockRenderer) Render(ctx context.Context, d agentcore.StoredDesiredConfig) (renderer.Output, error) {
	return renderer.Output{}, nil
}

type mockApplyEngine struct{}

func (m *mockApplyEngine) Apply(ctx context.Context, o renderer.Output) error {
	return nil
}

/*
TC-AGENT-LIFE-010
Type: Safety
Title: Shutting down runtime cancels active reconnect reconcile and leaves no stray goroutines
Summary:
Starts the agent and triggers a reconnect-triggered reconciliation pass.
The reconciliation pass blocks in the state store Load method. Calling Close() must cancel
the context, causing the reconnect goroutine to unblock and exit, and Close()
must wait for it to complete.

Validates:
  - Close() cancels context and waits for reconnect reconciliation goroutine.
  - No stray goroutines are left running.
*/
func TestRuntimeCloseCancelsActiveReconnectReconcile(t *testing.T) {
	appCfg := config.DefaultAppConfig()
	appCfg.Agent.Target = "vyos"
	coreCfg := agentcore.Config{}

	r, err := New(&appCfg, coreCfg)
	if err != nil {
		t.Fatalf("failed to create agent runtime: %v", err)
	}

	blockCh := make(chan struct{})
	loadEntered := make(chan struct{})

	stateStore := &mockStateStore{
		loadFunc: func(ctx context.Context) (state.State, error) {
			close(loadEntered)
			select {
			case <-blockCh:
				return state.State{}, nil
			case <-ctx.Done():
				return state.State{}, ctx.Err()
			}
		},
	}

	client := &mockAgentCoreClient{}
	rndr := &mockRenderer{}
	apply := &mockApplyEngine{}

	configureService, err := configure.NewService(configure.Dependencies{
		Client:      client,
		StateStore:  stateStore,
		Renderer:    rndr,
		ApplyEngine: apply,
		Now:         r.now,
	})
	if err != nil {
		t.Fatalf("failed to create configure service: %v", err)
	}

	r.configureService = configureService

	// Simulate starting reconnect reconciliation goroutine (as in agentcore WithReconnectHandler)
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.reconnectReconcileCount.Add(1)
		_ = r.configureService.Reconcile(r.ctx, "vyos")
	}()

	// Wait for the state store Load to be entered and blocked
	select {
	case <-loadEntered:
	case <-time.After(1 * time.Second):
		t.Fatal("reconcile goroutine did not start or load state")
	}

	closeDone := make(chan struct{})
	go func() {
		_ = r.Close(context.Background())
		close(closeDone)
	}()

	// Verify that Close returns and unblocks
	select {
	case <-closeDone:
	case <-time.After(1 * time.Second):
		t.Fatal("Close blocked and did not return (possibly didn't cancel context or wait properly)")
	}
}

/*
TC-AGENT-LIFE-011
Type: Safety
Title: Close() does not hang indefinitely when reconnect reconciliation goroutine is non-cooperative
Summary:
Starts the agent and triggers a reconnect-reconciliation goroutine that blocks indefinitely in stateStore.Load
(ignoring the context cancel signal). Calling Close() with a context timeout should return a timeout error
quickly instead of hanging forever.

Validates:
  - Close() handles non-cooperative/blocked background goroutines gracefully by returning a context timeout error.
*/
func TestRuntimeCloseHandlesNonCooperativeReconnectReconcile(t *testing.T) {
	appCfg := config.DefaultAppConfig()
	appCfg.Agent.Target = "vyos"
	coreCfg := agentcore.Config{}

	r, err := New(&appCfg, coreCfg)
	if err != nil {
		t.Fatalf("failed to create agent runtime: %v", err)
	}

	loadEntered := make(chan struct{})

	// StateStore that blocks indefinitely and ignores context Done/cancel.
	stateStore := &mockStateStore{
		loadFunc: func(ctx context.Context) (state.State, error) {
			close(loadEntered)
			// Block forever, ignoring context Done.
			select {}
		},
	}

	client := &mockAgentCoreClient{}
	rndr := &mockRenderer{}
	apply := &mockApplyEngine{}

	configureService, err := configure.NewService(configure.Dependencies{
		Client:      client,
		StateStore:  stateStore,
		Renderer:    rndr,
		ApplyEngine: apply,
		Now:         r.now,
	})
	if err != nil {
		t.Fatalf("failed to create configure service: %v", err)
	}

	r.configureService = configureService

	// Simulate starting reconnect reconciliation goroutine
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.reconnectReconcileCount.Add(1)
		_ = r.configureService.Reconcile(r.ctx, "vyos")
	}()

	// Wait for the state store Load to be entered and blocked
	select {
	case <-loadEntered:
	case <-time.After(1 * time.Second):
		t.Fatal("reconcile goroutine did not start or load state")
	}

	// Call Close with a 100ms timeout context
	closeCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	closeDone := make(chan struct{})
	var closeErr error
	go func() {
		closeErr = r.Close(closeCtx)
		close(closeDone)
	}()

	select {
	case <-closeDone:
		if closeErr == nil {
			t.Fatal("expected Close to return an error on timeout, got nil")
		}
		if !errors.Is(closeErr, context.DeadlineExceeded) && !strings.Contains(closeErr.Error(), "deadline exceeded") {
			t.Fatalf("expected close error to be deadline exceeded, got: %v", closeErr)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Close hung and failed to return on context timeout within 500ms")
	}
}
