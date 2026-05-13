# vyos-nats-agent

`vyos-nats-agent` is a Go daemon that runs inside or near the VyOS environment and uses `nats-agent-core` for NATS, JetStream KV, command handling, and result/status publishing.

The first milestone is intentionally minimal: prove the end-to-end configure and action flow with placeholder VyOS renderer/apply logic before integrating real VyOS rendering or command execution.

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
      publish.go

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

    actions/
      trace.go

    state/
      store.go

  tests/
    integration/
      vyos_agent_e2e_test.go
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

```bash
go test -count=1 -v -tags=integration ./tests/integration/...
```

## Design principle

`nats-agent-core` handles bus-facing behavior. `vyos-nats-agent` handles VyOS-specific orchestration.

The first implementation should keep all real VyOS behavior behind interfaces so placeholders can be replaced later without changing the NATS lifecycle.
