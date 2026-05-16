package agent

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/routerarchitects/nats-agent-core/agentcore"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/config"
)

type slogAdapter struct {
	logger *slog.Logger
}

func (l *slogAdapter) Debug(msg string, kv ...any) { l.logger.Debug(msg, kv...) }
func (l *slogAdapter) Info(msg string, kv ...any)  { l.logger.Info(msg, kv...) }
func (l *slogAdapter) Warn(msg string, kv ...any)  { l.logger.Warn(msg, kv...) }
func (l *slogAdapter) Error(msg string, kv ...any) { l.logger.Error(msg, kv...) }

// NewLogger creates a structured agent/logger adapter for runtime and agentcore.
func NewLogger(cfg config.LoggingConfig, output io.Writer) (agentcore.Logger, error) {
	var level slog.Level
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		return nil, fmt.Errorf("unsupported logging level %q", cfg.Level)
	}

	handlerOptions := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	switch cfg.Format {
	case "text":
		handler = slog.NewTextHandler(output, handlerOptions)
	case "json":
		handler = slog.NewJSONHandler(output, handlerOptions)
	default:
		return nil, fmt.Errorf("unsupported logging format %q", cfg.Format)
	}

	return &slogAdapter{
		logger: slog.New(handler).With("component", "vyos-nats-agent"),
	}, nil
}

func (r *Runtime) logDebug(msg string, kv ...any) {
	if r.logger == nil {
		return
	}
	r.logger.Debug(msg, kv...)
}

func (r *Runtime) logInfo(msg string, kv ...any) {
	if r.logger == nil {
		return
	}
	r.logger.Info(msg, kv...)
}

func (r *Runtime) logWarn(msg string, kv ...any) {
	if r.logger == nil {
		return
	}
	r.logger.Warn(msg, kv...)
}

func (r *Runtime) logError(msg string, kv ...any) {
	if r.logger == nil {
		return
	}
	r.logger.Error(msg, kv...)
}
