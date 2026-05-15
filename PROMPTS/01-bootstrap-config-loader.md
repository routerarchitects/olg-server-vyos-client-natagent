# PROMPTS/01-bootstrap-config-loader.md

# Phase 1: Bootstrap Go Module and YAML Config Loader

## Objective

Read `README.md`, `SPEC.md`, and `config.example.yaml`.

Implement only the initial software-development phase for `vyos-nats-agent`.

This phase is only about:

```text
config.yaml
  -> load into the agent's own raw config model
  -> apply defaults
  -> validate required values
  -> parse duration strings
  -> build agentcore.Config from github.com/routerarchitects/nats-agent-core/agentcore
```

Do not implement daemon runtime, NATS connection startup, configure handling, action handling, renderer logic, apply logic, state store, or any message-processing lifecycle in this phase.

The output of this phase should be a clean Go project that can load and validate YAML configuration and prepare the `agentcore.Config` that will be used in the next phase.

## Hard Rules

1. Use `github.com/routerarchitects/nats-agent-core/agentcore` as the only source of truth for NATS agent-core configuration types.
2. Do not duplicate `nats-agent-core` behavior.
3. Do not implement custom NATS connection code.
4. Do not implement custom subject-builder logic.
5. Do not implement custom KV access logic.
6. Do not implement configure/action/result/status transport behavior.
7. Do not create any internal package that tries to replace or shadow `agentcore`.
8. Do not hardcode runtime values in application logic.
9. Runtime values must come from YAML config plus clearly-defined defaults.
10. Keep the project minimal, clean, and phase-specific.

## Required Dependencies

Use:

```go
github.com/routerarchitects/nats-agent-core
gopkg.in/yaml.v3
```

If `go.mod` does not exist, create it.

Use the module path inferred from the repository remote or current repository name. Do not invent unrelated module paths.

For local development, if the `nats-agent-core` repository exists as a sibling directory, it is acceptable to add a temporary local `replace` directive, for example:

```go
replace github.com/routerarchitects/nats-agent-core => ../nats-agent-core
```

Do not vendor the library.

## Required Directory Structure

Create only this minimal structure:

```text
.
├── go.mod
├── cmd/
│   └── vyos-nats-agent/
│       └── main.go
└── internal/
    └── config/
        ├── config.go
        ├── defaults.go
        ├── loader.go
        ├── validate.go
        └── convert.go
```

Do not create other directories in this phase.

Do not create renderer, apply, actions, agent, state, transport, or integration-related packages in this phase.

## Required CLI Behavior

Implement a minimal CLI entrypoint in:

```text
cmd/vyos-nats-agent/main.go
```

Supported flags:

```text
--config <path>
--validate-config
```

Behavior:

### `--config`

Specifies the YAML config file path.

### `--validate-config`

Loads config, applies defaults, validates it, converts it to `agentcore.Config`, prints a short success message, and exits.

Example success output:

```text
configuration valid
```

This mode must not connect to NATS.

This mode must not call `agentcore.New`.

This mode must not start any daemon lifecycle.

If `--validate-config` is not provided, the command may print a short message saying that only config validation is implemented in this phase.

Example:

```text
phase 1 complete: config loader available; agent runtime not implemented yet
```

## Config Path Resolution

Implement config path resolution in this order:

```text
1. --config flag
2. VYOS_NATS_AGENT_CONFIG environment variable
3. /etc/vyos-nats-agent/config.yaml
```

Keep this logic small and obvious.

## YAML Config Model

Create an agent-owned raw config model under `internal/config`.

Use YAML tags.

The raw config model should contain string duration fields so duration parsing is explicit and controlled.

Top-level shape:

```go
type AppConfig struct {
    Agent     AgentConfig     `yaml:"agent"`
    AgentCore AgentCoreConfig `yaml:"agentcore"`
}
```

Agent-specific config:

```go
type AgentConfig struct {
    Name      string         `yaml:"name"`
    Version   string         `yaml:"version"`
    Target    string         `yaml:"target"`
    StateFile string         `yaml:"state_file"`
    Renderer  RendererConfig `yaml:"renderer"`
    Apply     ApplyConfig    `yaml:"apply"`
    Actions   ActionsConfig  `yaml:"actions"`
}

type RendererConfig struct {
    Mode string `yaml:"mode"`
}

type ApplyConfig struct {
    Mode            string `yaml:"mode"`
    SaveAfterCommit bool   `yaml:"save_after_commit"`
}

type ActionsConfig struct {
    Enabled []string `yaml:"enabled"`
}
```

Agent-core config sections:

```go
type AgentCoreConfig struct {
    NATS      NATSConfig      `yaml:"nats"`
    JetStream JetStreamConfig `yaml:"jetstream"`
    Subjects  SubjectConfig   `yaml:"subjects"`
    KV        KVConfig        `yaml:"kv"`
    Timeouts  TimeoutConfig   `yaml:"timeouts"`
    Retry     RetryConfig     `yaml:"retry"`
    Execution ExecutionConfig `yaml:"execution"`
}
```

NATS config:

```go
type NATSConfig struct {
    Servers              []string  `yaml:"servers"`
    ClientName           string    `yaml:"client_name"`
    CredentialsFile      string    `yaml:"credentials_file"`
    NKeySeedFile         string    `yaml:"nkey_seed_file"`
    UserJWTFile          string    `yaml:"user_jwt_file"`
    Username             string    `yaml:"username"`
    Password             string    `yaml:"password"`
    Token                string    `yaml:"token"`
    ConnectTimeout       string    `yaml:"connect_timeout"`
    RetryOnFailedConnect bool      `yaml:"retry_on_failed_connect"`
    MaxReconnects        int       `yaml:"max_reconnects"`
    ReconnectWait        string    `yaml:"reconnect_wait"`
    ReconnectBufSize     int       `yaml:"reconnect_buf_size"`
    TLS                  TLSConfig `yaml:"tls"`
}

type TLSConfig struct {
    Enabled            bool   `yaml:"enabled"`
    InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
    CAFile             string `yaml:"ca_file"`
    CertFile           string `yaml:"cert_file"`
    KeyFile            string `yaml:"key_file"`
    ServerName         string `yaml:"server_name"`
}
```

JetStream config:

```go
type JetStreamConfig struct {
    Domain         string `yaml:"domain"`
    APIPrefix      string `yaml:"api_prefix"`
    DefaultTimeout string `yaml:"default_timeout"`
}
```

Subject config:

```go
type SubjectConfig struct {
    ConfigurePattern string `yaml:"configure_pattern"`
    ActionPattern    string `yaml:"action_pattern"`
    ResultPattern    string `yaml:"result_pattern"`
    StatusPattern    string `yaml:"status_pattern"`
    HealthPattern    string `yaml:"health_pattern"`
}
```

KV config:

```go
type KVConfig struct {
    Bucket           string `yaml:"bucket"`
    KeyPattern       string `yaml:"key_pattern"`
    AutoCreateBucket bool   `yaml:"auto_create_bucket"`
    History          uint8  `yaml:"history"`
    TTL              string `yaml:"ttl"`
    MaxValueSize     int32  `yaml:"max_value_size"`
    Storage          string `yaml:"storage"`
    Replicas         int    `yaml:"replicas"`
}
```

Timeout config:

```go
type TimeoutConfig struct {
    PublishTimeout   string `yaml:"publish_timeout"`
    SubscribeTimeout string `yaml:"subscribe_timeout"`
    KVTimeout        string `yaml:"kv_timeout"`
    ShutdownTimeout  string `yaml:"shutdown_timeout"`
    HandlerWarnAfter string `yaml:"handler_warn_after"`
}
```

Retry and execution config:

```go
type RetryConfig struct {
    PublishAttempts int    `yaml:"publish_attempts"`
    PublishBackoff  string `yaml:"publish_backoff"`
}

type ExecutionConfig struct {
    HandlerMode string `yaml:"handler_mode"`
}
```

## Defaults

Implement defaults in `internal/config/defaults.go`.

Defaults must be applied after YAML load and before validation.

Default values:

```text
agent.name = vyos-nats-agent
agent.version = 0.1.0
agent.target = vyos
agent.state_file = /var/lib/vyos-nats-agent/state.json
agent.renderer.mode = placeholder
agent.apply.mode = placeholder
agent.apply.save_after_commit = true
agent.actions.enabled = ["trace"]

agentcore.nats.servers = ["nats://127.0.0.1:4222"]
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

Do not hide defaults inside unrelated logic.

## Validation

Implement validation in `internal/config/validate.go`.

Validation must reject:

```text
- empty agent.target
- empty agent.state_file
- unsupported agent.renderer.mode
- unsupported agent.apply.mode
- unsupported action names
- empty agentcore.nats.servers
- empty agentcore.subjects.configure_pattern
- empty agentcore.subjects.action_pattern
- empty agentcore.subjects.result_pattern
- empty agentcore.subjects.status_pattern
- empty agentcore.subjects.health_pattern
- empty agentcore.kv.bucket
- empty agentcore.kv.key_pattern
- invalid duration strings
- invalid KV history when zero after defaults
- invalid KV replicas when less than one after defaults
- invalid retry publish_attempts when less than one after defaults
```

Supported values for this phase:

```text
agent.renderer.mode = placeholder
agent.apply.mode = placeholder
agent.actions.enabled may contain only trace
agentcore.execution.handler_mode = sync
```

Validation errors should be clear enough to identify the failing field.

## Duration Parsing

Create a small helper for parsing durations.

Rules:

```text
- use time.ParseDuration
- empty duration strings should be allowed only where the matching field is optional
- required duration strings should be defaulted before validation
- return errors that include the config field name
```

Do not rely on YAML unmarshalling directly into `time.Duration`.

## Conversion to agentcore.Config

Implement conversion in:

```text
internal/config/convert.go
```

Add a method or function:

```go
func (c AppConfig) ToAgentCoreConfig() (agentcore.Config, error)
```

This function must:

```text
- parse all duration strings
- populate agentcore.Config
- use agentcore.NATSConfig
- use agentcore.JetStreamConfig
- use agentcore.SubjectConfig
- use agentcore.KVConfig
- use agentcore.TimeoutConfig
- use agentcore.RetryConfig
- use agentcore.ExecutionConfig
```

It must not construct custom replacements for these types.

It must not call `agentcore.New`.

It must not connect to NATS.

It must only prepare and return the config object.

The `agentcore.Config` shape includes top-level `AgentName`, `Version`, `NATS`, `JetStream`, `Subjects`, `KV`, `Timeouts`, `Retry`, and `Execution`, so map the YAML fields to those existing library types.

## Loader API

Implement a small public API inside the internal config package:

```go
func ResolvePath(flagPath string) string
func Load(path string) (*AppConfig, error)
func LoadResolved(flagPath string) (*AppConfig, string, error)
```

Behavior:

```text
Load:
  - read YAML file
  - unmarshal into AppConfig
  - apply defaults
  - validate
  - return config

LoadResolved:
  - resolve path
  - call Load
  - return config and resolved path
```

Keep package names clear:

```go
package config
```

## Main Program Behavior

`cmd/vyos-nats-agent/main.go` should:

```text
- parse --config
- parse --validate-config
- call config.LoadResolved
- call cfg.ToAgentCoreConfig()
- print "configuration valid" when --validate-config is used and config conversion succeeds
- print a phase-placeholder message when --validate-config is not used
```

Do not start the agent.

Do not connect to NATS.

Do not create renderer/apply/action packages.

## Error Handling

Keep error handling simple and Go-idiomatic.

Use `fmt.Errorf("...: %w", err)` where useful.

Do not introduce a heavy custom error framework in this phase.

## Code Style

Keep code minimal and readable.

Avoid unnecessary abstractions.

Avoid global mutable state.

Do not create large files.

Use clear function names.

Use comments only where they clarify intent.

Run `gofmt` on all generated Go files.

## Deliverables

At the end of this phase, the repository should contain only the minimal Go project structure and config loader needed for later phases.

Expected changed or added files:

```text
go.mod
go.sum
cmd/vyos-nats-agent/main.go
internal/config/config.go
internal/config/defaults.go
internal/config/loader.go
internal/config/validate.go
internal/config/convert.go
internal/config/print.go
internal/config/redact.go
```

Do not modify `README.md`, `SPEC.md`, or `config.example.yaml` unless there is a direct mismatch preventing implementation.

## Final Response Required From Codex

After coding, summarize:

```text
- files created or changed
- config path resolution behavior
- how YAML config maps to agentcore.Config
- what is intentionally not implemented in this phase
```
