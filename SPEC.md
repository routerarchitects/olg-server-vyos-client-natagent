# VyOS NATS Agent Specification

## 1. Project name

```text
vyos-nats-agent
```

## 2. Goal

Build a minimal VyOS-focused daemon that uses `nats-agent-core` for all NATS, JetStream KV, subject, handler, result, and status communication.

The first milestone must prove an end-to-end configure/action lifecycle using:

- YAML runtime configuration
- placeholder VyOS renderer
- placeholder VyOS apply engine
- placeholder `trace` action executor
- real NATS integration tests

## 3. Core architecture

`vyos-nats-agent` is a daemon.

`nats-agent-core` is a library embedded inside the daemon.

The agent owns:

- process lifecycle
- YAML configuration loading
- conversion to `agentcore.Config`
- handler registration
- placeholder rendering
- placeholder apply logic
- placeholder action execution
- local applied UUID state
- result/status publication decisions

The library owns:

- NATS connection
- JetStream access
- desired-config KV
- subject patterns
- configure/action/result/status envelopes
- handler registration mechanics
- publish/subscribe mechanics
- reconnect behavior

## 4. Configuration source of truth

The agent must be configured from YAML.

The implementation must not hardcode:

- NATS server URLs
- NATS client name
- NATS authentication paths or values
- JetStream settings
- KV bucket name
- KV key pattern
- subject patterns
- target name
- local state path
- renderer mode
- apply mode
- enabled actions
- timeout values
- retry values

## 5. Config path resolution

The config path must be resolved in this order:

```text
1. --config /path/to/config.yaml
2. VYOS_NATS_AGENT_CONFIG=/path/to/config.yaml
3. /etc/vyos-nats-agent/config.yaml
```

## 6. Config file format

The primary config format is YAML.

The repository must include:

```text
config.example.yaml
```

Do not support JSON or TOML in milestone 1.

The config loader must be isolated so additional formats can be added later without changing the rest of the agent.

## 7. Config file structure

The YAML config must have two top-level sections:

```yaml
agent: {}
agentcore: {}
```

## 8. Agent config section

The `agent` section contains VyOS-agent-specific settings.

Required fields:

```yaml
agent:
  name: vyos-nats-agent
  version: 0.1.0
  target: vyos
  state_file: /var/lib/vyos-nats-agent/state.json
```

Optional sections:

```yaml
agent:
  logging:
    enabled: true
    level: info
    format: text

  renderer:
    mode: placeholder

  apply:
    mode: placeholder
    save_after_commit: true

  actions:
    enabled:
      - trace
```

## 9. Agentcore config section

The `agentcore` section contains settings used to construct `agentcore.Config`.

Required subsections:

```yaml
agentcore:
  nats: {}
  subjects: {}
  kv: {}
```

Recommended full shape:

```yaml
agentcore:
  nats:
    servers:
      - nats://127.0.0.1:4222
    client_name: vyos-nats-agent
    credentials_file: ""
    nkey_seed_file: ""
    user_jwt_file: ""
    username: ""
    password: ""
    token: ""
    connect_timeout: 5s
    retry_on_failed_connect: false
    max_reconnects: -1
    reconnect_wait: 2s
    reconnect_buf_size: 0
    tls:
      enabled: false
      insecure_skip_verify: false
      ca_file: ""
      cert_file: ""
      key_file: ""
      server_name: ""

  jetstream:
    domain: ""
    api_prefix: ""
    default_timeout: 5s

  subjects:
    configure_pattern: cmd.configure.%s
    action_pattern: cmd.action.%s.%s
    result_pattern: result.%s
    status_pattern: status.%s
    health_pattern: health.%s

  kv:
    bucket: cfg_desired
    key_pattern: desired.%s
    auto_create_bucket: true
    history: 1
    ttl: 0s
    max_value_size: 0
    storage: file
    replicas: 1

  timeouts:
    publish_timeout: 5s
    subscribe_timeout: 5s
    kv_timeout: 5s
    shutdown_timeout: 10s
    handler_warn_after: 2s

  retry:
    publish_attempts: 1
    publish_backoff: 250ms

  execution:
    handler_mode: sync
```

For milestone 1, KV history is intentionally `1` because the agent converges from
the latest desired config UUID and local applied UUID comparison, not historical
KV revisions.

### KV bucket ownership

The VyOS agent needs KV settings so it can load the latest desired config, but production bucket creation should be owned by the controller/provisioning side.

`agentcore.kv.auto_create_bucket: true` is intended for local development and integration tests.

Production VyOS agents should normally use:

```yaml
agentcore:
  kv:
    auto_create_bucket: false
```

and bind to an existing JetStream KV bucket created by the controller/provisioning workflow.

## 10. Duration handling

The YAML config must use human-readable duration strings.

Examples:

```yaml
connect_timeout: 5s
publish_backoff: 250ms
ttl: 0s
```

Implementation rule:

```text
config.yaml
  -> internal/config.AppConfig with string duration fields
  -> Validate()
  -> time.ParseDuration(...)
  -> agentcore.Config
```

Do not unmarshal YAML directly into `agentcore.Config`.

## 11. Recommended YAML package

Use:

```bash
go get gopkg.in/yaml.v3
```

## 12. Default values

Defaults may be applied only when fields are omitted.

Default values:

```text
agent.name = vyos-nats-agent
agent.version = 0.1.0
agent.target = vyos
agent.state_file = /var/lib/vyos-nats-agent/state.json
agent.logging.enabled = true
agent.logging.level = info
agent.logging.format = text
agent.renderer.mode = placeholder
agent.apply.mode = placeholder
agent.apply.save_after_commit = true
agent.actions.enabled = [trace]

agentcore.nats.servers = [nats://127.0.0.1:4222]
agentcore.nats.client_name = vyos-nats-agent
agentcore.nats.connect_timeout = 5s
agentcore.nats.retry_on_failed_connect = false
agentcore.nats.max_reconnects = -1
agentcore.nats.reconnect_wait = 2s

agentcore.jetstream.default_timeout = 5s

agentcore.subjects.configure_pattern = cmd.configure.%s
agentcore.subjects.action_pattern = cmd.action.%s.%s
agentcore.subjects.result_pattern = result.%s
agentcore.subjects.status_pattern = status.%s
agentcore.subjects.health_pattern = health.%s

agentcore.kv.bucket = cfg_desired
agentcore.kv.key_pattern = desired.%s
agentcore.kv.auto_create_bucket = true
agentcore.kv.history = 1
agentcore.kv.ttl = 0s
agentcore.kv.storage = file
agentcore.kv.replicas = 1

agentcore.timeouts.publish_timeout = 5s
agentcore.timeouts.subscribe_timeout = 5s
agentcore.timeouts.kv_timeout = 5s
agentcore.timeouts.shutdown_timeout = 10s
agentcore.timeouts.handler_warn_after = 2s

agentcore.retry.publish_attempts = 1
agentcore.retry.publish_backoff = 250ms
agentcore.execution.handler_mode = sync
```

## 13. Validation rules

The config loader must reject:

- empty agent target
- empty state file path
- empty NATS server list
- empty KV bucket
- empty KV key pattern
- empty configure subject pattern
- empty action subject pattern
- empty result subject pattern
- empty status subject pattern
- invalid duration strings
- unsupported renderer mode
- unsupported apply mode
- unsupported action names
- unsupported logging level
- unsupported logging format
- negative timeout values where unsupported
- invalid KV history values
- invalid KV storage values

For milestone 1, supported values are:

```text
renderer.mode = placeholder
apply.mode = placeholder
actions.enabled = trace
```

## 14. Runtime config package

Use this package structure:

```text
internal/config/
  config.go      // raw config structs
  defaults.go    // default values
  loader.go      // path resolution and YAML loading
  validate.go    // validation rules
  convert.go     // conversion to agentcore.Config
```

Expected usage:

```go
cfg, err := config.Load(path)
if err != nil {
    return err
}

if err := cfg.Validate(); err != nil {
    return err
}

agentCoreCfg, err := cfg.ToAgentCoreConfig()
if err != nil {
    return err
}
```

## 15. Public daemon lifecycle

Startup sequence:

```text
1. Resolve config path.
2. Load YAML config.
3. Apply defaults.
4. Validate config.
5. Convert config to agentcore.Config.
6. Create agentcore.Client.
7. Create renderer implementation.
8. Create apply engine implementation.
9. Create action implementations.
10. Create local state store.
11. Register configure handler.
12. Register action handlers.
13. Start agentcore.Client.
14. Publish startup status.
15. Run startup reconcile.
16. Wait for SIGINT/SIGTERM.
17. Close agentcore.Client gracefully.
```

## 16. Configure lifecycle

When the agent receives a configure notification:

```text
1. Receive ConfigureNotification.
2. Publish running status.
3. Load desired config with LoadDesiredConfig(ctx, target).
4. Compare desired UUID with local applied UUID.
5. If already applied:
   - publish success result
   - do not render
   - do not apply
6. If not applied:
   - pass desired payload to Renderer.Render(...)
   - pass rendered config to ApplyEngine.Apply(...)
   - update local applied UUID after success
   - publish success result
7. If any step fails:
   - do not update local applied UUID
   - publish failure result
```

### Configure recovery model

Configure notification is only a fast-path trigger.

The durable source of truth is the latest desired config stored in JetStream KV at `desired.<target>`.

The agent must not rely only on receiving `cmd.configure.<target>` notifications for correctness.

If a configure notification is missed, or if `SubmitConfigure` writes KV successfully but fails before publishing the notification, the agent must still converge by:

1. loading the latest desired config from KV during startup reconcile or explicit recover,
2. comparing desired UUID with the locally applied UUID,
3. rendering/applying when the desired UUID differs,
4. updating local applied UUID only after successful apply,
5. publishing result/status using the desired config metadata where available.

## 17. Action lifecycle

For milestone 1, only the `trace` action is required.

When the agent receives an action command:

```text
1. Receive ActionCommand.
2. Check action is enabled in config.
3. Publish running status if useful.
4. Execute placeholder trace action.
5. Publish success or failure result.
```

## 18. Renderer interface

The renderer must be behind an interface.

```go
type Renderer interface {
    Render(ctx context.Context, payload []byte) ([]byte, error)
}
```

Milestone 1 implementation:

```text
placeholder renderer
```

The placeholder renderer must not call real VyOS code.

It may return a deterministic text representation of the input payload.

## 19. Apply engine interface

The apply engine must be behind an interface.

```go
type Engine interface {
    Apply(ctx context.Context, rendered []byte) error
}
```

Milestone 1 implementation:

```text
placeholder apply engine
```

The placeholder apply engine must not call real VyOS commands.

It may log or store the rendered config for test verification.

## 20. Action executor interface

Action execution must be behind an interface.

```go
type TraceExecutor interface {
    Trace(ctx context.Context, payload []byte) ([]byte, error)
}
```

Milestone 1 implementation:

```text
placeholder trace executor
```

The placeholder trace executor must return deterministic JSON output.

## 21. Local state

The runtime config is YAML.

The local state file is JSON.

Default path:

```text
/var/lib/vyos-nats-agent/state.json
```

State shape:

```json
{
  "target": "vyos",
  "applied_uuid": "cfg-e2e-1",
  "applied_at": "2026-05-12T10:30:00Z"
}
```

Rules:

- create the parent directory if needed
- tolerate missing file as empty state
- write updates atomically
- update `applied_uuid` only after successful apply
- never update applied UUID after render/apply failure
- milestone 1 converges from only the latest desired config (`history = 1`) and
  does not depend on historical KV revisions

### Apply success with state-save failure

The agent updates local `applied_uuid` only after apply succeeds. If apply
succeeds but saving local state fails, the agent reports configure failure and
does not checkpoint the UUID. A later retry may re-process the same desired
config.

This is acceptable for the Phase 3 placeholder apply path. Before real VyOS
apply is introduced, apply behavior should be idempotent and/or include a
verification step so retries after state-save failure do not produce unsafe
duplicate effects.

## 22. Result publishing

Every accepted configure/action request must publish a final result.

Configure success:

```json
{
  "version": "1.0",
  "rpc_id": "rpc-123",
  "target": "vyos",
  "command_type": "configure",
  "uuid": "cfg-123",
  "result": "success",
  "message": "VyOS configuration applied",
  "timestamp": "2026-05-12T10:30:00Z"
}
```

Configure failure:

```json
{
  "version": "1.0",
  "rpc_id": "rpc-123",
  "target": "vyos",
  "command_type": "configure",
  "uuid": "cfg-123",
  "result": "failure",
  "error_code": "apply_failed",
  "message": "VyOS commit failed",
  "timestamp": "2026-05-12T10:30:00Z"
}
```

Action success:

```json
{
  "version": "1.0",
  "rpc_id": "rpc-456",
  "target": "vyos",
  "command_type": "action",
  "action": "trace",
  "result": "success",
  "message": "Trace completed",
  "payload": {},
  "timestamp": "2026-05-12T10:30:00Z"
}
```

## 23. Status publishing

Status publishing is allowed for startup/running/progress messages.

Minimum startup status:

```json
{
  "version": "1.0",
  "target": "vyos",
  "status": "running",
  "stage": "startup",
  "message": "vyos-nats-agent started",
  "timestamp": "2026-05-12T10:30:00Z"
}
```

## 24. Startup reconcile

After `agentcore.Client.Start(ctx)`, the agent must run startup reconcile.

Minimal behavior:

```text
1. Call StartupReconcile(ctx, target) or LoadDesiredConfig(ctx, target).
2. If no desired config exists, continue normally.
3. If desired UUID equals local applied UUID, continue normally.
4. If desired UUID differs, run the same render/apply/result lifecycle.
```

### Startup reconcile failure policy

Startup reconcile runs after `agentcore.Client.Start(ctx)` succeeds.

Fatal startup errors:
- config path cannot be resolved
- YAML config cannot be loaded or validated
- config cannot be converted to `agentcore.Config`
- `agentcore.Client` cannot be created
- `agentcore.Client.Start(ctx)` fails
- local state store cannot be initialized safely

Non-fatal reconcile errors:
- no desired config exists in KV
- desired config already matches local applied UUID
- desired config load times out after startup
- renderer fails during reconcile
- apply engine fails during reconcile
- result/status publish fails after a reconcile attempt

For non-fatal reconcile errors, the agent must:
1. publish degraded/failure status when possible,
2. not update local applied UUID unless apply succeeds,
3. continue running and keep handling future configure notifications/actions,
4. allow a later startup reconcile or explicit recover path to converge from KV.

## 25. Handler concurrency

Configure apply must be serialized.

For milestone 1, direct synchronous handler execution is acceptable because placeholder work is fast.

The code must still be structured so a future phase can move configure processing to a single worker queue.

## 26. Integration tests

Integration tests must use real `nats-server -js`.

They must not use raw NATS publish for the primary test flow.

Use public `agentcore` APIs for controller-side behavior.

### 26.1 Configure end-to-end

```text
1. Start real nats-server -js.
2. Start vyos-nats-agent with test config.
3. Create controller agentcore.Client with same NATS server and KV bucket.
4. Register controller result handler.
5. Controller calls SubmitConfigure.
6. VyOS agent receives configure notification.
7. VyOS agent loads desired config from KV.
8. Placeholder renderer runs.
9. Placeholder apply engine runs.
10. Local applied UUID is updated.
11. VyOS agent publishes result.
12. Controller receives result.
13. Assert rpc_id, target, uuid, result.
```

### 26.2 Action end-to-end

```text
1. Start real nats-server -js.
2. Start vyos-nats-agent with test config.
3. Create controller agentcore.Client with same NATS server and KV bucket.
4. Register controller result handler.
5. Controller calls SubmitAction for trace.
6. VyOS agent receives action.
7. Placeholder trace executor runs.
8. VyOS agent publishes result.
9. Controller receives result.
10. Assert rpc_id, target, action, result.
```

### 26.3 Startup reconcile

```text
1. Start real nats-server -js.
2. Controller stores desired config through SubmitConfigure or StoreDesiredConfig.
3. Start vyos-nats-agent.
4. Agent runs startup reconcile.
5. Agent applies latest desired config.
6. Agent updates local applied UUID.
```

Current repository smoke scripts:

- `tests/scripts/phase3-real-nats-configure-smoke.sh`
- `tests/scripts/phase4-real-nats-action-smoke.sh`

## 27. Development phases

### Phase 1: Bootstrap and config loader

Implement:

- Go module
- CLI entrypoint
- YAML config loader
- config defaults
- config validation
- conversion to `agentcore.Config`
- `--validate-config` CLI mode

Do not connect to NATS in this phase.

### Phase 2: Agent lifecycle

Implement:

- `agentcore.New(...)`
- configure/action handler registration
- `Start(ctx)`
- startup status publication
- signal handling
- graceful shutdown

Do not implement configure apply yet.

### Phase 3: Configure flow with placeholders

Implement:

- `RegisterConfigureHandler(target, handler)`
- `LoadDesiredConfig(ctx, target)`
- placeholder renderer
- placeholder apply engine
- local applied UUID state
- configure result/status publishing

### Phase 4: Action flow with one action

Implement:

- `RegisterActionHandler(target, "trace", handler)`
- placeholder trace executor
- action result/status publishing

### Phase 5: Integration tests

Implement integration tests for:

- configure flow
- action flow
- startup reconcile/latest desired config recovery

## 28. Codex implementation rules

Codex must follow these rules:

- Work one phase at a time.
- Do not generate future phase code early.
- Do not hardcode config values.
- Do not bypass `nats-agent-core`.
- Do not use raw NATS APIs in production code.
- Keep public APIs small.
- Keep handlers thin.
- Keep renderer/apply/action logic behind interfaces.
- Keep config loader deterministic and well-tested.
- Use placeholder implementations until explicitly asked to integrate real VyOS logic.
- Use structured errors with context.
- Run `gofmt` on changed Go files.
- Run `go test ./...` after each phase.
- Run integration tests only when NATS server is available.
- Do not claim tests passed unless they were actually run.

## 29. Acceptance criteria for milestone 1

Milestone 1 is complete when:

```text
[ ] Agent starts from YAML config.
[ ] Agent constructs agentcore.Config from YAML config.
[ ] Agent connects to NATS.
[ ] Agent registers configure handler for target "vyos".
[ ] Agent registers action handler for "trace".
[ ] Agent publishes startup status.
[ ] Agent handles configure notification.
[ ] Agent loads desired config from KV.
[ ] Placeholder renderer runs.
[ ] Placeholder apply engine runs.
[ ] Applied UUID is saved to local state.
[ ] Agent publishes configure result.
[ ] Agent handles trace action.
[ ] Agent publishes action result.
[ ] Integration test proves configure end-to-end.
[ ] Integration test proves action end-to-end.
[ ] Integration test proves startup reconcile or latest desired config recovery.
```
