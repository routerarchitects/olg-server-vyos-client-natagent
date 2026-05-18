# Codex Prompt: Generate Phase 3 Unit Tests

## Purpose

Generate focused unit tests for the Phase 3 configure flow and local state store.

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

Add unit tests only for behavior implemented in Phase 3:

- configure service orchestration
- desired config validation
- local applied UUID state handling
- placeholder render/apply orchestration through interfaces
- configure status/result publishing behavior
- JSON file-backed state store behavior

Do not add integration tests, smoke tests, real NATS tests, real VyOS tests, action tests, or future worker-queue tests.

## Files to inspect

Before writing tests, inspect the current implementation:

```text
internal/configure/service.go
internal/configure/types.go
internal/state/store.go
internal/state/file_store.go
internal/renderer/renderer.go
internal/apply/engine.go
```

Also inspect any existing tests in the repository and follow the established style.

## Files to add

Add focused unit tests beside the packages under test:

```text
internal/configure/service_test.go
internal/state/file_store_test.go
```

Use package-local tests unless the repository already follows a different pattern.

## Configure service test requirements

Cover the implemented configure lifecycle:

```text
receive configure notification
  -> publish received status
  -> load desired config
  -> validate desired target and UUID
  -> load local state
  -> skip if already applied
  -> render
  -> apply
  -> save state after successful apply
  -> publish status/result
```

Use small fake implementations for service dependencies. Do not use mock frameworks or new dependencies.

### Positive coverage

Include tests for:

- new desired config is rendered, applied, saved, and publishes success
- already-in-sync UUID skips render/apply/save and publishes success
- expected status stages are emitted for the successful flow

### Negative coverage

Include tests for:

- desired config load failure
- nil/missing desired config
- desired target mismatch
- desired UUID mismatch
- state load failure
- render failure
- apply failure
- state save failure after apply succeeds
- status/result publish failure behavior as currently implemented

### Important assertions

Assert behavior, not internal trivia.

Verify where applicable:

- render call count
- apply call count
- save call count
- saved target and applied UUID
- success/failure result
- stable error code values already implemented by the service
- important status stages such as:
  - `received`
  - `loading_desired`
  - `rendering`
  - `rendered`
  - `applying`
  - `applied`
  - `already_in_sync`
  - `failed`

Do not assert log lines or duration values.

## State store test requirements

Cover the implemented JSON file state behavior.

### Positive coverage

Include tests for:

- missing state file returns empty state
- save then load round-trips state fields
- save creates parent directories
- saved state file uses owner-only permissions, expected mode `0600`

### Negative coverage

Include tests for:

- nil context
- canceled context
- empty state path
- malformed JSON state file

### Important assertions

Verify where applicable:

- loaded state matches saved state
- missing file returns zero-value state
- malformed JSON returns an error
- invalid path/context returns an error
- file mode after save is `0600`

## Production code adjustment allowed

If the state store still writes files using `0644`, change it to `0600`.

Do not otherwise change production behavior unless required to fix a real test-discovered issue.

Do not change:

- state schema
- configure flow order
- when state is saved
- status/result semantics
- renderer/apply interfaces
- smoke scripts

## Boundaries

Do not implement or test:

- configure worker queue
- startup reconcile
- desired-config watch
- real VyOS rendering
- real VyOS apply
- action execution
- raw NATS behavior
- custom KV behavior
- shell command execution
- network probing
- integration or end-to-end behavior

Unit tests must not start external services or require local NATS tooling.

## Quality expectations

Keep the test suite minimal but sufficient.

Prefer clear standalone tests for different failure paths unless a table-driven test improves readability.

Use fixed timestamps where timestamps are relevant.

Use `t.TempDir()` for filesystem tests.

Use `context.Background()` for normal tests and explicit canceled contexts for cancellation tests.

Avoid brittle full error-string assertions unless the exact text is an implemented contract.

## Validation

Run:

```bash
gofmt -w internal/configure/service_test.go internal/state/file_store_test.go internal/state/file_store.go
go test ./...
go build ./...
```

Do not claim a command passed unless it actually ran.

## Expected summary

After completing the task, summarize:

```text
Files added:
- internal/configure/service_test.go
- internal/state/file_store_test.go

Files modified:
- internal/state/file_store.go, only if permission changed to 0600

Positive cases covered:
- ...

Negative cases covered:
- ...

Validation run:
- ...

Deferred intentionally:
- configure worker queue tests
- real VyOS render/apply tests
- integration/smoke tests
- action tests
```
