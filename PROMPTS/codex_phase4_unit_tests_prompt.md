# Codex Prompt: Generate Phase 4 Unit Tests

## Purpose

Generate focused unit tests for the Phase 4 action flow.

This prompt is feature-specific and should be used together with the repository’s base test standard:

```text
TESTS_STANDARD_TEMPLATE.md
```

Follow that standard for:

- TC-style test comments
- naming conventions
- helper style
- assertion style
- package placement
- positive/negative coverage balance
- unit-test boundaries

## Scope

Add unit tests only for behavior implemented in Phase 4:

- action service orchestration
- action command validation
- enabled/supported action handling
- placeholder trace executor orchestration through interfaces
- action status/result publishing behavior
- placeholder trace executor behavior

Do not add integration tests, smoke tests, real NATS tests, real VyOS tests, configure tests, state-store tests, startup reconcile tests, or future worker-queue tests.

## Files to inspect

Before writing tests, inspect the current implementation:

```text
internal/actions/*.go
internal/agent/agent.go
internal/agent/handlers.go
internal/config/*.go
```

Also inspect any existing tests in the repository and follow the established style.

## Files to add

Add focused unit tests beside the packages under test:

```text
internal/actions/service_test.go
internal/actions/placeholder_test.go
```

If the placeholder trace executor already has a different file name or package layout, place its tests beside the implementation and keep the test file name consistent with the repository style.

Use package-local tests unless the repository already follows a different pattern.

## Action service test requirements

Cover the implemented action lifecycle:

```text
receive action command
  -> publish received status
  -> validate target/action
  -> check action is enabled/supported
  -> execute placeholder trace action through the executor interface
  -> publish executing/completed or failure status
  -> publish success/failure result
```

Use small fake implementations for service dependencies. Do not use mock frameworks or new dependencies.

### Positive coverage

Include tests for:

- enabled `trace` action executes successfully and publishes success
- action payload is passed to the trace executor
- trace executor output is included in the success result payload when implemented
- expected status stages are emitted for the successful flow

### Negative coverage

Include tests for:

- nil context
- canceled context
- nil or malformed action command, if the implemented API permits this case
- target mismatch, if the service validates target ownership
- unsupported action
- disabled action
- trace executor failure
- status/result publish failure behavior as currently implemented

### Important assertions

Assert behavior, not internal trivia.

Verify where applicable:

- executor call count
- executor received payload
- success/failure result
- result target
- result `rpc_id`
- result command type is `action`
- result action is `trace`
- result payload contains the placeholder trace output when implemented
- stable error code values already implemented by the service
- important status stages such as:
  - `received`
  - `executing`
  - `completed`
  - `failed`

If the implementation uses different stage names, assert the current implemented constants/values instead of inventing new ones.

Do not assert log lines or duration values.

## Placeholder trace executor test requirements

Cover the implemented placeholder trace behavior.

### Positive coverage

Include tests for:

- trace executor returns deterministic output for a valid payload
- returned output is valid JSON if the implementation promises JSON output
- returned output includes the important request/payload fields that the implementation currently preserves

### Negative coverage

Include tests for:

- nil context
- canceled context
- invalid payload, only if the implementation validates payload structure

### Important assertions

Verify where applicable:

- output is non-empty
- output is deterministic for the same input
- output can be decoded as JSON if JSON output is part of the implementation contract
- context cancellation returns an error when the implementation checks context

Do not add real network probing, DNS lookup, ping, traceroute, shell command execution, or VyOS command execution.

## Production code adjustment allowed

Do not change production behavior unless required to fix a real compile or test-discovered issue in the existing Phase 4 implementation.

If a production change is required, keep it minimal and directly related to making the implemented Phase 4 behavior testable or correct.

Do not change:

- public action command/result/status schemas
- action flow order
- configure flow behavior
- state store behavior
- renderer/apply behavior
- handler registration architecture
- smoke scripts
- README/SPEC except for a tiny correction required by a real mismatch

## Boundaries

Do not implement or test:

- configure flow
- configure worker queue
- startup reconcile
- desired-config watch
- local applied UUID state
- real VyOS rendering
- real VyOS apply
- real trace, ping, traceroute, rtty, or shell execution
- raw NATS behavior
- custom KV behavior
- integration or end-to-end behavior

Unit tests must not start external services or require local NATS tooling.

## Quality expectations

Keep the test suite minimal but sufficient.

Prefer clear standalone tests for different failure paths unless a table-driven test improves readability.

Use fixed timestamps where timestamps are relevant.

Use `context.Background()` for normal tests and explicit canceled contexts for cancellation tests.

Avoid brittle full error-string assertions unless the exact text is an implemented contract.

Use fake publishers/executors to verify orchestration instead of relying on logs.

## Validation

Run:

```bash
gofmt -w internal/actions/service_test.go internal/actions/placeholder_test.go
go test ./...
go build ./...
```

If production files are modified, include them in the `gofmt` command.

Do not claim a command passed unless it actually ran.

## Expected summary

After completing the task, summarize:

```text
Files added:
- internal/actions/service_test.go
- internal/actions/placeholder_test.go

Files modified:
- ...

Positive cases covered:
- ...

Negative cases covered:
- ...

Validation run:
- ...

Deferred intentionally:
- configure tests
- state-store tests
- startup reconcile tests
- real VyOS action execution tests
- integration/smoke tests
```

