# PROMPTS/02-agent-lifecycle.md

# Phase 2: Agent Lifecycle Using nats-agent-core

## Objective

Read the current repository carefully before editing:

- `README.md`
- `SPEC.md`
- `config.example.yaml`
- `cmd/vyos-nats-agent/main.go`
- all files under `internal/config`
- `go.mod`

Implement **Phase 2** of `vyos-nats-agent`.

This phase wires the Phase 1 YAML config loader into a minimal long-running daemon lifecycle using only the public `github.com/routerarchitects/nats-agent-core/agentcore` API.

This phase is about:

```text
config.yaml
  -> internal/config.AppConfig
  -> agentcore.Config
  -> agentcore.New(...)
  -> register configure/action handlers
  -> Start(ctx)
  -> publish startup status
  -> wait for SIGINT/SIGTERM
  -> graceful Close(ctx)
```

Do **not** implement configure apply, desired-config loading, renderer integration, apply engine, local state, startup reconcile, real action execution, or integration tests in this phase.

The goal is to prove that the VyOS agent can start, connect to the NATS bus through `nats-agent-core`, register handlers, publish startup status, and shut down cleanly.

---

## Current Phase 1 Baseline

Phase 1 already provides:

```text
cmd/vyos-nats-agent/main.go
internal/config/config.go
internal/config/defaults.go
internal/config/loader.go
internal/config/validate.go
internal/config/convert.go
internal/config/print.go
internal/config/redact.go
config.example.yaml
```

Do not rewrite Phase 1 unnecessarily.

Keep existing `--config`, `--validate-config`, and `--print-effective-config` behavior.

When `--validate-config` is used, the process should still validate config and exit without starting NATS.

When `--validate-config` is not used, the process should start the Phase 2 agent lifecycle.

---

## Hard Rules

1. Use `github.com/routerarchitects/nats-agent-core/agentcore` as the only NATS lifecycle, subscription, publish, status, and handler-registration library.
2. Do not import `github.com/nats-io/nats.go` directly in this agent.
3. Do not implement raw NATS connection logic.
4. Do not implement raw NATS publish/subscribe logic.
5. Do not construct raw NATS subjects manually.
6. Do not implement custom KV access.
7. Do not duplicate any `nats-agent-core` subject, session, transport, or registry logic.
8. Do not call internal packages from `nats-agent-core`.
9. Use only the public `agentcore` package.
10. Keep handlers thin.
11. Do not implement real VyOS rendering or config apply.
12. Do not implement real `trace` execution.
13. Do not introduce any platform-specific command execution.
14. Do not log secrets or full config values.
15. Keep code small, idiomatic, and easy to review.

---

## Relevant nats-agent-core Public API

Use the public `agentcore` APIs already available from the library:

```go
agentcore.New(cfg, opts...)
client.RegisterConfigureHandler(target, handler, opts...)
client.RegisterActionHandler(target, action, handler, opts...)
client.Start(ctx)
client.Close(ctx)
client.Health()
client.PublishStatus(ctx, msg)
client.PublishResult(ctx, msg)
agentcore.WithLogger(logger)
agentcore.WithErrorSink(func(error))
```

Do not use raw NATS APIs for any of this.

The `nats-agent-core` library is not a daemon. It is meant to be used inside long-running agents. The VyOS agent owns daemon lifecycle and local business behavior, while the library owns bus-facing behavior.

---

## Required Directory Structure

Add only the minimal Phase 2 structure:

```text
cmd/
  vyos-nats-agent/
    main.go

internal/
  agent/
    agent.go
    handlers.go
    lifecycle.go
    logging.go
    status.go

internal/config/
  existing Phase 1 files
```

Do not add these packages in Phase 2:

```text
internal/renderer
internal/apply
internal/actions
internal/state
internal/transport
internal/nats
internal/kv
```

Those are later-phase or library-owned concerns.

---

## Logging Requirements

The agent should use the `agentcore.Logger` hook so library logs are visible when logging is enabled.

Implement a small standard-library logger adapter in:

```text
internal/agent/logging.go
```

Use Go standard library logging only. Prefer `log/slog`.

The logger adapter must satisfy:

```go
Debug(msg string, kv ...any)
Info(msg string, kv ...any)
Warn(msg string, kv ...any)
Error(msg string, kv ...any)
```

Pass it into the library using:

```go
agentcore.WithLogger(logger)
```

Also pass an error sink:

```go
agentcore.WithErrorSink(func(err error) {
    logger.Error("agentcore async error", "error", err)
})
```

### Logging config

Add optional agent logging config to the YAML model:

```yaml
agent:
  logging:
    enabled: true
    level: info
    format: text
```

Update:

```text
internal/config/config.go
internal/config/defaults.go
internal/config/validate.go
config.example.yaml
README.md or SPEC.md only if necessary
```

Supported values:

```text
enabled: true|false
level: debug|info|warn|error
format: text|json
```

Defaults:

```text
agent.logging.enabled = true
agent.logging.level = info
agent.logging.format = text
```

If logging is disabled, do not pass a logger into `agentcore.New`.

If logging is enabled:
- create the standard-library logger adapter
- pass it to `agentcore.WithLogger`
- use it for agent lifecycle logs
- pass it to `agentcore.WithErrorSink`

### Required log events

Log these events with structured key/value fields:

```text
- config loaded
- agentcore config prepared
- agentcore client created
- configure handler registered
- action handler registered
- agentcore client starting
- agentcore client started
- startup status publishing
- startup status published
- configure handler invoked
- configure handler placeholder result publishing
- action handler invoked
- action handler placeholder result publishing
- shutdown requested
- agentcore client closing
- agentcore client closed
- shutdown error, if any
```

Use fields such as:

```text
target
action
rpc_id
uuid
stage
status
health_state
error
```

Never log:

```text
password
token
credential file contents
NKey seed contents
JWT contents
full config dump
payload body
```

Logging should help trace the lifecycle, but must not expose secrets or payloads.

---

## Main CLI Behavior

Keep existing flags:

```text
--config <path>
--validate-config
--print-effective-config
--help
```

Behavior:

### `--validate-config`

Same as Phase 1:

```text
- load config
- validate config
- convert to agentcore.Config
- optionally print sanitized effective config
- print "configuration valid"
- exit without creating or starting the agent runtime
```

### Normal run without `--validate-config`

New Phase 2 behavior:

```text
1. Resolve and load config.
2. Convert to agentcore.Config.
3. Create logger if enabled.
4. Create internal agent runtime.
5. Install SIGINT/SIGTERM signal handling.
6. Start agent runtime.
7. Wait for signal/context cancellation.
8. Gracefully close agent runtime.
9. Exit 0 on clean shutdown.
10. Exit non-zero on startup/shutdown errors.
```

Use:

```go
signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
```

`context.Background()` is acceptable here because `main` is an application boundary.

Do not use `context.Background()` deep inside library operations unless there is no caller context available.

---

## Agent Runtime Design

Create an internal agent package.

Suggested public-internal shape:

```go
package agent

type Runtime struct {
    appConfig  *config.AppConfig
    coreConfig agentcore.Config
    client     *agentcore.Client
    logger     agentcore.Logger
    now        func() time.Time
}

func New(appCfg *config.AppConfig, coreCfg agentcore.Config, opts ...Option) (*Runtime, error)

func (r *Runtime) Run(ctx context.Context) error
func (r *Runtime) Start(ctx context.Context) error
func (r *Runtime) Close(ctx context.Context) error
```

You may adjust names if needed, but keep the structure small.

The runtime should own:
- the `agentcore.Client`
- handler registration
- startup status publication
- lifecycle logging
- graceful shutdown

The runtime must not own:
- renderer
- apply engine
- local applied UUID state
- real action executor
- raw NATS connection

---

## agentcore.Client Creation

Use the Phase 1 config conversion:

```go
coreCfg, err := appCfg.ToAgentCoreConfig()
```

Create the client with:

```go
client, err := agentcore.New(coreCfg, options...)
```

Where options may include:

```go
agentcore.WithLogger(logger)
agentcore.WithErrorSink(...)
```

Do not call `agentcore.New` in config validation mode.

Do not call `agentcore.New` just to validate config.

---

## Handler Registration

Register handlers before `Start(ctx)`.

Use the configured target:

```go
target := appCfg.Agent.Target
```

Configure handler:

```go
client.RegisterConfigureHandler(target, runtime.handleConfigure)
```

Action handlers:

For each enabled action in:

```go
appCfg.Agent.Actions.Enabled
```

register the action handler.

For Phase 2 the only supported action is still:

```text
trace
```

Use:

```go
client.RegisterActionHandler(target, "trace", runtime.handleAction)
```

Do not register result/status handlers in this phase unless they are required for compile or minimal lifecycle behavior.

Do not manually compose subjects.

---

## Start Lifecycle

`Runtime.Start(ctx)` should:

```text
1. Log starting.
2. Register handlers if not already registered.
3. Call client.Start(ctx).
4. Log health state from client.Health().
5. Publish startup status.
6. Log startup status success.
```

If handler registration fails, return the error.

If `client.Start(ctx)` fails, return the error.

If startup status publication fails:
- log the error
- close the client with a short shutdown context
- return the error

---

## Run Lifecycle

`Runtime.Run(ctx)` should:

```text
1. Call Start(ctx).
2. Wait for ctx.Done().
3. Log shutdown requested.
4. Call Close(shutdownCtx).
5. Return close error if close fails.
```

Use the configured shutdown timeout from:

```go
coreCfg.Timeouts.ShutdownTimeout
```

If shutdown timeout is zero, use a safe fallback:

```text
10s
```

---

## Close Lifecycle

`Runtime.Close(ctx)` should:

```text
1. Log closing.
2. Call client.Close(ctx).
3. Log closed health state.
4. Return any close error.
```

Do not ignore close errors.

---

## Startup Status Publication

After successful `client.Start(ctx)`, publish a startup/running status using:

```go
client.PublishStatus(ctx, agentcore.StatusEnvelope{...})
```

Suggested fields:

```go
agentcore.StatusEnvelope{
    Version:   "1.0",
    Target:    appCfg.Agent.Target,
    Status:    "running",
    Stage:     "startup",
    Message:   "vyos-nats-agent started",
    Timestamp: now().UTC(),
}
```

Use a constant for the wire contract version:

```go
const wireVersion = "1.0"
```

Do not use the binary/app version as the wire contract version.

If `PublishStatus` fails, fail startup.

---

## Configure Handler Placeholder

The configure handler must be registered and callable, but must not apply configuration.

Handler signature should match `agentcore.ConfigureHandler`:

```go
func (r *Runtime) handleConfigure(ctx context.Context, msg agentcore.ConfigureNotification) error
```

For Phase 2:

```text
1. Log handler invocation with target, rpc_id, uuid.
2. Publish a result that clearly says configure apply is not implemented in Phase 2.
3. Return publish error if result publication fails.
```

Use:

```go
client.PublishResult(ctx, agentcore.ResultEnvelope{...})
```

Suggested result:

```go
agentcore.ResultEnvelope{
    Version:     "1.0",
    RPCID:       msg.RPCID,
    Target:      msg.Target,
    CommandType: "configure",
    UUID:        msg.UUID,
    Result:      "failure",
    ErrorCode:   "not_implemented",
    Message:     "configure apply is not implemented in Phase 2",
    Timestamp:   now().UTC(),
}
```

Do not call:

```text
LoadDesiredConfig
StartupReconcile
renderer
apply engine
state store
```

This handler is only a lifecycle placeholder.

---

## Action Handler Placeholder

Handler signature should match `agentcore.ActionHandler`:

```go
func (r *Runtime) handleAction(ctx context.Context, msg agentcore.ActionCommand) error
```

For Phase 2:

```text
1. Log handler invocation with target, action, rpc_id.
2. Publish a result that clearly says action execution is not implemented in Phase 2.
3. Return publish error if result publication fails.
```

Use:

```go
client.PublishResult(ctx, agentcore.ResultEnvelope{...})
```

Suggested result:

```go
agentcore.ResultEnvelope{
    Version:     "1.0",
    RPCID:       msg.RPCID,
    Target:      msg.Target,
    CommandType: "action",
    Action:      msg.Action,
    Result:      "failure",
    ErrorCode:   "not_implemented",
    Message:     "action execution is not implemented in Phase 2",
    Timestamp:   now().UTC(),
}
```

Do not execute trace.

Do not parse or log action payload.

---

## Context Rules

Follow idiomatic Go context behavior:

```text
- context.Context should be the first parameter for blocking/runtime methods.
- Do not store caller contexts in structs.
- main may create the root signal context.
- pass ctx to Start, PublishStatus, PublishResult, and Close.
- derive shutdown context with timeout and call cancel().
- return promptly when ctx is canceled.
```

Do not use nil contexts.

If a nil context can reach public/internal runtime methods, reject it with a clear error.

---

## Error Handling Rules

Use simple Go error wrapping at the agent layer:

```go
fmt.Errorf("start agentcore client: %w", err)
```

Do not panic for normal runtime errors.

Do not swallow errors from:

```text
agentcore.New
RegisterConfigureHandler
RegisterActionHandler
Start
PublishStatus
PublishResult
Close
```

Do not log and return the same error at many layers. Prefer:
- lower layer returns error with context
- top-level logs once and exits

Handler-level publish failures should be logged because they happen inside callback execution and are operationally useful.

---

## Health Logging

After start and after close, call:

```go
client.Health()
```

Log at least:

```text
health_state
connected_url
jetstream_ready
kv_ready
registered_subscriptions
active_subscriptions
last_error
```

Do not add custom health model. Use the library health snapshot.

---

## What Must Stay Out of Scope

Do not implement:

```text
- LoadDesiredConfig in configure handler
- StartupReconcile
- renderer package
- apply package
- actions package
- state package
- local applied_uuid tracking
- actual VyOS command execution
- actual trace execution
- rollback/discard
- event publishing
- metrics implementation
- integration tests
- raw NATS code
- custom subject building
- custom KV access
```

The only result/status publishing in this phase should be:
- startup status
- configure-not-implemented result
- action-not-implemented result

---

## Config Updates Required

Update `config.example.yaml` to include logging:

```yaml
agent:
  logging:
    enabled: true
    level: info
    format: text
```

Update config model/default/validation accordingly.

Keep the existing YAML keys backward-compatible where possible. Existing config without `agent.logging` should still work by applying defaults.

---

## Main Program Expected Behavior

### Validate only

Command:

```bash
go run ./cmd/vyos-nats-agent --config ./config.example.yaml --validate-config
```

Expected behavior:

```text
configuration valid
```

No NATS connection should be attempted.

### Run daemon

Command:

```bash
go run ./cmd/vyos-nats-agent --config ./config.example.yaml
```

Expected behavior:

```text
- load config
- create agentcore client
- register handlers
- connect to NATS through agentcore.Start(ctx)
- publish startup status
- wait until SIGINT/SIGTERM
- close gracefully
```

If NATS is not running, startup may fail with a clear error from `agentcore.Start(ctx)`. Do not hide that failure.

---

## Verification Commands

Run:

```bash
gofmt -w <changed go files>
go build ./...
go test ./...
go run ./cmd/vyos-nats-agent --config ./config.example.yaml --validate-config
```

If a local NATS server is available, also run:

```bash
go run ./cmd/vyos-nats-agent --config ./config.example.yaml
```

Then stop it with Ctrl+C and confirm graceful shutdown logs.

Do not claim a command passed unless it was actually run.

If the daemon run cannot be verified because NATS is unavailable, say so explicitly.

---

## Final Response Required From Codex

After implementation, summarize:

```text
1. Files added
2. Files changed
3. How agentcore.New is used
4. How logger is wired into agentcore.WithLogger
5. How error sink is wired
6. How configure handler registration works
7. How action handler registration works
8. How startup status publishing works
9. How signal handling works
10. How graceful shutdown works
11. What remains intentionally out of scope
12. Commands run and results
13. Whether NATS runtime was manually verified or skipped
```

Also explicitly confirm:

```text
- no raw NATS code was added
- no custom subject building was added
- no custom KV code was added
- no renderer/apply/state/action execution was added
- nats-agent-core remains the only bus-facing implementation
```
