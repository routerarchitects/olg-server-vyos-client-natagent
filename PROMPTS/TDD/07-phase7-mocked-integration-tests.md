# Phase 7: Mocked Integration Tests With Real NATS

## Purpose

Read these files before making changes:

1. `TDD_SPEC.md`
2. `PROMPTS/TDD/PHASE3_COVERAGE.md`
3. `PROMPTS/TDD/PHASE4_COVERAGE.md`
4. `PROMPTS/TDD/PHASE5_COVERAGE.md`
5. `PROMPTS/TDD/PHASE6_COVERAGE.md`
6. `.github/workflows/ci.yml`
7. `internal/agent/*`
8. `internal/configure/*`
9. `internal/actions/*`
10. `internal/renderervyos/*`
11. `internal/applyvyos/*`
12. `internal/testutil/*`
13. Existing smoke scripts under `tests/scripts/`

We are implementing ONLY Phase 7 from `TDD_SPEC.md`:

```text
Phase 7: Mocked Integration Tests
```

Phase 7 validates full agent flow without requiring a real VyOS device.

It should use:

```text
real NATS / JetStream / KV path where practical
real nats-agent-core integration path
real handler registration / subject routing path where practical
fake renderer / fake apply backend / fake action executor
temporary local state
```

This phase should also integrate the new mocked integration tests into CI/CD.

## Phase 7 Mental Model

Earlier phases tested individual pieces directly:

```text
service.Handle(...)
adapter.Apply(...)
executor.Execute(...)
```

Phase 7 should test the wiring:

```text
publish command to NATS
-> handler receives command
-> service runs
-> desired config is read from KV
-> fake backend is invoked
-> status/result is published
-> test receives status/result from NATS
```

The key idea:

```text
NATS is real.
JetStream/KV is real where possible.
The VyOS platform backend is fake.
```

This is the bridge between unit tests and real VyOS lab validation.

## Strict Scope

Implement only mocked integration tests and CI wiring needed for Phase 7.

Do NOT implement:

- real VyOS lab tests
- real platform apply
- real trace/rtty execution
- broad lifecycle/restart suite
- logging/security suite
- concurrency/load suite
- Phase 8 or Phase 9 work

Do not require a real VyOS device.

Do not require external NATS infrastructure. Tests must start/use a local `nats-server` or documented CI-installed nats-server.

## Required Phase 7 Requirements

From `TDD_SPEC.md`, cover or explicitly document:

```text
INT-001 TestIntegrationConfigureFlowWithMockBackend
INT-002 TestIntegrationRealModeUsesRendererAndApplyAdapters
INT-003 TestIntegrationPlaceholderAndRealMockFlowAreEquivalent
INT-004 TestIntegrationConfigureFailurePublishesFailureStatus
INT-005 TestIntegrationActionTraceFlowWithMockExecutor
INT-006 TestIntegrationStatusResultSubjectsReceiveExpectedMessages
INT-007 TestIntegrationConfigureReadsDesiredConfigFromKV
INT-008 TestIntegrationMissingDesiredConfigPublishesFailure
INT-009 TestIntegrationAgentCoreConnectionFailureHandled
```

All P0 items should be covered unless there is a clear architecture reason to document partial coverage or deferral.

`INT-009` is P1 and can be covered or explicitly deferred if startup/connection failure behavior belongs to broader lifecycle handling.

## Recommended Location

Use one of these approaches depending on current repo style:

```text
internal/integration/mocked_agent_flow_integration_test.go
```

or:

```text
tests/integration/mocked_agent_flow_integration_test.go
```

If integration tests require build tags, use:

```go
//go:build integration
```

and run them separately in CI with:

```bash
go test -tags=integration ./...
```

If they are fast and reliable enough, they can run under normal:

```bash
go test ./...
```

Prefer a separate integration job or step if nats-server is required.

## CI/CD Requirement

Update `.github/workflows/ci.yml` so Phase 7 tests run automatically.

Current CI already installs `nats-server` in the smoke job and runs NATS smoke scripts.

Add one of these patterns:

### Option A: Add integration test step to existing smoke job

```yaml
- name: Mocked integration tests
  timeout-minutes: 5
  run: go test -tags=integration ./...
```

Use this if tests start their own nats-server or can use the installed binary.

### Option B: Add a separate integration-tests job

```yaml
integration-tests:
  name: Mocked Integration Tests
  runs-on: ubuntu-latest
  needs: test-and-build
  timeout-minutes: 10
  steps:
    - checkout
    - setup go
    - go mod download
    - install nats-server
    - run go test -tags=integration ./...
```

Prefer Option B if tests are heavier or need clearer CI separation.

Requirements:

```text
CI must install nats-server if needed.
CI must run the Phase 7 integration tests.
CI must fail if integration tests fail.
Use timeouts to prevent hanging.
```

## Integration Test Infrastructure

Implement small, deterministic helpers.

Useful helpers:

```text
startTestNATSServer(t)
waitForStatus(...)
waitForResult(...)
publishConfigure(...)
publishAction(...)
storeDesiredConfig(...)
newIntegrationRuntime(...)
```

Rules:

```text
use random/free ports or per-test unique NATS port
use t.TempDir() for state
use context.WithTimeout
clean up nats-server process
avoid sleeps where possible; use polling/subscription with deadlines
avoid global shared state
```

If existing smoke scripts already start nats-server, do not rely on scripts from Go tests unless that is the established pattern. Prefer direct Go integration helpers or script-level CI smoke if simpler.

## Requirement Details

---

# INT-001: Configure flow with mock backend

Add:

```go
func TestIntegrationConfigureFlowWithMockBackend(t *testing.T)
```

Expected:

```text
real NATS path is used
desired config is stored
configure notification is published
agent/runtime handler receives it
fake renderer is called
fake apply backend is called
state is saved
success status/result is received from NATS
```

Assertions:

```text
renderer calls == 1
apply calls == 1
state contains requested UUID
success result received
target/uuid/rpc_id preserved
```

---

# INT-002: Real mode uses renderer and apply adapters

Add:

```go
func TestIntegrationRealModeUsesRendererAndApplyAdapters(t *testing.T)
```

Expected:

```text
when configured in real/mock mode
handler path uses renderer/apply adapters, not placeholder bypass
fake adapter/backend call counters prove invocation
```

Assertions:

```text
renderer adapter/backend called
apply adapter/backend called
success result received
no placeholder-only bypass occurred
```

If current architecture cannot inject fake real-mode adapters through runtime config without meaningful refactor, document partial coverage and exactly what is missing.

Do not require real VyOS.

---

# INT-003: Placeholder and real mock flow are equivalent

Add:

```go
func TestIntegrationPlaceholderAndRealMockFlowAreEquivalent(t *testing.T)
```

Expected:

```text
placeholder mode configure flow and real-mock adapter configure flow both produce equivalent externally visible status/result/state behavior
```

Compare:

```text
final result = success
status stages contain expected successful configure lifecycle
state saved to requested UUID
target/uuid/rpc_id preserved
```

Do not compare internal implementation details unless needed.

---

# INT-004: Configure failure publishes failure status

Add:

```go
func TestIntegrationConfigureFailurePublishesFailureStatus(t *testing.T)
```

Expected:

```text
real NATS command path triggers configure service
fake renderer or apply backend fails
failure status/result is received from NATS
success result is not received
state is not falsely updated
```

Assertions:

```text
failure status received
failure result received
error_code matches render_failed or apply_failed
no final success result
state does not contain requested UUID if apply did not succeed
```

---

# INT-005: Action trace flow with mock executor

Add:

```go
func TestIntegrationActionTraceFlowWithMockExecutor(t *testing.T)
```

Expected:

```text
real NATS action command path triggers action service
fake trace executor is called
statuses received: received -> executing -> completed
success result received
```

Assertions:

```text
executor calls == 1
status order stable
result command_type = action
result action = trace
target/rpc_id preserved
```

---

# INT-006: Status/result subjects receive expected messages

Add:

```go
func TestIntegrationStatusResultSubjectsReceiveExpectedMessages(t *testing.T)
```

Expected:

```text
test subscribers on status/result subjects receive expected envelopes
schemas include required fields
messages arrive on expected subjects
```

Assert for configure or action, preferably both if easy:

```text
version
rpc_id
target
uuid for configure result
action for action result
status/stage
result
command_type
timestamp present if applicable
```

This proves subject contract and envelope shape.

---

# INT-007: Configure reads desired config from KV

Add:

```go
func TestIntegrationConfigureReadsDesiredConfigFromKV(t *testing.T)
```

Expected:

```text
desired config is stored in JetStream KV
configure notification contains only trigger/correlation metadata
agent loads desired config from KV
renderer receives that desired payload
```

Assertions:

```text
fake renderer input payload == KV stored payload
configure notification itself did not manually inject desired payload into service
success result received
```

This is one of the most important Phase 7 tests.

---

# INT-008: Missing desired config publishes failure

Add:

```go
func TestIntegrationMissingDesiredConfigPublishesFailure(t *testing.T)
```

Expected:

```text
configure notification is published
desired config is absent from KV
agent publishes failure status/result
apply is not called
```

Assertions:

```text
failure result error_code = desired_config_missing or current equivalent
apply calls == 0
no success result
```

---

# INT-009: Agent-core connection failure handled

P1.

Add:

```go
func TestIntegrationAgentCoreConnectionFailureHandled(t *testing.T)
```

Expected if supported:

```text
invalid NATS URL / unavailable server
agent startup returns clear error
test does not hang
```

If connection lifecycle belongs to nats-agent-core or broader runtime/lifecycle testing, document as deferred with reason.

Do not create flaky connection tests.

## Smoke Test Cross-Coverage

The current CI has smoke scripts for real NATS configure/action. Keep them passing.

If Phase 7 Go integration tests overlap existing smoke scripts, document the relationship:

```text
Smoke scripts prove CLI/script path.
Phase 7 Go tests prove integration behavior with assertions and fake backends.
```

Do not delete existing smoke scripts.

## Coverage Documentation

Add:

```text
PROMPTS/TDD/PHASE7_COVERAGE.md
```

Map every item:

```text
INT-001
INT-002
INT-003
INT-004
INT-005
INT-006
INT-007
INT-008
INT-009
```

For each row include:

```text
ID
Status: Covered / Partially Covered / Deferred
Test file or script
Test name or CI step
Notes
```

Also include CI/CD mapping:

```text
CI job
CI step
command
timeout
nats-server setup
```

## CI/CD Documentation

Update or add documentation explaining how to run Phase 7 locally:

```text
go test -tags=integration ./...
```

or the actual command chosen.

If a local nats-server binary is required, document:

```text
go install github.com/nats-io/nats-server/v2@v2.10.22
```

If tests start nats-server themselves, document that too.

## Production Code Rules

Prefer using existing public/runtime APIs.

Production code changes are allowed only if needed to:

```text
inject fake renderer/apply/action backends for integration testing
make runtime construction testable
expose clean lifecycle start/stop hooks
make subject/KV behavior explicit
fix wiring bugs discovered by tests
```

Keep changes minimal.

Do not implement real VyOS behavior.

Do not redesign nats-agent-core contracts from this repo.

## Commands To Run

After implementation, run:

```bash
go test ./...
go test -race ./...
go build ./...
```

Also run the Phase 7 integration command, for example:

```bash
go test -tags=integration ./...
```

Run existing smoke scripts or rely on CI if they are already part of GitHub Actions.

## Final Codex Response Required

After implementation, summarize:

1. Files added.
2. Files modified.
3. Phase 7 integration tests added.
4. INT-* IDs covered.
5. INT-* IDs partially covered/deferred and why.
6. CI/CD changes.
7. How to run Phase 7 locally.
8. Any production code changes.
9. Test helper changes.
10. Commands run and results.
11. What remains for Phase 8.

## Acceptance Criteria

Phase 7 is complete when:

```text
[ ] INT-001 configure flow with mock backend covered
[ ] INT-002 real/mock mode proves renderer/apply adapters are invoked
[ ] INT-003 placeholder and real-mock flow equivalence covered
[ ] INT-004 configure failure publishes failure through NATS
[ ] INT-005 action trace flow with mock executor covered
[ ] INT-006 status/result subjects receive expected messages
[ ] INT-007 configure reads desired config from KV
[ ] INT-008 missing desired config publishes failure
[ ] INT-009 connection failure covered or explicitly deferred
[ ] real NATS/JetStream path is tested locally
[ ] no real VyOS dependency introduced
[ ] Phase 7 tests are integrated into CI/CD
[ ] CI installs/uses nats-server as needed
[ ] tests have timeouts and do not hang
[ ] PHASE7_COVERAGE.md maps all INT items and CI command
[ ] go test ./... passes
[ ] go test -race ./... passes
[ ] go build ./... passes
[ ] Phase 7 integration command passes
```
