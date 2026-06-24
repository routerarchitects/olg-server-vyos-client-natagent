package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/routerarchitects/nats-agent-core/agentcore"
)

const (
	defaultShutdownTimeout = 10 * time.Second
	startupCloseTimeout    = 5 * time.Second
)

// Runtime is single-use. If startup fails, the process should exit and a
// service supervisor should create a fresh Runtime on restart.
func (r *Runtime) beginStart() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return errors.New("runtime is closed")
	}
	if r.started {
		return errors.New("runtime already started")
	}

	r.started = true
	return nil
}

func (r *Runtime) markClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return true
	}

	r.closed = true
	return false
}

// Run starts the runtime, waits for shutdown signal via ctx cancellation,
// and closes the runtime gracefully.
func (r *Runtime) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("run context is nil")
	}

	if err := r.Start(ctx); err != nil {
		return err
	}

	<-ctx.Done()
	r.logInfo("shutdown requested", "error", ctx.Err())

	shutdownTimeout := r.coreConfig.Timeouts.ShutdownTimeout
	if shutdownTimeout <= 0 {
		shutdownTimeout = defaultShutdownTimeout
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := r.Close(shutdownCtx); err != nil {
		r.logError("shutdown error", "error", err)
		return err
	}
	return nil
}

// Start registers handlers, starts agentcore client, and publishes startup status.
func (r *Runtime) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("start context is nil")
	}

	if err := r.beginStart(); err != nil {
		return err
	}

	if err := r.registerHandlers(); err != nil {
		return err
	}

	r.logInfo("agentcore client starting", "target", r.appConfig.Agent.Target)
	if err := r.client.Start(ctx); err != nil {
		return fmt.Errorf("start agentcore client: %w", err)
	}

	startedHealth := r.client.Health()
	r.logInfo("agentcore client started", healthLogFields(startedHealth)...)

	if err := r.publishStartupStatus(ctx); err != nil {
		r.logError("startup status publish failed", "error", err)
		closeCtx, cancel := context.WithTimeout(context.Background(), startupCloseTimeout)
		defer cancel()
		if closeErr := r.Close(closeCtx); closeErr != nil {
			return fmt.Errorf("startup status failed and close failed: %v: %w", closeErr, err)
		}
		return err
	}

	r.logInfo("running startup reconcile", "target", r.appConfig.Agent.Target)
	if err := r.configureService.Reconcile(ctx, r.appConfig.Agent.Target); err != nil {
		r.logWarn("startup reconcile completed with error", "target", r.appConfig.Agent.Target, "error", err)
	} else {
		r.logInfo("startup reconcile completed", "target", r.appConfig.Agent.Target)
	}

	return nil
}

// Close stops the agentcore client lifecycle.
func (r *Runtime) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("close context is nil")
	}
	if r.markClosed() {
		return nil
	}

	r.cancel()

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// all background goroutines finished cleanly
	case <-ctx.Done():
		r.logWarn("close context timed out or was cancelled before background tasks finished", "error", ctx.Err())
		return fmt.Errorf("close: %w", ctx.Err())
	}

	r.logInfo("agentcore client closing", "target", r.appConfig.Agent.Target)
	if err := r.client.Close(ctx); err != nil {
		return fmt.Errorf("close agentcore client: %w", err)
	}

	closedHealth := r.client.Health()
	r.logInfo("agentcore client closed", healthLogFields(closedHealth)...)
	return nil
}

func healthLogFields(h agentcore.HealthSnapshot) []any {
	return []any{
		"health_state", h.State,
		"connected_url", h.ConnectedURL,
		"jetstream_ready", h.JetStreamReady,
		"kv_ready", h.KVReady,
		"registered_subscriptions", h.RegisteredSubscriptions,
		"active_subscriptions", h.ActiveSubscriptions,
		"last_error", h.LastError,
	}
}
