# Phase 3 Configure Workflow Coverage

This table maps the configure workflow cases from `TDD_SPEC.md` section 11 to the current unit tests.

| ID | Status | Test file | Test name | Notes |
|---|---|---|---|---|
| CWF-001 | Covered | `internal/configure/configure_workflow_test.go` | `TestConfigureWorkflowSuccessAppliesAndSavesState` | Proves successful service-level placeholder-style flow with injected fakes. |
| CWF-002 | Deferred | n/a | n/a | Mocked real-mode adapter flow requires real-mode adapter/backend wiring and belongs to the real-mode adapter or mocked integration phase, not service-only Phase 3. No real VyOS dependency is introduced here. |
| CWF-003 | Covered | `internal/configure/configure_workflow_test.go` | `TestConfigureWorkflowSavesStateAfterApply` | Uses `EventRecorder` to prove apply precedes state save. |
| CWF-004 | Covered | `internal/configure/configure_workflow_test.go` | `TestConfigureWorkflowSuccessAppliesAndSavesState` | Asserts renderer call count is exactly one for a new UUID. |
| CWF-005 | Covered | `internal/configure/configure_workflow_test.go` | `TestConfigureWorkflowSuccessAppliesAndSavesState` | Asserts apply call count is exactly one for a new UUID. |
| CWF-006 | Covered | `internal/configure/configure_workflow_test.go` | `TestConfigureWorkflowAlreadyInSyncSkipsApply` | Proves already-applied UUID skips render, apply, and state save. |
| CWF-007 | Covered | `internal/configure/configure_workflow_test.go` | `TestConfigureWorkflowRepeatedSameUUIDIsIdempotent` | First call applies and saves; second same UUID skips additional render/apply/save. |
| CWF-008 | Covered | `internal/configure/configure_workflow_test.go` | `TestConfigureWorkflowNewUUIDTriggersRenderApplyAndStateUpdate` | Existing state has old UUID; new UUID renders, applies, and checkpoints new UUID. |
| CWF-009 | Covered | `internal/configure/configure_workflow_test.go` | `TestConfigureWorkflowMissingDesiredConfigFailsBeforeSideEffects` | Nil desired config fails before state load, render, apply, or save. |
| CWF-010 | Covered | `internal/configure/configure_workflow_test.go` | `TestConfigureWorkflowWrongTargetFailsBeforeSideEffects` | Desired target mismatch fails safely before side effects. |
| CWF-011 | Covered | `internal/configure/configure_workflow_test.go` | `TestConfigureWorkflowEmptyUUIDFailsBeforeSideEffects` | Empty notification UUID fails before desired lookup, state load, render, apply, or save. |
| CWF-012 | Covered | `internal/configure/configure_workflow_test.go` | `TestConfigureWorkflowSuccessPreservesCorrelationIdentifiers` | Success result preserves RPC ID, target, and UUID. |
| CWF-013 | Covered | `internal/configure/configure_workflow_test.go` | `TestConfigureWorkflowSuccessAppliesAndSavesState` | Asserts success path publishes no failure result. |
| CWF-014 | Covered | `internal/configure/configure_workflow_test.go` | `TestConfigureWorkflowSavesStateAfterApply` | State save is ordered after apply, preventing pre-apply checkpointing. |
| CWF-015 | Covered | `internal/configure/configure_workflow_test.go` | `TestConfigureWorkflowSuccessPreservesCorrelationIdentifiers` | Target and UUID are preserved through renderer input, apply input, and result. |

Additional Phase 3 coverage:

| Behavior | Test file | Test name | Notes |
|---|---|---|---|
| Success result after checkpoint | `internal/configure/configure_workflow_test.go` | `TestConfigureWorkflowPublishesSuccessAfterStateSave` | Proves state save precedes success result publication. |
| Final success status after checkpoint | `internal/configure/configure_workflow_test.go` | `TestConfigureWorkflowPublishesSuccessAfterStateSave` | Proves state save precedes final success status publication. |
| Desired UUID mismatch | `internal/configure/configure_workflow_test.go` | `TestConfigureWorkflowDesiredUUIDMismatchFailsBeforeSideEffects` | Preserves notification correlation data on failure. |
| Empty target | `internal/configure/configure_workflow_test.go` | `TestConfigureWorkflowEmptyTargetFailsBeforeSideEffects` | Validates notification-target guard before desired lookup. |
| Post-checkpoint reporting failure | `internal/configure/service_test.go` | `TestHandleResultPublishFailureBehavior`, `TestHandleSuccessStatusPublishFailureAfterSave` | Apply + state save remain authoritative; no contradictory configure failure is published afterward. |
| Invalid payload JSON | `internal/configure/configure_workflow_test.go` | `TestConfigureWorkflowInvalidDesiredConfigFailsAtRendererBoundary` | Invalid desired payload is rejected by the renderer after state load and before apply/save side effects. |
