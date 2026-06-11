# VyOS Agent Test Hardening TDD Specification

## 1. Purpose

This document is the Test Driven Development (TDD) specification for the VyOS Agent test-hardening phase.

The phase is complete only when the test cases in this specification are implemented, automated, and passing.

This specification combines:

- the original intent of the test-hardening design document,
- the reviewer's repo-specific test feedback,
- the known current coverage,
- the missing production-readiness gaps,
- and a clear implementation order for building the tests.

The purpose is simple:

> Move from "the agent works in the happy path" to "the agent behaves safely and predictably in production-like failure conditions."

---

## 2. Development Model

This phase should be implemented using a TDD-style approach:

1. Pick one section from this document.
2. Add the required test cases for that section.
3. Run the tests.
4. Fix or improve the implementation until tests pass.
5. Move to the next section only after the current section is passing.

The test-hardening task is not complete when code is written. It is complete when the tests pass and prove the expected behavior.

---

## 3. Scope

### 3.1 In Scope

- Configuration loading and validation tests.
- Agent lifecycle tests.
- Configure workflow tests.
- Configure failure handling tests.
- Action workflow tests.
- State management tests.
- Renderer and apply adapter tests.
- Mocked real-mode integration tests.
- NATS integration smoke tests.
- Logging and security tests.
- Retry and idempotency tests.
- Restart and persistence tests.
- Concurrency and race tests.
- Lightweight large payload/load tests.
- Real VyOS lab smoke validation as release evidence.

### 3.2 Out of Scope

- Rewriting the agent architecture.
- Changing business behavior unless tests expose a bug.
- Full performance benchmarking.
- Replacing real VyOS lab validation with mocks.
- Simulating every VyOS command behavior inside placeholder mode.

---

## 4. Core Clarification: Placeholder, Fake, and Real Mode

### 4.1 Current Placeholder

The current placeholder implementation mostly returns success.

It is useful for:

- CI validation,
- basic workflow testing,
- running the agent without a real VyOS device,
- proving the happy path.

But it is not enough for production-grade testing because it cannot simulate failure conditions.

### 4.2 Required Test Double Behavior

For this phase, test doubles should be added or enhanced.

A test double may be a fake, mock, spy, or stub. It should allow tests to control behavior.

Required capabilities:

- return success,
- return renderer failure,
- return apply failure,
- simulate state save failure,
- count calls,
- capture inputs,
- validate UUID and target propagation,
- simulate large payloads,
- simulate prepare/apply behavior,
- help verify ordering.

### 4.3 Real Mode vs Placeholder Mode

Placeholder and real mode should be interchangeable at the interface level.

Their purpose is different:

| Mode | Purpose | Used in Production |
|---|---|---|
| Placeholder | Simple test/dev implementation | No |
| Fake/Mock | Controlled testing implementation | No |
| Real | Actual renderer/apply backend | Yes |

Important rule:

> CI should not require a real VyOS device. CI should use placeholder/fake/mock implementations to prove agent behavior. Real VyOS tests are final lab validation, not the main test strategy.

---

## 5. Priority Definitions

| Priority | Meaning | CI Requirement |
|---|---|---|
| P0 | Must-have before considering this phase complete | Must run on every PR |
| P1 | Strongly recommended before production | Should run in CI or nightly |
| P2 | Useful extended validation | Can run manually or nightly |

---

## 6. Test Type Definitions

| Type | Meaning |
|---|---|
| Positive | Valid input and success path |
| Negative | Invalid input or expected failure path |
| Recovery | Retry, restart, or recovery after failure |
| Safety | Prevents unsafe side effects, data leaks, or false state updates |
| Integration | Multiple components tested together |
| Smoke | Script-level validation of major path |
| Concurrency | Race, ordering, or parallel execution behavior |
| Load | Large input or burst behavior sanity |

---

## 7. Known Current Coverage

The repo already has useful coverage, but it is mostly happy-path and smoke-level.

| Area | Current Status | Meaning |
|---|---|---|
| Basic unit tests | Partially covered | `go test ./...` exists, but depth needs improvement |
| Config happy path | Covered by script | Config validation script exists |
| Configure E2E success | Covered | Real NATS configure smoke exists |
| Configure basic idempotency | Covered | Same UUID happy-path resubmit is checked |
| State write success | Covered | State file updated in happy path |
| Action E2E success | Covered | Real NATS action smoke exists |
| Action status sequence happy path | Covered | received -> executing -> completed |
| Graceful shutdown | Partially covered | Smoke tests indirectly validate it |
| Failure injection | Missing | Highest risk gap |
| State corruption | Missing | Must be added |
| State save failure | Missing | Critical edge case |
| Retry after failure | Missing | Must be added |
| Negative config tests | Missing | Invalid YAML, invalid values, missing fields |
| Adapter correctness | Missing or partial | Needs explicit mapping assertions |
| Logging/security | Missing | Must verify no sensitive leak |
| Concurrency/race | Missing | Needed before production confidence |

---

## 8. Required Test Infrastructure

These helper components should be implemented first to avoid duplicate test code.

### 8.1 Fake Renderer

Purpose: simulate renderer behavior without calling the real renderer library.

Required behavior:

| Capability | Reason |
|---|---|
| Return success | Happy path testing |
| Return configured error | Renderer failure testing |
| Count calls | Verify called once or not called |
| Capture inputs | Verify payload, UUID, target mapping |
| Return large output | Large config testing |
| Validate input | Invalid payload and mapping tests |

Suggested fields:

```go
type FakeRenderer struct {
    Calls  int
    Inputs []RenderInput
    Output RenderOutput
    Err    error
}
```

### 8.2 Fake Apply Backend

Purpose: simulate apply behavior without requiring VyOS.

Required behavior:

| Capability | Reason |
|---|---|
| Return success | Happy path testing |
| Return configured error | Apply failure testing |
| Count Apply calls | Verify apply called once or skipped |
| Capture input | Verify renderer output maps to apply input |
| Optional Prepare support | Validate prepare/apply flow |
| Mutate input intentionally in test | Validate apply uses correct input after prepare |

Suggested fields:

```go
type FakeApplyBackend struct {
    PrepareCalls   int
    ApplyCalls     int
    PrepareInputs  []ApplyInput
    ApplyInputs    []ApplyInput
    PrepareErr     error
    ApplyErr       error
    MutateOnPrepare bool
}
```

### 8.3 Fake State Store

Purpose: simulate local state behavior.

Required behavior:

| Capability | Reason |
|---|---|
| Load existing UUID | Already-in-sync test |
| Return missing state | First-run behavior |
| Return corrupt state error | State corruption test |
| Fail save | Critical state-save failure test |
| Count writes | Verify no write on failure |
| Capture saved state | Verify correct UUID persisted |

Suggested fields:

```go
type FakeStateStore struct {
    Current     State
    LoadErr     error
    SaveErr     error
    SaveCalls   int
    SavedStates []State
}
```

### 8.4 Status and Result Recorder

Purpose: capture status/result messages published by the agent.

Required behavior:

- capture success result,
- capture failure result,
- capture already_in_sync result,
- capture status order,
- capture target, UUID, rpc_id, action, and reason/message.

### 8.5 Log Capture

Purpose: verify safe logging.

Required behavior:

- capture info/debug/warn/error logs,
- check if raw payload appears,
- check if rendered commands appear,
- check if apply plan appears,
- check if secret-like values appear,
- verify debug logging rules.

### 8.6 Test NATS Harness

Purpose: run local integration tests without real VyOS.

Required behavior:

- start real NATS/JetStream for tests,
- create required KV bucket,
- publish configure/action messages,
- subscribe to status/result messages,
- stop and restart agent where required.

---

# 9. Configuration Tests

## 9.1 Goal

Ensure the agent loads, validates, overlays, and converts configuration correctly.

## 9.2 Target Area

- `internal/config/*`
- config loader
- config validation
- default overlay logic
- YAML to `agentcore.Config` conversion

## 9.3 Test Cases

| ID | Test Name | Priority | Type | Purpose | Expected Result |
|---|---|---|---|---|---|
| CFG-001 | `TestConfigLoadValidYAMLReturnsSuccess` | P0 | Positive | Valid YAML should load | No error; fields populated |
| CFG-002 | `TestConfigLoadMissingFileReturnsError` | P0 | Negative | Missing file should fail | Clear error returned |
| CFG-003 | `TestConfigLoadInvalidYAMLReturnsError` | P0 | Negative | Malformed YAML should fail | Parse error returned |
| CFG-004 | `TestConfigLoadPartialYAMLAppliesDefaults` | P0 | Positive | Partial config should use defaults | Missing optional fields defaulted |
| CFG-005 | `TestConfigInvalidNATSConfigFailsValidation` | P0 | Negative | Bad NATS config should fail | Validation error |
| CFG-006 | `TestConfigInvalidSubjectPatternFailsValidation` | P0 | Negative | Bad subject should fail | Validation error |
| CFG-007 | `TestConfigUnsupportedActionFailsValidation` | P0 | Negative | Unsupported action should fail | Validation error |
| CFG-008 | `TestConfigInvalidConfigureModeFailsAtParseLevel` | P0 | Negative | Invalid mode should fail early | Fails before runtime wiring |
| CFG-009 | `TestConfigDefaultConfigureModeIsPlaceholder` | P0 | Positive | Default mode should be explicit | Final config mode is placeholder |
| CFG-010 | `TestConfigRealModeSelectsRealAdapters` | P0 | Positive | Real mode should wire real adapters | Uses `renderervyos.Adapter` and `applyvyos.Adapter` |
| CFG-011 | `TestConfigPlaceholderModeSelectsPlaceholderAdapters` | P0 | Positive | Placeholder mode should wire placeholders | Uses `renderer.Placeholder` and `apply.Placeholder` |
| CFG-012 | `TestConfigDebugFlagsDoNotChangeEngineSelection` | P0 | Safety | Debug flags should not change backend | Same engine selected with/without debug flags |
| CFG-013 | `TestConfigYAMLOverridesDefaultsCorrectly` | P0 | Positive | YAML should override defaults | YAML values preserved |
| CFG-014 | `TestConfigDefaultsAreNotReappliedAfterOverlay` | P0 | Safety | Prevent overlay drift | Defaults do not override YAML values |
| CFG-015 | `TestConfigConvertsToAgentCoreConfigCorrectly` | P0 | Positive | Shared library config mapping should be correct | `agentcore.Config` matches expected values |

## 9.4 Acceptance Criteria

This section is complete when:

- invalid config fails before runtime,
- default mode explicitly resolves to placeholder,
- real and placeholder modes wire correct implementations,
- defaults and YAML overlay are deterministic,
- conversion to `agentcore.Config` is tested.

---

# 10. Agent Lifecycle Tests

## 10.1 Goal

Ensure the agent starts, registers handlers, publishes startup status, and shuts down safely.

## 10.2 Target Area

- `internal/agent/lifecycle.go`
- signal handling
- handler registration
- NATS connection management

## 10.3 Test Cases

| ID | Test Name | Priority | Type | Purpose | Expected Result |
|---|---|---|---|---|---|
| LIFE-001 | `TestAgentStartupWithValidConfigInitializesSuccessfully` | P0 | Positive | Valid config starts agent | No error; dependencies initialized |
| LIFE-002 | `TestAgentStartupRegistersConfigureHandler` | P0 | Positive | Configure handler should exist | Configure handler registered |
| LIFE-003 | `TestAgentStartupRegistersActionHandler` | P0 | Positive | Action handler should exist | Action handler registered |
| LIFE-004 | `TestAgentStartupPublishesStartupStatus` | P0 | Positive | Startup should be visible | Startup/ready status published |
| LIFE-005 | `TestAgentShutdownClosesNATSConnection` | P0 | Positive | Shutdown should clean resources | NATS connection closed/drained |
| LIFE-006 | `TestAgentShutdownOnSIGINTIsGraceful` | P1 | Recovery | SIGINT should stop cleanly | Agent exits cleanly |
| LIFE-007 | `TestAgentShutdownOnSIGTERMIsGraceful` | P1 | Recovery | SIGTERM should stop cleanly | Agent exits cleanly |
| LIFE-008 | `TestAgentStartupFailsWhenNATSUnavailable` | P1 | Negative | Bad NATS should fail clearly | Startup error; no panic |
| LIFE-009 | `TestAgentReconnectRestoresHandlers` | P1 | Recovery | Reconnect should not lose handlers | Handlers restored after reconnect |

## 10.4 Acceptance Criteria

This section is complete when:

- startup works with valid config,
- configure and action handlers are registered,
- startup status is published,
- shutdown is graceful,
- startup failure is safe and clear.

---

# 11. Configure Workflow Tests

## 11.1 Goal

Verify the configure workflow at service level, not only engine construction.

Expected successful flow:

```text
desired config received
-> desired UUID compared with local state UUID
-> UUID mismatch detected
-> renderer called once
-> apply called once
-> state saved after apply succeeds
-> success status/result published
```

## 11.2 Target Area

- configure service
- renderer interface usage
- apply interface usage
- state store
- status/result publishing
- idempotency logic

## 11.3 Test Cases

| ID | Test Name | Priority | Type | Purpose | Expected Result |
|---|---|---|---|---|---|
| CWF-001 | `TestConfigureHappyPathPlaceholderMode` | P0 | Positive | Placeholder success path | Render once; apply once; state saved; success published |
| CWF-002 | `TestConfigureHappyPathRealModeWithFakeBackend` | P0 | Integration | Real-mode path without VyOS | Real adapters invoked through fake backend; success |
| CWF-003 | `TestConfigureStateSavedAfterApply` | P0 | Safety | Verify ordering | Apply happens before state save |
| CWF-004 | `TestConfigureRendererCalledExactlyOnceForNewUUID` | P0 | Positive | Prevent duplicate render | Renderer call count is 1 |
| CWF-005 | `TestConfigureApplyCalledExactlyOnceForNewUUID` | P0 | Positive | Prevent duplicate apply | Apply call count is 1 |
| CWF-006 | `TestConfigureAlreadyInSyncSkipsRenderAndApply` | P0 | Positive | Same UUID should skip work | No render; no apply; already_in_sync success |
| CWF-007 | `TestConfigureSameConfigProcessedTwiceSecondRunSkipped` | P0 | Positive | Idempotency after success | First run applies; second skips |
| CWF-008 | `TestConfigureNewUUIDTriggersRenderAndApply` | P0 | Positive | New config should apply | Render/apply called; state updated |
| CWF-009 | `TestConfigureMissingDesiredConfigFails` | P0 | Negative | Missing desired config should not proceed | Failure published; no render/apply/state save |
| CWF-010 | `TestConfigureWrongTargetFailsOrIsIgnoredSafely` | P0 | Safety | Wrong target should not execute | No apply; safe failure or ignore per spec |
| CWF-011 | `TestConfigureEmptyUUIDFails` | P0 | Negative | Empty UUID is unsafe | Failure; no render/apply/state save |
| CWF-012 | `TestConfigureSuccessPublishesExpectedResultFields` | P0 | Positive | Result contract should be stable | Result includes target, UUID, status, message |
| CWF-013 | `TestConfigureSuccessDoesNotPublishFailure` | P0 | Safety | Avoid mixed outputs | Only success output is published |
| CWF-014 | `TestConfigureDoesNotUpdateStateBeforeApply` | P0 | Safety | Prevent false checkpoint | State not updated before apply success |
| CWF-015 | `TestConfigureMaintainsTargetAndUUIDAcrossWorkflow` | P0 | Positive | Preserve metadata | Target and UUID preserved across render/apply/result |

## 11.4 Acceptance Criteria

This section is complete when:

- happy path is proven in placeholder and mocked real mode,
- call counts are verified,
- state update order is verified,
- already-in-sync behavior is verified,
- same UUID after success skips,
- invalid inputs do not render or apply.

---

# 12. Configure Failure Handling Tests

## 12.1 Goal

Prove the configure workflow is safe when renderer, apply, or state persistence fails.

This is the most important production-readiness section.

## 12.2 Test Cases

| ID | Test Name | Priority | Type | Purpose | Expected Result |
|---|---|---|---|---|---|
| FAIL-001 | `TestConfigureRendererFailureStopsBeforeApply` | P0 | Negative | Renderer failure must stop workflow | Apply not called; state not saved; failure published |
| FAIL-002 | `TestConfigureRendererFailurePreservesPreviousState` | P0 | Safety | Failed render must not corrupt state | Previous UUID remains unchanged |
| FAIL-003 | `TestConfigureRendererFailurePublishesClearFailure` | P0 | Negative | Failure must be observable | Failure includes renderer error reason |
| FAIL-004 | `TestConfigureApplyFailureDoesNotSaveState` | P0 | Negative | Apply failure must not checkpoint UUID | State save not called; failure published |
| FAIL-005 | `TestConfigureApplyFailureAllowsRetrySameUUID` | P0 | Recovery | Failed apply should retry later | Retry renders/applies again and can succeed |
| FAIL-006 | `TestConfigureApplyFailurePreservesPreviousUUID` | P0 | Safety | Failed apply must not change local UUID | Previous UUID remains unchanged |
| FAIL-007 | `TestConfigureStateSaveFailureMarksConfigureFailed` | P0 | Negative | Critical edge case | Apply succeeds; save fails; configure reported failed |
| FAIL-008 | `TestConfigureStateSaveFailureDoesNotPersistUUID` | P0 | Safety | Failed checkpoint must not appear applied | New UUID not persisted |
| FAIL-009 | `TestConfigureStateSaveFailureRetriesSafely` | P0 | Recovery | Retry after state failure | Retry reaches apply/save again safely |
| FAIL-010 | `TestConfigureFailureDoesNotPublishSuccess` | P0 | Safety | Avoid mixed statuses | Failure path does not publish success |
| FAIL-011 | `TestConfigureFailureIncludesCorrelationData` | P0 | Safety | Failures must be traceable | Failure preserves target, UUID, rpc_id where applicable |
| FAIL-012 | `TestConfigureUnexpectedPanicRecoveredAsFailure` | P1 | Recovery | Prevent dependency panic from crashing agent | Panic becomes safe failure if recovery supported |
| FAIL-013 | `TestConfigureContextCancellationStopsWorkflow` | P1 | Recovery | Respect context cancellation | Workflow stops; no unsafe state save |
| FAIL-014 | `TestConfigureTimeoutPublishesFailure` | P1 | Recovery | Prevent stuck operation | Timeout/failure status; no unsafe state update |

## 12.3 Acceptance Criteria

This section is complete when:

- renderer failure prevents apply,
- apply failure prevents state save,
- state save failure is treated as configure failure,
- post-checkpoint reporting failure is surfaced separately from configure failure,
- retry after failure is safe,
- failed workflows never publish success,
- previous state is preserved unless apply and state save both succeed.

---

# 13. Action Workflow Tests

## 13.1 Goal

Verify action handling, especially for the currently supported `trace` action.

Expected action flow:

```text
action request received
-> action validated
-> received status published
-> executing status published
-> action executed
-> completed or failed status published
```

## 13.2 Test Cases

| ID | Test Name | Priority | Type | Purpose | Expected Result |
|---|---|---|---|---|---|
| ACT-001 | `TestActionTraceHappyPathPublishesReceivedExecutingCompleted` | P0 | Positive | Validate trace success sequence | received -> executing -> completed |
| ACT-002 | `TestActionTraceHappyPathPublishesFinalResult` | P0 | Positive | Validate final result | Completed result has expected fields |
| ACT-003 | `TestActionUnsupportedActionFails` | P0 | Negative | Unsupported action rejected | Failure; no execution |
| ACT-004 | `TestActionDisabledActionFails` | P0 | Negative | Disabled action rejected | Failure; no execution |
| ACT-005 | `TestActionExecutionFailurePublishesFailed` | P0 | Negative | Executor failure handled | received -> executing -> failed |
| ACT-006 | `TestActionMissingRequiredPayloadFails` | P0 | Negative | Invalid payload rejected | Failure; no execution |
| ACT-007 | `TestActionWrongTargetDoesNotExecute` | P0 | Safety | Wrong target should not execute | No execution; safe failure or ignore |
| ACT-008 | `TestActionStatusSequenceOrderIsStable` | P0 | Safety | Prevent out-of-order status | Status order is deterministic |
| ACT-009 | `TestActionFailureDoesNotPublishCompleted` | P0 | Safety | Avoid mixed result | Failed action does not publish completed |
| ACT-010 | `TestActionPreservesCorrelationData` | P0 | Positive | Traceability | Status/result preserves rpc_id, target, action |
| ACT-011 | `TestActionContextCancellationStopsExecution` | P1 | Recovery | Respect cancellation | Cancelled/failed status; no hang |
| ACT-012 | `TestActionTimeoutPublishesFailure` | P1 | Recovery | Prevent stuck action | Failed/timeout status |

## 13.3 Acceptance Criteria

This section is complete when:

- trace happy path is verified,
- status sequence is deterministic,
- unsupported and disabled actions fail safely,
- action failure publishes failed status,
- correlation data is preserved.

---

# 14. State Management Tests

## 14.1 Goal

Ensure local state is loaded, saved, corrupted, missing, and retried safely.

## 14.2 Target Area

- `internal/state/*`
- state file path handling
- read/write logic
- atomic write behavior if supported

## 14.3 Test Cases

| ID | Test Name | Priority | Type | Purpose | Expected Result |
|---|---|---|---|---|---|
| STATE-001 | `TestStateLoadValidFileReturnsState` | P0 | Positive | Valid state loads | State loaded successfully |
| STATE-002 | `TestStateLoadMissingFileReturnsDefaultState` | P0 | Recovery | First run behavior | Default/empty state returned |
| STATE-003 | `TestStateLoadCorruptJSONFailsSafely` | P0 | Negative | Corrupt state must not be trusted | Error or safe recovery; no blind apply decision |
| STATE-004 | `TestStateLoadInvalidUUIDFailsSafely` | P0 | Negative | Invalid state content rejected | Error or safe behavior |
| STATE-005 | `TestStateWriteAfterApplySuccessPersistsUUID` | P0 | Positive | Success checkpoint | File contains applied UUID |
| STATE-006 | `TestStateNotWrittenWhenRendererFails` | P0 | Safety | No checkpoint on render fail | Save not called |
| STATE-007 | `TestStateNotWrittenWhenApplyFails` | P0 | Safety | No checkpoint on apply fail | Save not called |
| STATE-008 | `TestStateSaveFailureReturnsError` | P0 | Negative | Write error visible | Error returned |
| STATE-009 | `TestStateSaveFailureDoesNotCreatePartialValidState` | P0 | Safety | Avoid corrupted checkpoint | No partial valid new UUID |
| STATE-010 | `TestStateReflectsLastAppliedUUIDOnly` | P0 | Safety | Last applied equals last saved | State UUID equals most recent successful apply |
| STATE-011 | `TestStateNoPartialWrites` | P1 | Safety | Atomicity | Old state remains or safe error |
| STATE-012 | `TestStatePathIsPersistentNotTmpForProductionConfig` | P1 | Safety | Avoid reboot loss | Production config does not default to `/tmp` unless explicit |

## 14.4 Acceptance Criteria

This section is complete when:

- valid, missing, and corrupt state behavior is covered,
- state is written only after apply success,
- state save failure is visible and safe,
- no false UUID checkpoint occurs,
- production state path is safe.

---

# 15. Renderer and Apply Adapter Tests

## 15.1 Goal

Validate the boundary between the agent and the real renderer/apply backend.

Adapters are fragile because they translate data between systems. These tests ensure metadata and payloads are not lost or corrupted.

## 15.2 Renderer Adapter Test Cases

| ID | Test Name | Priority | Type | Purpose | Expected Result |
|---|---|---|---|---|---|
| RAD-001 | `TestRendererAdapterMinimalPayloadMapsCorrectly` | P0 | Positive | Minimal payload mapping | Renderer input fields match expected |
| RAD-002 | `TestRendererAdapterLargePayloadMapsCorrectly` | P1 | Load | Large payload mapping | No crash; input preserved |
| RAD-003 | `TestRendererAdapterInvalidPayloadReturnsError` | P0 | Negative | Bad payload rejected | Error propagated |
| RAD-004 | `TestRendererAdapterPreservesUUID` | P0 | Positive | UUID must not change | UUID preserved |
| RAD-005 | `TestRendererAdapterPreservesTarget` | P0 | Positive | Target must not change | Target preserved |
| RAD-006 | `TestRendererAdapterPreservesCorrelationID` | P1 | Positive | rpc_id traceability | rpc_id preserved if supported |
| RAD-007 | `TestRendererAdapterPropagatesRendererError` | P0 | Negative | Underlying error visible | Adapter returns renderer error |
| RAD-008 | `TestRendererAdapterDoesNotMutatePayload` | P1 | Safety | Avoid side effects | Original payload unchanged |

## 15.3 Apply Adapter Test Cases

| ID | Test Name | Priority | Type | Purpose | Expected Result |
|---|---|---|---|---|---|
| AAD-001 | `TestApplyAdapterRenderedOutputMapsToApplyInput` | P0 | Positive | Validate input mapping | `vyosapply.Input` fields correct |
| AAD-002 | `TestApplyAdapterCallsPrepareWhenSupported` | P0 | Positive | Prepare flow | Prepare called once |
| AAD-003 | `TestApplyAdapterLogsPlanFieldsSafely` | P1 | Safety | Plan logging safe | Only safe plan summary logged |
| AAD-004 | `TestApplyAdapterCallsApplyExactlyOnce` | P0 | Positive | Avoid duplicate apply | Apply call count is 1 |
| AAD-005 | `TestApplyAdapterPropagatesPrepareError` | P0 | Negative | Prepare failure visible | Apply not called; error returned |
| AAD-006 | `TestApplyAdapterPropagatesApplyError` | P0 | Negative | Apply failure visible | Error returned |
| AAD-007 | `TestApplyAdapterPrepareDoesNotMutateInputForApply` | P0 | Safety | Prevent stale/mutated input bug | Apply receives correct intended input |
| AAD-008 | `TestApplyAdapterBackendWithBothPrepareAndApplyUsesCorrectInput` | P0 | Safety | Critical reviewer case | Prepare and Apply called; Apply input correct |
| AAD-009 | `TestApplyAdapterDoesNotApplyWhenPrepareFails` | P0 | Safety | Prevent unsafe apply | Apply not called |
| AAD-010 | `TestApplyAdapterHandlesNilOrEmptyPlanSafely` | P1 | Negative | Robustness | No panic; behavior per spec |
| AAD-011 | `TestApplyAdapterDoesNotLogRenderedCommandsByDefault` | P0 | Safety | Avoid sensitive leak | Commands not logged at info level |

## 15.4 Acceptance Criteria

This section is complete when:

- renderer input mapping is verified,
- apply input mapping is verified,
- UUID, target, and correlation metadata are preserved,
- Prepare and Apply behavior is verified,
- errors are propagated,
- input mutation bugs are prevented.

---

# 16. Logging and Security Tests

## 16.1 Goal

Ensure logs are useful but do not leak sensitive configuration, rendered commands, apply plans, or large payloads unexpectedly.

## 16.2 Test Cases

| ID | Test Name | Priority | Type | Purpose | Expected Result |
|---|---|---|---|---|---|
| LOG-001 | `TestLoggingInfoLevelDoesNotLogPayload` | P0 | Safety | Prevent payload leak | Raw payload not present in info logs |
| LOG-002 | `TestLoggingInfoLevelDoesNotLogRenderedCommands` | P0 | Safety | Prevent command leak | Rendered commands not logged |
| LOG-003 | `TestLoggingInfoLevelDoesNotLogApplyPlan` | P0 | Safety | Prevent plan leak | Apply plan not logged |
| LOG-004 | `TestLoggingDebugWithPayloadFlagDoesNotLogRawPayload` | P1 | Safety | Explicit debug behavior | Payload metadata may be logged, but raw payload body is absent |
| LOG-005 | `TestLoggingDebugWithoutPayloadFlagDoesNotLogPayload` | P0 | Safety | Debug alone is not enough | Payload not logged |
| LOG-006 | `TestLoggingPayloadFlagWithoutDebugDoesNotLogPayload` | P0 | Safety | Flag alone is not enough | Payload not logged |
| LOG-007 | `TestLoggingPartialDebugConfigDoesNotEmitDebugLogs` | P0 | Safety | Reviewer requirement | Debug logs not emitted unless level is debug |
| LOG-008 | `TestLoggingRedactsKnownSecretFields` | P1 | Safety | Prevent secret exposure | password/token/key values absent or redacted |
| LOG-009 | `TestLoggingLargePayloadDoesNotCrash` | P1 | Load | Large payload logging safety | No crash |
| LOG-010 | `TestLoggingLargePayloadDoesNotConvertUnnecessarilyToString` | P1 | Safety | Avoid memory blowup | No unsafe allocation pattern around `string(payload)` |
| LOG-011 | `TestFailureLogsContainSafeErrorContext` | P1 | Safety | Useful safe errors | Logs include reason, not sensitive config |

## 16.3 Acceptance Criteria

This section is complete when:

- no sensitive data is logged by default,
- debug payload logging requires both debug level and explicit flag and emits metadata only,
- rendered commands and apply plans are not logged by default,
- large payloads do not crash or cause unsafe memory behavior.

---

# 17. Mocked Integration Tests

## 17.1 Goal

Validate the full agent flow without requiring a real VyOS device.

These tests should run in CI and prove that:

- NATS/agent-core integration still works,
- configure events trigger the service,
- desired config is read,
- adapters are invoked,
- state is updated,
- result/status is published.

## 17.2 Test Cases

| ID | Test Name | Priority | Type | Purpose | Expected Result |
|---|---|---|---|---|---|
| INT-001 | `TestIntegrationConfigureFlowWithMockBackend` | P0 | Integration | Full configure flow without VyOS | Render/apply/state/result all succeed |
| INT-002 | `TestIntegrationRealModeUsesRendererAndApplyAdapters` | P0 | Integration | Ensure real-mode path is not bypassed | Adapter call counters confirm invocation |
| INT-003 | `TestIntegrationPlaceholderAndRealMockFlowAreEquivalent` | P0 | Integration | Behavior consistency | Same result/status/state behavior |
| INT-004 | `TestIntegrationConfigureFailurePublishesFailureStatus` | P0 | Integration | Failure through full path | Failure status visible through NATS |
| INT-005 | `TestIntegrationActionTraceFlowWithMockExecutor` | P0 | Integration | Full action flow | received/executing/completed statuses |
| INT-006 | `TestIntegrationStatusResultSubjectsReceiveExpectedMessages` | P0 | Integration | Contract validation | Messages match expected schema |
| INT-007 | `TestIntegrationConfigureReadsDesiredConfigFromKV` | P0 | Integration | KV integration | Agent reads desired config from KV |
| INT-008 | `TestIntegrationMissingDesiredConfigPublishesFailure` | P0 | Negative | Missing KV config safe | Failure status; no apply |
| INT-009 | `TestIntegrationAgentCoreConnectionFailureHandled` | P1 | Negative | Connection error safety | Clear startup failure |

## 17.3 Acceptance Criteria

This section is complete when:

- real NATS/JetStream path is tested locally,
- configure and action flows work without VyOS,
- real-mode adapters are invoked,
- failure statuses are observable through NATS.

---

# 18. NATS Smoke Tests

## 18.1 Goal

Keep existing smoke scripts and define what they must prove.

## 18.2 Existing Scripts

| Script | Purpose | Required Status |
|---|---|---|
| `tests/scripts/validate-config.sh` | Validate config loading/conversion | P0 |
| `phase3-real-nats-configure-smoke.sh` | Configure E2E with real NATS | P0 |
| `phase4-real-nats-action-smoke.sh` | Action E2E with real NATS | P0 |
| `tests/scripts/real-vyos-configure-lab-smoke.sh` | Real VyOS lab smoke | P1/manual or release gate |

## 18.3 Required Smoke Validations

| ID | Test Name | Priority | Type | Purpose | Expected Result |
|---|---|---|---|---|---|
| SMK-001 | `SmokeValidateConfigScriptPasses` | P0 | Smoke | Config script works | Script exits 0 |
| SMK-002 | `SmokeConfigureRealNATSSuccess` | P0 | Smoke | Configure success path | Status success; state updated |
| SMK-003 | `SmokeConfigureSameUUIDAlreadyInSync` | P0 | Smoke | Basic idempotency | Second submit returns already_in_sync |
| SMK-004 | `SmokeActionTraceRealNATSSuccess` | P0 | Smoke | Trace action success | received/executing/completed |
| SMK-005 | `SmokeGracefulShutdown` | P0 | Smoke | Process lifecycle | Agent exits cleanly |
| SMK-006 | `SmokeRealVyOSConfigureLabRun` | P1 | Smoke | Real device proof | Config applied; state updated; no unexpected retries |

## 18.4 Acceptance Criteria

This section is complete when:

- NATS configure/action smoke scripts pass in CI,
- real VyOS smoke run is documented before production,
- output logs are attached to the PR or release when real-device validation is required.

---

# 19. Restart and Persistence Tests

## 19.1 Goal

Ensure the agent behaves correctly across restart and does not reapply already-applied config if state persists.

## 19.2 Test Cases

| ID | Test Name | Priority | Type | Purpose | Expected Result |
|---|---|---|---|---|---|
| RST-001 | `TestRestartWithPersistedStateDoesNotReapplySameUUID` | P0 | Recovery | Prevent duplicate apply after restart | No render/apply; already_in_sync |
| RST-002 | `TestRestartWithMissingStateReappliesConfig` | P1 | Recovery | Missing state behavior | Reapply or safe reconcile per spec |
| RST-003 | `TestRestartWithCorruptStateFailsSafely` | P0 | Negative | Corrupt state safe handling | Safe error/recovery; no blind trust |
| RST-004 | `TestRestartReadsStateBeforeConfigureDecision` | P0 | Safety | Correct ordering | State loaded before skip/apply decision |
| RST-005 | `TestRestartDoesNotLoseConfiguredStateWhenPathPersistent` | P1 | Recovery | Production path safety | State survives process restart |
| RST-006 | `TestTmpStatePathIsNotUsedForProductionByDefault` | P1 | Safety | Avoid reboot loss | Production config does not default to `/tmp` unless explicit |

## 19.3 Acceptance Criteria

This section is complete when:

- persisted state prevents duplicate apply,
- corrupt state is handled safely,
- state path behavior is tested,
- restart does not cause unexpected reconfiguration.

---

# 20. Retry and Idempotency Tests

## 20.1 Goal

Ensure duplicate configure events and retries after failure do not corrupt the system.

## 20.2 Test Cases

| ID | Test Name | Priority | Type | Purpose | Expected Result |
|---|---|---|---|---|---|
| IDEMP-001 | `TestSameUUIDAfterSuccessSkipsApply` | P0 | Positive | Idempotency after success | Second run skips apply |
| IDEMP-002 | `TestSameUUIDAfterRendererFailureRetries` | P0 | Recovery | Renderer failure should not checkpoint | Retry renders again |
| IDEMP-003 | `TestSameUUIDAfterApplyFailureRetries` | P0 | Recovery | Apply failure should not checkpoint | Retry applies again |
| IDEMP-004 | `TestSameUUIDAfterStateSaveFailureRetries` | P0 | Recovery | State save failure should retry | Retry reaches apply/save again safely |
| IDEMP-005 | `TestDuplicateConfigureEventsDoNotDoubleApplyAfterSuccess` | P0 | Safety | Duplicate event safety | At most one successful checkpoint |
| IDEMP-006 | `TestNewUUIDAfterOldSuccessAppliesAgain` | P0 | Positive | New config should apply | New UUID applies and saves |
| IDEMP-007 | `TestOlderUUIDAfterNewerAppliedDoesNotRollbackUnexpectedly` | P1 | Safety | Prevent unsafe rollback if applicable | Behavior follows explicit spec |
| IDEMP-008 | `TestRetryDoesNotPublishDuplicateSuccessForSameAttempt` | P1 | Safety | Clean result stream | No unexpected duplicate success |

## 20.3 Acceptance Criteria

This section is complete when:

- same UUID after success skips,
- same UUID after failure retries,
- state save failure does not create false idempotency,
- duplicate events do not cause duplicate apply after success.

---

# 21. Concurrency and Race Tests

## 21.1 Goal

Detect race conditions and unsafe state access before production.

## 21.2 Test Cases

| ID | Test Name | Priority | Type | Purpose | Expected Result |
|---|---|---|---|---|---|
| CONC-001 | `TestConcurrentConfigureEventsSameUUIDApplyAtMostOnce` | P1 | Concurrency | Duplicate concurrent event safety | At most one apply/checkpoint |
| CONC-002 | `TestConcurrentConfigureEventsDifferentUUIDsPreserveOrdering` | P1 | Concurrency | Ordering correctness | Final state deterministic per spec |
| CONC-003 | `TestConfigureAndActionCanOverlapSafely` | P1 | Concurrency | Parallel workflow safety | No race/panic; both statuses correct |
| CONC-004 | `TestConcurrentStateAccessNoRace` | P1 | Concurrency | State file safety | `go test -race` reports no race |
| CONC-005 | `TestBurstConfigureEventsNoPanicNoRace` | P1 | Load | Burst robustness | No panic; final state valid |
| CONC-006 | `TestBurstActionEventsNoPanicNoRace` | P2 | Load | Action burst robustness | No panic; statuses valid |

## 21.3 Acceptance Criteria

This section is complete when:

- race detector passes,
- concurrent configure does not corrupt state,
- configure/action overlap is safe,
- burst events do not panic.

---

# 22. Large Payload and Lightweight Load Tests

## 22.1 Goal

Perform lightweight sanity testing, not full benchmarking.

## 22.2 Test Cases

| ID | Test Name | Priority | Type | Purpose | Expected Result |
|---|---|---|---|---|---|
| PERF-001 | `TestLargeConfigOneThousandCommandsDoesNotCrash` | P1 | Load | Large config sanity | No crash; result correct |
| PERF-002 | `TestLargePayloadDoesNotCauseUnsafeLoggingAllocation` | P1 | Safety | Avoid `string(payload)` risks | No excessive allocation/crash |
| PERF-003 | `TestRapidConfigureEventsCompleteWithoutRace` | P1 | Load | Burst behavior | No race; final state valid |
| PERF-004 | `TestLargeRenderedOutputHandledSafely` | P1 | Load | Large renderer output | Apply receives correct output; no crash |
| PERF-005 | `TestLargeActionPayloadRejectedOrHandledSafely` | P2 | Negative | Action payload safety | Rejected or handled per limits |

## 22.3 Acceptance Criteria

This section is complete when:

- large configs do not crash,
- logger does not create unsafe memory behavior,
- rapid events do not corrupt state.

---

# 23. Real VyOS Lab Smoke Tests

## 23.1 Goal

Validate that the real renderer/apply integration works against a real VyOS device or lab environment.

These tests are not a replacement for unit or mocked tests. They are final confidence checks.

## 23.2 Test Cases

| ID | Test Name | Priority | Type | Purpose | Expected Result |
|---|---|---|---|---|---|
| LAB-001 | `LabSmokeRealVyOSConfigureAppliesConfig` | P1 | Smoke | Real config apply | Config visible/applied on VyOS |
| LAB-002 | `LabSmokeRealVyOSStateUpdatedAfterApply` | P1 | Smoke | State checkpoint | State contains applied UUID |
| LAB-003 | `LabSmokeRealVyOSNoUnexpectedRetries` | P1 | Smoke | Retry sanity | No duplicate/looping apply |
| LAB-004 | `LabSmokeRealVyOSActionTraceRuns` | P2 | Smoke | Real trace action | Trace action executes successfully |
| LAB-005 | `LabSmokeLogsAttachedToPR` | P1 | Process | Review evidence | PR includes logs/output proof |

## 23.3 Acceptance Criteria

This section is complete when:

- at least one successful real configure run is recorded,
- logs are attached to PR/release notes,
- state update is proven,
- no unexpected retries are observed.

---

# 24. CI/CD Mapping

## 24.1 Must Run on Every PR

| Test Group | Command or Method |
|---|---|
| Go unit tests | `go test ./...` |
| Config tests | Go tests plus `tests/scripts/validate-config.sh` |
| Configure workflow mocked tests | Go unit/integration tests |
| Configure failure injection tests | Go unit tests |
| Action workflow tests | Go unit/integration tests |
| State tests | Go unit tests |
| NATS configure/action smoke | Existing smoke scripts |
| Race-sensitive tests | `go test -race ./...` if runtime allows |

## 24.2 Should Run in CI or Nightly

| Test Group | Reason |
|---|---|
| Large payload tests | Slightly heavier |
| Burst/concurrency tests | May take longer |
| Reconnect/restart tests | More integration-heavy |
| Real NATS integration tests | Requires service setup |

## 24.3 Manual or Release Gate

| Test Group | Reason |
|---|---|
| Real VyOS lab smoke | Requires real lab/device |
| Long-running stability tests | Not needed on every PR |

---

# 25. Recommended Implementation Order

## Phase 1: Test Infrastructure

Implement:

- FakeRenderer,
- FakeApplyBackend,
- FakeStateStore,
- StatusRecorder,
- LogCapture,
- common config and payload fixtures.

Gate:

- fakes can simulate success/failure,
- call counts and inputs are inspectable.

## Phase 2: Configuration and State Unit Tests

Implement:

- config negative tests,
- default overlay tests,
- state read/write/corruption tests.

Gate:

- invalid config and corrupt state are safely handled.

## Phase 3: Configure Workflow Tests

Implement:

- happy path,
- already-in-sync,
- idempotency,
- ordering,
- invalid input.

Gate:

- configure behavior is proven without real VyOS.

## Phase 4: Configure Failure Injection Tests

Implement:

- renderer failure,
- apply failure,
- state save failure,
- retry after failure.

Gate:

- failed workflows do not checkpoint UUID or publish success.

## Phase 5: Action Workflow Tests

Implement:

- trace happy path,
- unsupported action,
- disabled action,
- execution failure,
- status ordering.

Gate:

- action lifecycle is deterministic.

## Phase 6: Adapter Tests

Implement:

- renderer adapter mapping,
- apply adapter mapping,
- prepare/apply flow,
- mutation safety,
- error propagation.

Gate:

- adapter boundary is proven safe.

## Phase 7: Mocked Integration Tests

Implement:

- real NATS plus fake backend configure flow,
- action flow,
- KV read,
- status/result validation.

Gate:

- local integration works without real VyOS.

## Phase 8: Logging, Restart, Concurrency, and Load

Implement:

- log safety,
- restart persistence,
- race tests,
- large payload tests.

Gate:

- production-readiness gaps are closed.

## Phase 9: Real VyOS Lab Evidence

Run:

- real VyOS configure smoke,
- real action smoke if applicable,
- attach logs to PR.

Gate:

- real backend validated.

---

# 26. Definition of Done

This phase is done when all of the following are true:

## 26.1 Required

- [ ] All P0 test cases are implemented.
- [ ] All P0 tests pass locally.
- [ ] All P0 tests pass in CI.
- [ ] Configure happy path and failure paths are tested.
- [ ] State save failure is tested.
- [ ] Retry/idempotency after failure is tested.
- [ ] State corruption behavior is tested.
- [ ] Action happy path and failure path are tested.
- [ ] Adapter mapping tests are implemented.
- [ ] Logging safety tests are implemented.
- [ ] Mocked real-mode integration test exists.
- [ ] Existing smoke scripts still pass.

## 26.2 Strongly Recommended Before Production

- [ ] P1 tests are implemented or explicitly tracked.
- [ ] Restart/persistence tests pass.
- [ ] Race tests pass.
- [ ] Large payload tests pass.
- [ ] Real VyOS lab smoke output is attached.

---

# 27. Reviewer Approval Checklist

A reviewer can approve the test-hardening PR when these questions are answered with yes:

| Question | Yes/No |
|---|---|
| Does invalid config fail early? |  |
| Does default mode explicitly resolve to placeholder? |  |
| Does real mode wire real renderer/apply adapters? |  |
| Does placeholder mode wire placeholder implementations? |  |
| Does configure happy path call render/apply exactly once? |  |
| Is state saved only after apply succeeds? |  |
| Does already-in-sync skip render/apply? |  |
| Does same UUID after success avoid duplicate apply? |  |
| Does renderer failure skip apply and state save? |  |
| Does apply failure skip state save? |  |
| Does state save failure mark configure failed? |  |
| Does retry after failure work safely? |  |
| Are renderer/apply adapter mappings verified? |  |
| Is Prepare plus Apply behavior tested? |  |
| Are sensitive payloads hidden from normal logs? |  |
| Is debug payload logging gated by both debug level and explicit flag? |  |
| Does mocked real-mode integration prove adapters are invoked? |  |
| Does restart with persisted state avoid reapply? |  |
| Does corrupt state fail safely or recover cleanly? |  |
| Do NATS smoke tests still pass? |  |

---

# 28. Final Test Philosophy

The final test suite should not only prove:

```text
The agent can apply a config.
```

It must also prove:

```text
The agent does not apply twice accidentally.
The agent does not checkpoint failed configs.
The agent does not leak sensitive data.
The agent does not lose correctness after restart.
The agent does not depend on real VyOS for CI validation.
The agent fails safely when dependencies fail.
```

That is the standard for considering this agent production-ready from a testing perspective.
