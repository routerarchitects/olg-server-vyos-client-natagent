# PROMPTS/04-action-flow-placeholder-trace.md

# Phase 4: Action Flow With Placeholder Trace Executor

## Objective

Read the current branch carefully before editing:

- `README.md`
- `SPEC.md`
- `config.example.yaml`
- `cmd/vyos-nats-agent/main.go`
- all files under `internal/config`
- all files under `internal/agent`
- all files under `internal/configure`
- all files under `internal/renderer`
- all files under `internal/apply`
- all files under `internal/state`
- existing smoke scripts under `tests/scripts`
- `go.mod`

Implement **Phase 4** of `vyos-nats-agent`: replace the Phase 2 action-handler `not_implemented` placeholder with a real action workflow for one supported action: `trace`.

This phase is about proving the action lifecycle:

```text
Action command received
  -> validate action is enabled and supported
  -> publish action status: received/running
  -> execute placeholder trace executor
  -> publish action status: completed
  -> publish action result: success
```

Do **not** implement real VyOS trace, rtty, shell execution, network probing, or platform-specific command execution in this phase.

The goal is to complete the agent-owned action workflow boundary while keeping the real action executor replaceable later.

---

## Current Baseline

By now the repository should already have:

```text
Phase 1:
  - YAML config loader
  - defaults/validation
  - conversion to agentcore.Config

Phase 2:
  - agentcore.New(...)
  - handler registration
  - Start(ctx)
  - startup status
  - SIGINT/SIGTERM shutdown

Phase 3:
  - configure service
  - LoadDesiredConfig(ctx, target)
  - placeholder renderer
  - placeholder apply engine
  - local applied UUID state
  - configure status/result publishing
```

The current action handler may still publish:

```text
failure / not_implemented
```

This phase must replace only the action placeholder behavior.

The configure flow should remain Phase 3 behavior. Do not regress it.

---

## Hard Rules

1. Use `github.com/routerarchitects/nats-agent-core/agentcore` as the only bus-facing library.
2. Do not import `github.com/nats-io/nats.go` directly.
3. Do not implement raw NATS publish/subscribe logic.
4. Do not manually compose NATS subjects.
5. Do not implement custom KV logic.
6. Use `client.PublishStatus(ctx, ...)` for action status.
7. Use `client.PublishResult(ctx, ...)` for action result.
8. Do not duplicate `nats-agent-core` subject/session/transport behavior.
9. Do not call internal packages from `nats-agent-core`.
10. Do not implement real VyOS trace.
11. Do not implement rtty.
12. Do not execute shell commands.
13. Do not open network sockets for actual trace behavior.
14. Do not log action payload contents.
15. Do not log secrets or full config.
16. Keep code small, idiomatic, testable, and easy to review.

---

## Required New Package

Add only the package needed for Phase 4 action workflow:

```text
internal/actions/
  service.go
  types.go
  trace.go
  placeholder_trace.go
```

Use package name:

```go
package actions
```

Do not add:

```text
internal/nats
internal/kv
internal/transport
internal/vyos
internal/shell
internal/commands
```

Those are out of scope.

---

## Action Workflow

Replace the current `Runtime.handleAction` implementation with a call into an action service.

Target behavior:

```text
1. Log action handler invoked.
2. Publish action status: received.
3. Validate msg.Action is enabled in config.
4. Validate msg.Action is supported by the service.
5. Publish action status: executing.
6. Execute placeholder trace executor.
7. Publish action status: completed.
8. Publish action result: success with deterministic placeholder output payload.
9. Return nil.
```

Failure behavior:

```text
- On any failure, publish action status: failed.
- Publish action result: failure.
- Include a stable error_code.
- Return the original/wrapped error.
```

Do not update configure state file in action flow.

Do not touch renderer/apply/state packages.

---

## Suggested Runtime Wiring

Update `internal/agent/agent.go` so `Runtime` owns an action workflow dependency.

Suggested field:

```go
type Runtime struct {
    appConfig  *config.AppConfig
    coreConfig agentcore.Config
    client     *agentcore.Client
    logger     agentcore.Logger
    now        func() time.Time

    configureService *configure.Service
    actionService    *actions.Service

    mu      sync.Mutex
    started bool
    closed  bool
}
```

After `agentcore.New(...)`, create:

```go
traceExecutor := actions.NewPlaceholderTraceExecutor()
actionService, err := actions.NewService(actions.Dependencies{
    Client:  client,
    Logger:  options.logger,
    Now:     options.now,
    Enabled: appCfg.Agent.Actions.Enabled,
    Executors: map[string]actions.Executor{
        "trace": traceExecutor,
    },
})
if err != nil {
    return nil, fmt.Errorf("create action service: %w", err)
}
```

Keep this wiring simple.

---

## Action Service API

Create `internal/actions/service.go`.

Suggested shape:

```go
package actions

type Service struct {
    client    AgentCoreClient
    logger    agentcore.Logger
    now       func() time.Time
    enabled   map[string]struct{}
    executors map[string]Executor
}

type Dependencies struct {
    Client    AgentCoreClient
    Logger    agentcore.Logger
    Now       func() time.Time
    Enabled   []string
    Executors map[string]Executor
}

func NewService(deps Dependencies) (*Service, error)

func (s *Service) Handle(ctx context.Context, msg agentcore.ActionCommand) error
```

Use narrow interfaces:

```go
type AgentCoreClient interface {
    PublishStatus(ctx context.Context, msg agentcore.StatusEnvelope) error
    PublishResult(ctx context.Context, msg agentcore.ResultEnvelope) error
}

type Executor interface {
    Execute(ctx context.Context, msg agentcore.ActionCommand) (Output, error)
}
```

Suggested output type:

```go
type Output struct {
    Payload json.RawMessage
    Message string
}
```

---

## Trace Executor API

Create `internal/actions/trace.go`.

Suggested interface:

```go
type TraceExecutor interface {
    Execute(ctx context.Context, msg agentcore.ActionCommand) (Output, error)
}
```

If this duplicates the generic `Executor`, keep only the generic `Executor` unless the named interface improves readability.

Keep the API small.

---

## Placeholder Trace Executor

Create `internal/actions/placeholder_trace.go`.

Behavior:

```text
- Validate context is not nil and not canceled.
- Validate msg.Target is not empty.
- Validate msg.Action == trace.
- Validate msg.RPCID is not empty.
- Validate msg.Payload is valid JSON.
- Do not execute real trace.
- Do not open sockets.
- Do not run shell commands.
- Do not log raw payload.
- Return deterministic JSON output.
```

Suggested output payload:

```json
{
  "executor": "placeholder_trace",
  "action": "trace",
  "target": "vyos",
  "status": "completed",
  "placeholder": true
}
```

Use the actual message target for `target`.

It is okay to include safe metadata such as:

```json
{
  "received_payload": true
}
```

Do not echo the raw input payload in the result.

---

## Status Publishing

Publish action statuses using `agentcore.StatusEnvelope`.

Recommended stages:

```text
received
executing
completed
failed
```

Examples:

```go
agentcore.StatusEnvelope{
    Version:   "1.0",
    RPCID:     msg.RPCID,
    Target:    msg.Target,
    Status:    "running",
    Stage:     "executing",
    Message:   "executing placeholder trace action",
    Timestamp: now().UTC(),
}
```

Success status:

```text
status = success
stage = completed
message = placeholder trace action completed
```

Failure status:

```text
status = failure
stage = failed
message = action processing failed
```

Do not include raw action payload in status.

---

## Result Publishing

Publish final action result using `agentcore.ResultEnvelope`.

Success result:

```go
agentcore.ResultEnvelope{
    Version:     "1.0",
    RPCID:       msg.RPCID,
    Target:      msg.Target,
    CommandType: "action",
    Action:      msg.Action,
    Result:      "success",
    Message:     "placeholder trace action completed",
    Payload:     output.Payload,
    Timestamp:   now().UTC(),
}
```

Failure result:

```text
result = failure
error_code = stable code
message = short safe message
```

Suggested error codes:

```text
unsupported_action
disabled_action
invalid_action_payload
action_execute_failed
status_publish_failed
result_publish_failed
```

Do not include raw action input payload in result.

---

## Logging

Keep the Phase 2/3 logging style.

Add action workflow logs:

```text
action received
action executing
action completed
action result publishing
action result published
action failed
```

Use fields:

```text
target
action
rpc_id
stage
status
error
```

Do not log:

```text
action payload
executor output payload
secrets
full config
```

---

## Handler Changes

Update `internal/agent/handlers.go`.

Current action handler may publish `not_implemented`.

Replace it with:

```go
func (r *Runtime) handleAction(ctx context.Context, msg agentcore.ActionCommand) error {
    r.logInfo("action handler invoked", "target", msg.Target, "action", msg.Action, "rpc_id", msg.RPCID)
    if r.actionService == nil {
        return fmt.Errorf("action service is not initialized")
    }
    return r.actionService.Handle(ctx, msg)
}
```

Do not change configure handler except for compile-related dependency wiring.

---

## Enabled Actions

The service must respect:

```yaml
agent:
  actions:
    enabled:
      - trace
```

Rules:

```text
- If msg.Action is not in enabled list, publish failure result with error_code disabled_action.
- If msg.Action has no registered executor, publish failure result with error_code unsupported_action.
- For Phase 4, only trace is supported.
```

Config validation should already reject unsupported configured actions. Keep that behavior.

---

## Context Rules

Follow standard Go context practices:

```text
- context.Context is the first parameter for blocking operations.
- Do not store caller contexts in structs.
- Reject nil context in action service/executor methods.
- Return promptly if context is canceled.
- Use ctx for PublishStatus, PublishResult, and Executor.Execute.
```

Do not use nil contexts.

---

## Error Handling Rules

Use simple Go error wrapping:

```go
fmt.Errorf("execute action %q: %w", msg.Action, err)
```

Failure handling:

```text
- publish failure status/result when possible
- return wrapped original error after publishing failure result
- if failure result publishing also fails, return errors.Join(originalErr, resultErr)
```

Do not panic.

Do not swallow errors from:

```text
PublishStatus
PublishResult
Executor.Execute
```

---

## Smoke Script Requirement

Add a Phase 4 real-NATS action smoke script:

```text
tests/scripts/phase4-real-nats-action-smoke.sh
```

The smoke script should:

```text
1. Start real nats-server -js.
2. Start controller using nats-agent-core.
3. Start vyos-nats-agent.
4. Observe startup status.
5. SubmitAction for trace through nats-agent-core.
6. Observe action statuses.
7. Observe final action result:
   - result = success
   - command_type = action
   - action = trace
   - rpc_id preserved
   - payload.placeholder = true
   - payload.executor = placeholder_trace
8. Stop agent with SIGINT.
9. Verify graceful shutdown.
```

Important:

```text
- Do not use raw NATS publish for the primary test flow.
- Controller must use public nats-agent-core APIs.
- Do not kill existing nats-server by default.
- If port is busy, fail with message suggesting NATS_PORT=4223.
- Only kill existing NATS if explicitly requested by env var.
- Prefer waiting for NATS log line "Server is ready" over raw /dev/tcp probes to avoid parser ERROR noise.
```

Do not keep stale scripts in `tests/scripts` that assert old action `not_implemented` behavior after Phase 4.

---

## Docs Updates

Update `README.md` and `SPEC.md` where necessary.

Keep changes focused.

Document:

```text
- trace action now uses placeholder executor
- action handler now publishes action status/result
- action result is success for valid trace action
- placeholder trace does not run real VyOS/network commands
- real trace/rtty execution remains out of scope
```

Do not rewrite the full docs.

Update any Phase 4 checklist/roadmap if present.

---

## Config Updates

No new config keys are required.

Keep using:

```yaml
agent:
  actions:
    enabled:
      - trace
```

Do not add real trace command config yet.

Do not add VyOS command paths yet.

---

## Unit Tests

If the repository already has unit tests for similar service packages, add small tests for the action service and placeholder trace executor.

At minimum, if time permits, add tests for:

```text
- valid trace action returns success output
- disabled action returns failure result
- unsupported action returns failure result
- invalid JSON payload fails safely
```

Do not build a large test framework in this phase.

The smoke script is required.

---

## Verification Commands

Run:

```bash
gofmt -w <changed go files>
go build ./...
go test ./...
go run ./cmd/vyos-nats-agent --config ./config.example.yaml --validate-config
go run ./cmd/vyos-nats-agent --config ./config.example.yaml --print-effective-config --validate-config
```

If local `nats-server` is available, run:

```bash
./tests/scripts/phase3-real-nats-configure-smoke.sh
./tests/scripts/phase4-real-nats-action-smoke.sh
```

Do not claim a command passed unless it was actually run.

If real-NATS smoke tests cannot be run because `nats-server` is unavailable, state that clearly.

---

## Expected Files Added

Likely new files:

```text
internal/actions/service.go
internal/actions/types.go
internal/actions/trace.go
internal/actions/placeholder_trace.go
tests/scripts/phase4-real-nats-action-smoke.sh
```

Likely changed files:

```text
internal/agent/agent.go
internal/agent/handlers.go
README.md
SPEC.md
```

Only update other files if required for compile or focused documentation accuracy.

---

## Acceptance Criteria

- [ ] Action handler no longer returns `not_implemented` for valid trace commands.
- [ ] Action handler delegates to an action service.
- [ ] Action service validates action is enabled.
- [ ] Action service dispatches `trace` to placeholder trace executor.
- [ ] Placeholder trace executor does not execute real commands.
- [ ] Placeholder trace executor returns deterministic JSON output.
- [ ] Action status messages are published.
- [ ] Final action result is published.
- [ ] Valid trace action result is `success`.
- [ ] Unsupported/disabled actions publish failure result where possible.
- [ ] No configure flow regression.
- [ ] No raw NATS code is added.
- [ ] No custom KV code is added.
- [ ] No real VyOS action execution is added.
- [ ] `go build ./...` succeeds.
- [ ] `go test ./...` succeeds.
- [ ] Phase 4 real-NATS smoke script passes when `nats-server` is available.

---

## Final Response Required From Codex

After implementation, summarize:

```text
1. Files added
2. Files changed
3. How action handler changed
4. How enabled-action validation works
5. How placeholder trace executor works
6. Status/result messages published
7. Failure handling behavior
8. Smoke script behavior
9. Documentation updates
10. What remains intentionally out of scope
11. Commands run and results
12. Whether real-NATS Phase 4 smoke was run
```

Also explicitly confirm:

```text
- no raw NATS code was added
- no custom KV code was added
- no real VyOS trace was added
- no shell command execution was added
- configure flow remains Phase 3 behavior
- nats-agent-core remains the only bus-facing implementation
```
