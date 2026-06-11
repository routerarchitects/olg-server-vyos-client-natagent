# Phase 3: Configure Workflow Correctness Tests

## Purpose

Read `TDD_SPEC.md`, the Phase 1/Phase 2 handoff summary, and the current codebase before making changes.

This phase must add focused unit/TDD coverage for the VyOS Agent configure workflow correctness.

Phase 1 already created reusable test utilities under `internal/testutil`.
Phase 2 already covered configuration loading/validation/defaults/conversion and local state load/save behavior.

Now Phase 3 must prove that the configure workflow itself behaves correctly on the successful path and on safe idempotency paths.

## Scope

Implement only Phase 3 tests and any minimal test-only wiring required to make those tests compile.

This phase should focus on configure workflow behavior using injected fakes/stubs.

Do not implement real VyOS behavior.
Do not add real NATS integration.
Do not add action workflow tests.
Do not add renderer/apply adapter mapping tests.
Do not add logging/security tests.
Do not add restart/concurrency/load tests.

Those belong to later phases.

## Main Goal

Prove that the configure service does the correct thing when configuration should be applied successfully, and when the desired configuration is already applied.

The core behavior to prove is:

```text
configure request received
-> desired config is rendered
-> rendered config is prepared/applied
-> state is saved only after apply succeeds
-> success result/status is published
```

And for idempotency:

```text
desired config UUID already matches applied state
-> render is skipped
-> apply is skipped
-> state save is skipped
-> already-in-sync result/status is published
```

## Expected Files

Prefer adding tests in the configure/service area. Use the actual existing package layout from the repository.

Likely file names:

```text
internal/configure/phase3_configure_workflow_test.go
```

or, if the configure service lives elsewhere, place the test beside the real service implementation.

Use the existing Phase 1 helpers from:

```text
internal/testutil/
```

## Test Infrastructure To Use

Use existing test utilities wherever possible:

```go
testutil.FakeRenderer
testutil.FakeApplyBackend
testutil.FakeStateStore
testutil.ResultRecorder
testutil.StatusRecorder
testutil.EventRecorder
testutil.OrderRecorder
testutil.Fixtures
```

Use only the names/types that actually exist in the repository. If a helper name differs from this prompt, adapt to the existing implementation.

Do not duplicate fake implementations unless absolutely necessary.

If a small missing test helper is required, add it under `internal/testutil` only if it is reusable for later phases. Keep such additions minimal and document them.

## Required Test Cases

### 1. Configure happy path applies desired config

Add a test proving:

```text
given a valid desired config with a new UUID/config ID
when configure is handled
then renderer is called exactly once
and apply prepare/apply is called exactly once
and state is saved exactly once
and success result/status is published
and no failure result/status is published
```

Assertions should check:

```text
renderer call count
apply prepare/apply call count
state save call count
saved UUID/config ID
success result/status publication
absence of failure result/status
```

### 2. State is saved only after apply succeeds

Add a test proving operation ordering.

Expected successful order:

```text
render
prepare/apply
state_save
publish_success
```

Use `EventRecorder` or `OrderRecorder` if available.

At minimum, prove that state save happens after apply.

This matters because the agent must never checkpoint a UUID before the configuration is actually applied.

### 3. Configure publishes success after state save

Add a test proving that success is published only after the state checkpoint has been attempted successfully.

Expected order:

```text
apply
state_save
publish_success
```

If the current service combines status/result publication differently, assert the closest existing success publication behavior.

Do not invent a new production event model just for this test.

### 4. Already-in-sync skips render/apply/save

Add a test proving:

```text
given local state already has applied_uuid equal to requested UUID/config ID
when configure is handled
then renderer is not called
and apply is not called
and state is not saved
and an already-in-sync result/status is published
```

Assertions should check:

```text
renderer call count == 0
apply prepare/apply call count == 0
state save call count == 0
already-in-sync result/status exists
failure result/status does not exist
```

If the current service reports already-in-sync as a normal success result with a message/reason, assert that exact current contract.

Do not change the public result contract unless existing design requires it.

### 5. Repeated successful configure is idempotent

Add a test proving:

```text
first configure applies successfully and saves state
second configure with the same UUID/config ID does not apply again
```

Expected behavior:

```text
first call:
  render called once
  apply called once
  state saved once
  success published

second call:
  render not called again
  apply not called again
  state not saved again
  already-in-sync or idempotent success published
```

This test may use the fake state store's saved state behavior if it persists saved UUIDs in memory.

If FakeStateStore records attempted saves but does not automatically update loaded state, explicitly seed the fake state before the second call, or enhance the fake only if that behavior is generally useful and safe.

### 6. Invalid desired config is rejected before render/apply

Add a test proving:

```text
given malformed or invalid desired config payload
when configure is handled
then renderer is not called
and apply is not called
and state is not saved
and failure result/status is published
```

Use existing fixtures such as invalid payload/minimal invalid desired config if available.

This is not a deep schema validation test. Phase 2 already covered config package validation.

Here, only prove configure workflow fail-fast behavior before side effects.

## Optional But Useful Test Cases

Add these only if the current service API makes them straightforward:

### 7. Configure loads state before deciding idempotency

Prove the workflow checks existing state before deciding whether to render/apply.

Expected rough order:

```text
state_load
render
apply
state_save
publish_success
```

For already-in-sync:

```text
state_load
publish_already_in_sync
```

### 8. Configure result preserves correlation identifiers

If configure inputs include `rpc_id`, `uuid`, `target`, or similar identifiers, assert that the success/already-in-sync/failure result preserves them.

## Out of Scope For Phase 3

Do not implement or test these in Phase 3:

```text
renderer failure workflow
apply failure workflow
state-save failure workflow after apply
action workflow tests
renderer adapter mapping tests
apply adapter mapping tests
NATS publish/subscribe integration
real NATS
real VyOS
logging/security behavior
restart/reconciliation behavior
concurrency/race/load tests
```

Renderer failure, apply failure, and state-save failure belong to Phase 4.

## Development Rules

1. Keep Phase 3 reviewable.
2. Prefer tests over production changes.
3. Do not redesign the configure service unless tests expose a small missing injection seam.
4. If production changes are required, keep them minimal and explain why.
5. Do not change real mode behavior unless necessary to support existing design.
6. Do not make placeholder mode behave like real mode.
7. Do not add broad refactors.
8. Keep tests deterministic and CI-friendly.
9. Use `t.TempDir()` for any filesystem needs.
10. Avoid sleeping/time-based assertions unless unavoidable.

## Expected Test Style

Use clear test names such as:

```go
func TestConfigureWorkflowSuccessAppliesAndSavesState(t *testing.T)
func TestConfigureWorkflowSavesStateAfterApply(t *testing.T)
func TestConfigureWorkflowAlreadyInSyncSkipsApply(t *testing.T)
func TestConfigureWorkflowRepeatedSameUUIDIsIdempotent(t *testing.T)
func TestConfigureWorkflowInvalidDesiredConfigFailsAtRendererBoundary(t *testing.T)
```

Use table-driven tests only if they improve readability.

For workflow tests, explicit individual tests are acceptable and often clearer.

## Expected Assertions

Each test should assert both positive and negative behavior.

Example:

```text
Positive:
- expected result/status exists
- expected call count is correct
- expected saved UUID is correct

Negative:
- no unexpected failure result
- no apply call on invalid/already-in-sync path
- no state save before apply
```

## Running Tests

After implementation, run:

```bash
go test ./...
```

If the repository already supports race testing and the tests are not too slow, also run:

```bash
go test ./... -race
```

Do not require external services for this phase.

## Final Response Required From Codex

After making changes, summarize:

1. Files added or modified.
2. Each Phase 3 test added.
3. Which behavior each test proves.
4. Whether any production code was changed.
5. Any assumptions made about the current configure service contract.
6. Commands run and whether they passed.
7. What remains for Phase 4.

## Acceptance Criteria

Phase 3 is complete when the codebase has tests proving:

```text
[ ] configure happy path renders desired config
[ ] apply is called exactly once on new config
[ ] state is saved after apply succeeds
[ ] success is published after state save
[ ] already-in-sync skips render/apply/save
[ ] repeated same UUID/config ID is idempotent
[ ] invalid desired config fails before side effects
[ ] tests use Phase 1 fakes/recorders
[ ] tests do not require real VyOS
[ ] tests do not require real NATS
[ ] go test ./... passes
```

## Reviewer-Facing Summary

This phase proves configure workflow correctness using deterministic unit tests.

It verifies that a new desired configuration is rendered, applied, checkpointed, and reported successfully in the correct order.

It also verifies safe idempotency: when the requested UUID/config ID is already applied, the agent does not render or apply again.

Failure-path behavior for renderer/apply/state-save errors is intentionally deferred to Phase 4.
