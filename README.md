# vyos-nats-agent

`vyos-nats-agent` is a Go daemon that runs inside or near the VyOS environment and uses `nats-agent-core` for NATS, JetStream KV, command handling, and result/status publishing.

The default mode is intentionally safe for CI and local development: configure uses placeholder VyOS renderer/apply logic unless real mode is selected explicitly.

## What this agent does

- Loads runtime settings from `config.yaml`.
- Builds `agentcore.Config` from that YAML configuration.
- Connects to NATS through `nats-agent-core`.
- Registers a configure handler for target `vyos`.
- Registers an initial action handler for `trace`.
- Receives configure notifications from `cmd.configure.vyos`.
- Loads desired configuration from JetStream KV.
- Sends the desired payload through the selected configure backend.
- Supports `placeholder` configure mode for CI/local non-VyOS runs.
- Supports `real` configure mode using `github.com/routerarchitects/olg-renderer-vyos` renderer/apply APIs.
- Stores the last successfully applied config UUID locally.
- Performs startup configuration reconciliation to sync local state with the latest KV configuration on initialization.
- Triggers automatic configuration reconciliation on reconnecting to NATS to sync offline updates.
- Supports both placeholder trace execution and real trace execution using `tcpdump` and HTTP multipart streaming upload.
- Publishes result and status messages to the bus.

## What is out of scope

- Direct raw VyOS command execution in this agent.
- Real VyOS apply in normal GitHub CI.
- Advanced rollback.
- Config diffing.
- Event publishing.
- Multiple production action handlers.
- Durable action queues.
- Cloud-side business validation.

## Configuration

The agent uses YAML as the runtime source of truth.

Default config path:

```text
/etc/vyos-nats-agent/config.yaml
```

Config path resolution order:

```text
1. --config /path/to/config.yaml
2. VYOS_NATS_AGENT_CONFIG=/path/to/config.yaml
3. /etc/vyos-nats-agent/config.yaml
```

Example config file:

```text
config.example.yaml
```

Config loading behavior:

```text
1. Start from built-in defaults.
2. Load YAML values on top of those defaults.
3. Validate final values.
```

Important rule:

```text
- Omitted fields use defaults.
- Explicit YAML values are preserved and validated (they are not overwritten by a second default pass).
```

The agent must not hardcode NATS servers, subject patterns, KV bucket names, target name, configure backend mode, apply save behavior, enabled actions, or state file paths in code. These values must come from YAML configuration.

Configure backend mode:

```yaml
agent:
  configure:
    mode: placeholder # placeholder | real
  actions:
    mode: placeholder # placeholder | real
```

`placeholder` is the default for both configure and action modes. `real` configure constructs adapters around `olg-renderer-vyos/renderer` and `olg-renderer-vyos/apply`. `real` action constructs a `VyOSTraceExecutor` which executes `/usr/bin/tcpdump` on the device and stream-uploads the captured PCAP.

`agent.configure.mode` is the single backend selector. There are no separate active renderer/apply mode fields.

Real apply save behavior is controlled separately:

```yaml
agent:
  apply:
    save_after_commit: false
```

Full payload, rendered command, and apply-plan logging is disabled by default. It is intended only for lab debugging and requires both `agent.logging.level: debug` and explicit debug flags:

```yaml
agent:
  logging:
    level: debug
  debug:
    log_payloads: false
    log_rendered: false
    log_apply_plan: false
```

Core module dependencies are pinned to released tags:

```text
github.com/routerarchitects/nats-agent-core v0.1.0
github.com/routerarchitects/olg-renderer-vyos v0.1.0
```

Validate real mode on a disposable or lab VyOS target before production rollout.

## Local state

The runtime config is YAML, but local machine-written state should be JSON.

Default state path:

```text
/tmp/vyos-nats-agent/state.json
```

State example:

```json
{
  "target": "vyos",
  "applied_uuid": "cfg-e2e-1",
  "applied_at": "2026-05-12T10:30:00Z"
}
```

The state file must only be updated after a successful apply.

### Apply success with state-save failure

The agent updates local `applied_uuid` only after apply succeeds. If apply succeeds but saving local state fails, the agent reports configure failure and does not checkpoint the UUID. A later retry may re-process the same desired config.

Real-mode lab validation should include retry behavior around post-apply state-save failures before production rollout.

## Minimal repository layout

```text
vyos-nats-agent/
  README.md
  SPEC.md
  config.example.yaml
  go.mod

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

    config/
      config.go
      defaults.go
      loader.go
      validate.go
      convert.go

    renderer/
      renderer.go
      placeholder.go

    renderervyos/
      adapter.go

    apply/
      engine.go
      placeholder.go

    applyvyos/
      adapter.go

    configure/
      service.go
      types.go

    state/
      store.go
      file_store.go
```

## Development phases

### Phase 1: Bootstrap and config loader

Create the Go module, CLI entrypoint, YAML config structs, config loader, config validation, and conversion to `agentcore.Config`.

No NATS connection is required in this phase.

### Phase 2: Agent lifecycle

Create the agent runtime, initialize `agentcore.Client`, register handlers, start/close the client, publish startup status, and handle OS signals.

### Phase 3: Configure flow with placeholders

Implement configure handling, desired config loading, placeholder renderer, placeholder apply engine, local applied UUID state, and configure result/status publishing.

### Phase 4: Action flow with one action

Implement the `trace` action handler with placeholder/real execution (`VyOSTraceExecutor`) and action result/status publishing.

### Phase 5: Integration tests

Use real `nats-server -js` to prove:

- configure end-to-end flow
- action end-to-end flow
- startup reconcile/latest desired config recovery
- reconnect-triggered reconciliation flow

### Phase 6: Real configure backend

Add `agent.configure.mode: placeholder | real`, keep placeholder as the default, and wire real mode through `olg-renderer-vyos` renderer/apply adapters.

Startup reconcile/latest desired config recovery is implemented and runs automatically on agent startup.

## Common commands

```bash
go test ./...
```

```bash
UNFORMATTED=$(gofmt -l $(find . -type f -name '*.go' -not -path './.git/*'))
test -z "$UNFORMATTED"
```

```bash
go build ./...
```

```bash
go run ./cmd/vyos-nats-agent --config ./config.example.yaml --validate-config
```

## Smoke Scripts

Action smoke:

```bash
./tests/smoke/real-nats-action-smoke.sh
```

Expected success marker:

```text
[PASS] Real-NATS action smoke test passed
```

Configure smoke:

```bash
./tests/smoke/real-nats-configure-smoke.sh
```

Expected success marker:

```text
[PASS] Real-NATS configure smoke test passed
```

Optional debug output for action smoke:

```bash
PRINT_LOGS_ON_PASS=true KEEP_SMOKE_ARTIFACTS=true NATS_PORT=4223 ./tests/smoke/real-nats-action-smoke.sh
```

`PRINT_LOGS_ON_PASS=true` prints NATS/agent/controller logs on success.  
`KEEP_SMOKE_ARTIFACTS=true` keeps temporary files and prints the artifact directory path.
`NATS_PORT=4223` is optional and helps avoid conflicts when `4222` is already in use.

Config validation script:

```bash
./tests/smoke/validate-config.sh
```

Manual real-VyOS lab smoke lives under `tests/lab` and is not part of normal
PR CI. See `tests/lab/README.md` before running it against a lab VM/device.

## CI coverage

`.github/workflows/ci.yml` currently validates:

- `gofmt` formatting check
- `go test ./...`
- `go build ./...`
- `./tests/smoke/validate-config.sh`
- `./tests/smoke/real-nats-configure-smoke.sh`
- `./tests/smoke/real-nats-action-smoke.sh`

## Binary usage

The current binary supports:
- validation-only mode with safe effective-config printing
- long-running runtime mode using `nats-agent-core` (`Start`, handler registration, status publish, graceful `Close`)
- configure workflow using `LoadDesiredConfig(ctx, target)`, selected configure backend mode, and local applied UUID state updates after successful apply
- startup configuration reconciliation on initialization to align local state with KV
- automatic reconnection reconciliation triggered by NATS connection recovery to fetch offline changes
- action workflow for `trace` using placeholder or real trace execution with parameters bounds, strict regex interface checks, secure randomized temp file PCAP storage, and zero-copy streaming multipart upload, with status/result publishing

```bash
go run ./cmd/vyos-nats-agent --config ./config.example.yaml --validate-config
```

### Options

| Option | Description |
|---|---|
| `--config <path>` | Path to the YAML configuration file. If omitted, the agent checks `VYOS_NATS_AGENT_CONFIG`, then falls back to `/etc/vyos-nats-agent/config.yaml`. |
| `--validate-config` | Loads the YAML config, applies defaults, validates values, converts to `agentcore.Config`, prints `configuration valid`, and exits. |
| `--print-effective-config` | Prints the sanitized effective YAML config after defaults and YAML overlay. Sensitive values are shown as `********`. |
| `--help` | Shows command-line help. |

### Config path resolution

The config file path is resolved in this order:

```text
1. --config /path/to/config.yaml
2. VYOS_NATS_AGENT_CONFIG=/path/to/config.yaml
3. /etc/vyos-nats-agent/config.yaml
```

### Current behavior

Running without `--validate-config` loads config, converts to `agentcore.Config`, creates the runtime, registers configure/action handlers, starts `agentcore`, publishes startup status, runs startup reconciliation, then waits for `SIGINT`/`SIGTERM` and shuts down gracefully.

Configure handling (both during normal configure notifications and reconciliation passes) loads desired config through `LoadDesiredConfig(ctx, target)`, verifies target/UUID, checks local `state_file`, and:
- publishes `already_in_sync` success when desired UUID already matches local `applied_uuid`
- otherwise runs the selected configure backend, updates local state after render/apply succeeds, and publishes configure success
- publishes configure failure status/result with stable error codes on workflow failures

During NATS reconnection, the registered reconnect handler automatically triggers a background configuration reconciliation pass to sync any updates stored in NATS KV while the agent was offline.

Normal CI and smoke tests should continue to use `agent.configure.mode: placeholder`. Real mode is intended for lab validation first and should not be added to normal CI without explicit VyOS lab opt-in.

Action handling in this phase supports `trace` only:
- validates action is enabled and supported
- runs placeholder trace execution or real trace execution (`VyOSTraceExecutor`) using `/usr/bin/tcpdump` and zero-copy HTTP streaming multipart upload to the controller
- enforces parameter limits (duration <= 300s, packets <= 10000) and strict interface validation rules (sys net class check or regex fallback)
- publishes action statuses (`received`, `executing`, `completed` or `failed`)
- publishes final action result with trace execution metadata payload

`--print-effective-config` prints the effective config as YAML after defaults and YAML overlay. Sensitive values are redacted as `********`, and the converted `agentcore.Config` is not printed.

Logging is configured under `agent.logging`:
- `enabled`: `true|false`
- `level`: `debug|info|warn|error`
- `format`: `text|json`

```bash
go run ./cmd/vyos-nats-agent \
  --config ./config.example.yaml \
  --print-effective-config \
  --validate-config
```

`--validate-config` still exits without creating or starting the `agentcore` client.

## Design principle

`nats-agent-core` handles bus-facing behavior. `vyos-nats-agent` handles VyOS-specific orchestration.

Real VyOS behavior stays behind interfaces and adapters so placeholder mode remains available without changing the NATS lifecycle.
