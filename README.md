# vyos-nats-agent

`vyos-nats-agent` is a Go daemon that runs inside or near the VyOS environment and uses `nats-agent-core` for NATS, JetStream KV, command handling, and result/status publishing.

The first milestone is intentionally minimal: prove configure lifecycle behavior end to end with placeholder VyOS renderer/apply logic, while keeping action behavior in placeholder mode until a later phase.

## What this agent does

- Loads runtime settings from `config.yaml`.
- Builds `agentcore.Config` from that YAML configuration.
- Connects to NATS through `nats-agent-core`.
- Registers a configure handler for target `vyos`.
- Registers an initial action handler for `trace`.
- Receives configure notifications from `cmd.configure.vyos`.
- Loads desired configuration from JetStream KV.
- Sends the desired payload through a placeholder renderer.
- Sends rendered config through a placeholder apply engine.
- Stores the last successfully applied config UUID locally.
- Publishes placeholder action failure (`not_implemented`) for `trace` in current phase.
- Publishes result and status messages to the bus.

## What is out of scope for the first milestone

- Real VyOS renderer integration.
- Real VyOS command execution.
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

The agent must not hardcode NATS servers, subject patterns, KV bucket names, target name, renderer mode, apply mode, enabled actions, or state file paths in code. These values must come from YAML configuration.

## Local state

The runtime config is YAML, but local machine-written state should be JSON.

Default state path:

```text
/var/lib/vyos-nats-agent/state.json
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

This is acceptable for the Phase 3 placeholder apply path. Before real VyOS apply is introduced, apply behavior should be idempotent and/or include a verification step so retries after state-save failure do not produce unsafe duplicate effects.

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

    apply/
      engine.go
      placeholder.go

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

Implement the `trace` action handler with placeholder execution and action result/status publishing.

### Phase 5: Integration tests

Use real `nats-server -js` to prove:

- configure end-to-end flow
- action end-to-end flow
- startup reconcile/latest desired config recovery

## Common commands

```bash
go test ./...
```

```bash
go run ./cmd/vyos-nats-agent --config ./config.example.yaml --validate-config
```

## Phase smoke scripts

Phase 2 action smoke (current action placeholder behavior):

```bash
./tests/scripts/phase2-real-nats-action-smoke.sh
```

Expected success marker:

```text
[PASS] Phase 2 real-NATS action smoke test passed
```

Configure smoke:

```bash
./tests/scripts/phase3-real-nats-configure-smoke.sh
```

Expected success marker:

```text
[PASS] Phase 3 real-NATS configure smoke test passed
```

Optional debug output for Phase 3 configure smoke:

```bash
PRINT_LOGS_ON_PASS=true KEEP_SMOKE_ARTIFACTS=true NATS_PORT=4223 ./tests/scripts/phase3-real-nats-configure-smoke.sh
```

`PRINT_LOGS_ON_PASS=true` prints NATS/agent/controller logs on success.  
`KEEP_SMOKE_ARTIFACTS=true` keeps temporary files and prints the artifact directory path.
`NATS_PORT=4223` is optional and helps avoid conflicts when `4222` is already in use.

Config validation script:

```bash
./tests/scripts/validate-config.sh
```

## Binary usage

The current binary supports Phase 3 behavior:
- validation-only mode with safe effective-config printing
- long-running runtime mode using `nats-agent-core` (`Start`, handler registration, status publish, graceful `Close`)
- configure workflow using `LoadDesiredConfig(ctx, target)`, placeholder render/apply, and local applied UUID state updates after successful apply

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

### Current Phase 3 behavior

Running without `--validate-config` loads config, converts to `agentcore.Config`, creates the runtime, registers configure/action handlers, starts `agentcore`, publishes startup status, then waits for `SIGINT`/`SIGTERM` and shuts down gracefully.

Configure handling in this phase loads desired config through `LoadDesiredConfig(ctx, target)`, verifies target/UUID, checks local `state_file`, and:
- publishes `already_in_sync` success when desired UUID already matches local `applied_uuid`
- otherwise runs placeholder render/apply, updates local state after apply succeeds, and publishes configure success
- publishes configure failure status/result with stable error codes on workflow failures

Action handling in the current phase remains placeholder behavior:
- `trace` action returns failure with `error_code=not_implemented`
- no real trace command execution, shell execution, or network probing is performed

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

The first implementation should keep all real VyOS behavior behind interfaces so placeholders can be replaced later without changing the NATS lifecycle.
