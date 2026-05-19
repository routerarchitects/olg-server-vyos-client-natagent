# PROMPTS/03-configure-flow-placeholders.md

# Phase 3: Configure Flow With Placeholders

## Objective

Read the current `main` branch carefully before editing:

- `README.md`
- `SPEC.md`
- `config.example.yaml`
- `cmd/vyos-nats-agent/main.go`
- all files under `internal/config`
- all files under `internal/agent`
- `tests/scripts/phase3-real-nats-configure-smoke.sh`
- `go.mod`

Implement **Phase 3** of `vyos-nats-agent`: replace the Phase 2 configure-handler `not_implemented` placeholder with a real configure workflow that still uses placeholder renderer/apply implementations.

This phase is about proving the configure lifecycle:

```text
Configure notification received
  -> LoadDesiredConfig(ctx, target) using nats-agent-core
  -> compare desired UUID with local applied UUID state
  -> render desired config using placeholder renderer
  -> apply rendered config using placeholder apply engine
  -> update local applied UUID state after successful apply
  -> publish configure status/result
```

Do **not** implement real VyOS rendering or real VyOS apply commands in this phase.

The goal is to complete the agent-owned configure workflow boundaries while keeping platform-specific renderer/apply logic replaceable later.

---

## Current Phase 2 Baseline

Phase 2 already provides:

```text
- YAML config loading
- conversion to agentcore.Config
- agentcore.New(...)
- configure/action handler registration
- Start(ctx)
- startup status publication
- signal handling
- graceful Close(ctx)
- placeholder configure result
- placeholder action result
- real-NATS smoke scripts
```

The current configure handler publishes:

```text
failure / not_implemented
```

This phase must replace only the configure placeholder behavior.

The action handler should remain Phase 2 behavior:

```text
failure / not_implemented
```

until Phase 4.

---

## Hard Rules

1. Use `github.com/routerarchitects/nats-agent-core/agentcore` as the only bus-facing library.
2. Do not import `github.com/nats-io/nats.go` directly.
3. Do not implement raw NATS publish/subscribe logic.
4. Do not manually compose NATS subjects.
5. Do not implement custom KV access.
6. Use `client.LoadDesiredConfig(ctx, target)` for loading desired config.
7. Use `client.PublishStatus(ctx, ...)` for configure status.
8. Use `client.PublishResult(ctx, ...)` for configure result.
9. Do not duplicate `nats-agent-core` subject/session/KV/transport behavior.
10. Do not call internal packages from `nats-agent-core`.
11. Do not implement real VyOS renderer repo integration.
12. Do not implement real VyOS CLI/config command execution.
13. Do not update local applied UUID state until placeholder apply succeeds.
14. Do not log desired payload contents.
15. Do not log secrets or full config.
16. Keep code idiomatic, small, and easy to review.

---

## Required New Packages

Add only the packages needed for Phase 3:

```text
internal/configure/
  service.go
  types.go

internal/renderer/
  renderer.go
  placeholder.go

internal/apply/
  engine.go
  placeholder.go

internal/state/
  store.go
  file_store.go
```

Do not add real VyOS packages yet.

Do not add `internal/nats`, `internal/kv`, `internal/transport`, or custom bus packages.

---

## Configure Workflow

Replace the current `Runtime.handleConfigure` implementation with a call into a configure service.

Target behavior:

```text
1. Log configure handler invoked.
2. Publish configure status: received.
3. Load desired config using r.client.LoadDesiredConfig(ctx, msg.Target).
4. Verify loaded desired config is not nil.
5. Verify desired record target matches msg.Target.
6. Verify desired record UUID matches msg.UUID.
7. Load local state from state file.
8. If local applied_uuid == desired UUID:
   - publish status: already_in_sync
   - publish result: success, message already applied
   - return nil
9. Render desired config using placeholder renderer.
10. Publish status: rendered.
11. Apply rendered config using placeholder apply engine.
12. Publish status: applied.
13. Save local state with applied_uuid and applied_at.
14. Publish configure result: success.
15. Return nil.
```

Failure behavior:

```text
- On any failure, publish configure status: failed.
- Publish configure result: failure.
- Include a stable error_code.
- Return the original/wrapped error.
- Do not update local state on failure.
```

---

## Suggested Runtime Wiring

Update `internal/agent/agent.go` so `Runtime` owns configure workflow dependencies.

Suggested fields:

```go
type Runtime struct {
    appConfig  *config.AppConfig
    coreConfig agentcore.Config
    client     *agentcore.Client
    logger     agentcore.Logger
    now        func() time.Time

    configureService *configure.Service

    mu      sync.Mutex
    started bool
    closed  bool
}
```

After `agentcore.New(...)`, create:

```go
stateStore := state.NewFileStore(appCfg.Agent.StateFile)
renderer := renderer.NewPlaceholder()
applyEngine := apply.NewPlaceholder()
configureService := configure.NewService(configure.Dependencies{
    Client:      client,
    StateStore:  stateStore,
    Renderer:    renderer,
    ApplyEngine: applyEngine,
    Logger:      options.logger,
    Now:         options.now,
})
```

Keep this wiring simple.

---

## Configure Service API

Create `internal/configure/service.go`.

Suggested shape:

```go
package configure

type Service struct {
    client      AgentCoreClient
    stateStore  StateStore
    renderer    Renderer
    applyEngine ApplyEngine
    logger      agentcore.Logger
    now         func() time.Time
}

type Dependencies struct {
    Client      AgentCoreClient
    StateStore  StateStore
    Renderer    Renderer
    ApplyEngine ApplyEngine
    Logger      agentcore.Logger
    Now         func() time.Time
}

func NewService(deps Dependencies) (*Service, error)

func (s *Service) Handle(ctx context.Context, msg agentcore.ConfigureNotification) error
```

Use narrow interfaces in the configure package so the service is easy to test later:

```go
type AgentCoreClient interface {
    LoadDesiredConfig(ctx context.Context, target string) (*agentcore.StoredDesiredConfig, error)
    PublishStatus(ctx context.Context, msg agentcore.StatusEnvelope) error
    PublishResult(ctx context.Context, msg agentcore.ResultEnvelope) error
}

type StateStore interface {
    Load(ctx context.Context) (state.State, error)
    Save(ctx context.Context, st state.State) error
}

type Renderer interface {
    Render(ctx context.Context, desired agentcore.StoredDesiredConfig) (renderer.Output, error)
}

type ApplyEngine interface {
    Apply(ctx context.Context, rendered renderer.Output) error
}
```

If import cycles occur, adjust package names/types cleanly. Prefer keeping shared domain structs small and acyclic.

---

## State Store

Create local JSON state file support.

Use the existing config field:

```yaml
agent:
  state_file: /var/lib/vyos-nats-agent/state.json
```

State file should be machine-written JSON.

Suggested struct:

```go
package state

type State struct {
    Target      string    `json:"target"`
    AppliedUUID string    `json:"applied_uuid"`
    AppliedAt   time.Time `json:"applied_at"`
}
```

Behavior:

```text
- Missing state file means empty state; do not fail startup/configure.
- Invalid JSON should fail configure.
- State directory should be created automatically on save.
- Save should be atomic:
  - write to temp file in same directory
  - fsync if simple/reasonable
  - rename temp file to final path
- File permission: 0644
- Directory permission: 0755
```

Do not write state until placeholder apply succeeds.

When saving state, set:

```text
target = msg.Target
applied_uuid = desired.Record.UUID
applied_at = now().UTC()
```

---

## Placeholder Renderer

Create:

```text
internal/renderer/renderer.go
internal/renderer/placeholder.go
```

Suggested output type:

```go
package renderer

type Output struct {
    Target string
    UUID   string
    Text   string
}
```

Renderer interface:

```go
type Renderer interface {
    Render(ctx context.Context, desired agentcore.StoredDesiredConfig) (Output, error)
}
```

Placeholder renderer behavior:

```text
- Validate context is not nil and not canceled.
- Validate desired.Record.Target is not empty.
- Validate desired.Record.UUID is not empty.
- Do not parse full VyOS schema.
- Do not perform real rendering.
- Return deterministic mock rendered text.
- Do not log payload.
```

Example rendered text:

```text
# placeholder vyos config
# target: vyos
# uuid: <uuid>
```

This placeholder exists only to prove the agent workflow boundary.

---

## Placeholder Apply Engine

Create:

```text
internal/apply/engine.go
internal/apply/placeholder.go
```

Apply interface:

```go
type Engine interface {
    Apply(ctx context.Context, rendered renderer.Output) error
}
```

Placeholder apply behavior:

```text
- Validate context is not nil and not canceled.
- Validate rendered.UUID is not empty.
- Validate rendered.Text is not empty.
- Do not execute shell commands.
- Do not touch VyOS config.
- Return nil on valid input.
```

This placeholder exists only to prove state update and result/status flow after successful apply.

---

## Status Publishing

Publish configure statuses through `agentcore.PublishStatus`.

Use `agentcore.StatusEnvelope`.

Use `wireVersion = "1.0"` from the agent package or define the same constant in configure package if needed.

Recommended status stages:

```text
received
loading_desired
already_in_sync
rendering
rendered
applying
applied
failed
```

Examples:

```go
agentcore.StatusEnvelope{
    Version:   "1.0",
    RPCID:     msg.RPCID,
    Target:    msg.Target,
    UUID:      msg.UUID,
    Status:    "running",
    Stage:     "loading_desired",
    Message:   "loading desired config",
    Timestamp: now().UTC(),
}
```

Final success status:

```text
status = success
stage = applied
message = placeholder configure apply completed
```

Already in sync status:

```text
status = success
stage = already_in_sync
message = desired config already applied
```

Failure status:

```text
status = failure
stage = failed
message = configure processing failed
```

Do not publish payload in status for this phase.

---

## Result Publishing

Publish configure result through `agentcore.PublishResult`.

Success result:

```go
agentcore.ResultEnvelope{
    Version:     "1.0",
    RPCID:       msg.RPCID,
    Target:      msg.Target,
    CommandType: "configure",
    UUID:        msg.UUID,
    Result:      "success",
    Message:     "placeholder configure apply completed",
    Timestamp:   now().UTC(),
}
```

Already applied result:

```text
result = success
message = desired config already applied
```

Failure result:

```text
result = failure
error_code = stable code
message = short safe message
```

Suggested error codes:

```text
load_desired_failed
desired_config_missing
desired_target_mismatch
desired_uuid_mismatch
state_load_failed
render_failed
apply_failed
state_save_failed
status_publish_failed
result_publish_failed
```

Do not include raw payload in result.

---

## Logging

Keep the Phase 2 logging style.

Add configure workflow logs:

```text
configure desired loading
configure desired loaded
configure state loaded
configure already in sync
configure rendering
configure rendered
configure applying
configure applied
configure state saved
configure result publishing
configure result published
configure failed
```

Use fields:

```text
target
rpc_id
uuid
stage
status
error
```

Do not log:

```text
desired payload
rendered config text
secrets
full config
```

---

## Handler Changes

Update `internal/agent/handlers.go`.

Current Phase 2 configure handler publishes `not_implemented`.

Replace it with:

```go
func (r *Runtime) handleConfigure(ctx context.Context, msg agentcore.ConfigureNotification) error {
    r.logInfo("configure handler invoked", "target", msg.Target, "rpc_id", msg.RPCID, "uuid", msg.UUID)
    if r.configureService == nil {
        return fmt.Errorf("configure service is not initialized")
    }
    return r.configureService.Handle(ctx, msg)
}
```

Do not change the action handler except for small refactoring needed for compile.

Action handler remains not implemented until Phase 4.

---

## Startup Reconcile

Do **not** implement startup reconcile in this phase unless it is already explicitly required by existing docs.

Phase 3 should handle configure notifications only.

Startup reconcile can be a later sub-phase after the core configure workflow is stable.

---

## Smoke Script Update

Use the Phase 3 configure smoke script:

```text
tests/scripts/phase3-real-nats-configure-smoke.sh
```

The Phase 3 configure smoke should:

```text
1. Start real nats-server -js.
2. Start controller using nats-agent-core.
3. Start vyos-nats-agent.
4. Observe startup status.
5. SubmitConfigure through nats-agent-core.
6. Observe configure statuses.
7. Observe final configure result:
   - result = success
   - command_type = configure
   - uuid = submitted UUID
8. Verify state file exists in temporary config path.
9. Verify state file contains applied_uuid equal to submitted UUID.
10. SubmitConfigure again with same UUID.
11. Observe already_in_sync success result.
12. Stop agent with SIGINT.
13. Verify graceful shutdown.
```

Important: the smoke script must use a temp state file path, not `/var/lib/...`.

When generating temp config, replace:

```yaml
state_file: /var/lib/vyos-nats-agent/state.json
```

with:

```text
state_file: ${WORK_DIR}/state.json
```

Safe script behavior:

```text
- Do not kill existing nats-server by default.
- If port is busy, fail with message suggesting NATS_PORT=4223.
- Only kill existing NATS when explicitly requested by env var.
- Avoid raw /dev/tcp readiness checks if possible; prefer NATS log "Server is ready".
```

---

## Config Updates

No new config keys are required for Phase 3.

Keep using existing:

```yaml
agent:
  state_file: /var/lib/vyos-nats-agent/state.json
  renderer:
    mode: placeholder
  apply:
    mode: placeholder
```

Do not add renderer repo config yet.

Do not add VyOS command config yet.

---

## Docs Updates

Update `README.md` and `SPEC.md` only where necessary to reflect Phase 3 behavior.

Keep changes focused.

Document:

```text
- configure handler now loads desired config from KV
- placeholder renderer/apply flow
- state file tracks applied_uuid
- state updates only after successful apply
- real renderer/apply remains out of scope
```

Do not rewrite the full docs.

---

## Error Handling Rules

Use simple Go error wrapping:

```go
fmt.Errorf("load desired config: %w", err)
```

Handler behavior:

```text
- publish failure status/result when possible
- return wrapped error after publishing failure result
- if failure result publishing also fails, return an error that includes both failures using errors.Join
```

Example:

```go
if resultErr := s.publishFailureResult(ctx, msg, code, safeMessage); resultErr != nil {
    return errors.Join(originalErr, resultErr)
}
return originalErr
```

Do not panic.

Do not swallow errors from:

```text
LoadDesiredConfig
PublishStatus
PublishResult
Renderer.Render
ApplyEngine.Apply
StateStore.Load
StateStore.Save
```

---

## Context Rules

Follow standard Go context practices:

```text
- context.Context is the first parameter for blocking operations.
- Do not store caller contexts in structs.
- Reject nil context in service/state/renderer/apply methods.
- Return promptly if context is canceled.
- Use ctx for LoadDesiredConfig, PublishStatus, PublishResult, Render, Apply, StateStore operations.
```

For file I/O, check context before and after potentially meaningful work.

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
```

Do not claim a command passed unless actually run.

If the real-NATS smoke test cannot be run because `nats-server` is unavailable, state that clearly.

---

## Expected Files Added

Likely new files:

```text
internal/configure/service.go
internal/configure/types.go
internal/renderer/renderer.go
internal/renderer/placeholder.go
internal/apply/engine.go
internal/apply/placeholder.go
internal/state/store.go
internal/state/file_store.go
tests/scripts/phase3-real-nats-configure-smoke.sh
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

- [ ] Configure handler no longer returns `not_implemented` for valid configure commands.
- [ ] Configure handler calls `LoadDesiredConfig(ctx, target)`.
- [ ] Desired config UUID is checked against notification UUID.
- [ ] Local state file is read.
- [ ] If `applied_uuid` matches desired UUID, result is success/already applied.
- [ ] Placeholder renderer is called for new desired UUID.
- [ ] Placeholder apply engine is called after rendering.
- [ ] State file is updated only after apply succeeds.
- [ ] Configure status messages are published.
- [ ] Configure result message is published.
- [ ] Failure path publishes failure status/result when possible.
- [ ] No real VyOS rendering is implemented.
- [ ] No real VyOS apply commands are executed.
- [ ] No raw NATS code is added.
- [ ] No custom KV code is added.
- [ ] Action handler remains Phase 2 placeholder behavior.
- [ ] `go build ./...` succeeds.
- [ ] `go test ./...` succeeds.

---

## Final Response Required From Codex

After implementation, summarize:

```text
1. Files added
2. Files changed
3. How configure handler changed
4. How LoadDesiredConfig is used
5. How UUID comparison works
6. How local state is stored
7. How placeholder renderer works
8. How placeholder apply engine works
9. Status/result messages published
10. Failure handling behavior
11. What remains intentionally out of scope
12. Commands run and results
13. Whether real-NATS Phase 3 smoke was run
```

Also explicitly confirm:

```text
- no raw NATS code was added
- no custom KV code was added
- no real VyOS renderer was added
- no real VyOS apply commands were added
- action handler remains placeholder for Phase 4
- nats-agent-core remains the only bus-facing implementation
```
