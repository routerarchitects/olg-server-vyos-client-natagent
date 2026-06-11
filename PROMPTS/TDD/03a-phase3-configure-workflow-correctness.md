Read these files carefully before making any changes:

1. TDD_SPEC.md
2. PROMPTS/TDD/03-phase3-configure-workflow-correctness.md
3. internal/configure/configure_workflow_test.go
4. internal/configure/service.go
5. internal/testutil/README.md
6. internal/testutil/*

We already implemented the main Phase 3 configure workflow tests, but the review found that a few configure workflow cases from the full TDD_SPEC.md are still missing or weak.

Your task is to complete Phase 3 so that nothing listed under the TDD_SPEC.md configure workflow section remains uncovered.

Implement ONLY remaining Phase 3 configure workflow correctness tests and minimal fixes needed to make those tests pass.

Do NOT implement Phase 4 renderer/apply/state-save failure injection tests.
Do NOT implement action workflow tests.
Do NOT implement adapter mapping tests beyond what is needed for Phase 3.
Do NOT add real VyOS dependency.
Do NOT add real NATS dependency.
Do NOT add logging/security tests.
Do NOT add restart/concurrency/load tests.

Focus only on the remaining Phase 3 configure workflow requirements.

Current known covered tests include:
- configure success applies and saves state
- state save happens after apply
- success result is published after state save
- already-in-sync skips render/apply/save
- repeated same UUID is idempotent
- invalid desired config fails before side effects
- empty UUID fails before side effects
- success preserves correlation identifiers

Now add or improve tests for the remaining TDD_SPEC.md Phase 3 configure workflow items:

1. Missing desired config fails safely

Add a test like:

TestConfigureWorkflowMissingDesiredConfigFailsBeforeSideEffects

Expected:
- FakeConfigureClient returns nil desired config
- service.Handle returns error
- renderer is not called
- apply is not called
- state is not loaded or saved, if current service validates missing desired before state load
- failure result is published
- failure result has error_code = desired_config_missing
- failure result preserves target, uuid, rpc_id
- no success result is published

2. Wrong target / desired target mismatch fails safely

Add a test like:

TestConfigureWorkflowWrongTargetFailsBeforeSideEffects

Expected:
- configure notification target is one value
- loaded desired config target is a different value
- service.Handle returns error
- renderer is not called
- apply is not called
- state is not loaded or saved, if current service validates mismatch before state load
- failure result is published
- failure result has error_code = desired_target_mismatch
- failure result preserves notification target, uuid, rpc_id
- no success result is published

3. Desired UUID mismatch fails safely

Add a test like:

TestConfigureWorkflowDesiredUUIDMismatchFailsBeforeSideEffects

Expected:
- configure notification UUID is one value
- loaded desired config UUID is different
- service.Handle returns error
- renderer is not called
- apply is not called
- state is not loaded or saved, if current service validates mismatch before state load
- failure result is published
- failure result has error_code = desired_uuid_mismatch
- failure result preserves notification target, notification uuid, rpc_id
- no success result is published

4. Empty target fails safely

Add a test like:

TestConfigureWorkflowEmptyTargetFailsBeforeSideEffects

Expected:
- desired config target or notification target is empty, depending on current service validation path
- service.Handle returns error
- renderer is not called
- apply is not called
- state is not saved
- failure result is published
- failure result has error_code = desired_target_invalid or the current intended error code
- no success result is published

5. New UUID after existing applied UUID triggers render/apply/save

Add a test like:

TestConfigureWorkflowNewUUIDTriggersRenderApplyAndStateUpdate

Expected:
- local state has AppliedUUID = cfg-old
- notification and desired config have UUID = cfg-new
- renderer is called once
- apply is called once
- state is saved once
- saved state AppliedUUID = cfg-new
- success result is published
- no failure result is published

This explicitly covers TDD_SPEC.md CWF-008.

6. Explicit call-count tests for new UUID

Current happy-path test already checks this, but TDD_SPEC.md separately lists:
- renderer called exactly once for new UUID
- apply called exactly once for new UUID

Either:
- keep the existing assertions and document in test comments that this covers CWF-004 and CWF-005,
or
- add small focused tests if that improves traceability.

Do not duplicate unnecessarily, but make coverage mapping clear.

7. Real-mode with fake backend / mocked real-mode path

TDD_SPEC.md lists:

TestConfigureHappyPathRealModeWithFakeBackend

If current architecture supports testing real-mode path without real VyOS, add a test that proves real-mode wiring can use fake/test backend safely.

If current architecture does not support this without a larger refactor, do NOT overbuild. Instead:
- add a short documented note in a Phase 3 coverage section explaining why CWF-002 is deferred,
- state whether it belongs to Phase 6 adapter mapping or mocked real-mode integration,
- ensure this is documented in the phase coverage table.

8. Success status/result ordering issue

Review found a semantic weakness:

- current service publishes status success/applied before state save
- current test only proves success result is after state save
- StatusRecorder records status as publish_status, while ResultRecorder records success result as publish_success

Decide and fix this clearly.

Preferred behavior:
- after apply succeeds, publish a running/applied or running/state_saving status if needed
- save state
- then publish final success/applied status
- then publish success result

Add or update tests to prove:
- final success result is after state_save
- final success status is not published before state_save, if the service has a final success status concept

If changing status semantics risks breaking existing smoke tests, make the smallest safe change and update tests accordingly.

Do not publish success before durable state checkpoint unless the code comment and tests clearly distinguish "apply completed" from "configure completed".

9. Coverage documentation

Add or update a simple Phase 3 coverage document or table.

Preferred location:
- TDD_SPEC.md, if the project is already tracking coverage there
or
- PROMPTS/TDD/03-phase3-configure-workflow-correctness.md
or
- a new small file such as PROMPTS/TDD/PHASE3_COVERAGE.md

Document every configure workflow test from TDD_SPEC.md and map it to the actual test file/test name.

The table should include at least:

- CWF-001 Configure happy path
- CWF-002 Real-mode with fake backend
- CWF-003 State saved after apply
- CWF-004 Renderer called exactly once
- CWF-005 Apply called exactly once
- CWF-006 Already-in-sync skips render/apply
- CWF-007 Same config processed twice second run skipped
- CWF-008 New UUID triggers render/apply
- CWF-009 Missing desired config fails
- CWF-010 Wrong target fails or ignored safely
- CWF-011 Empty UUID fails
- CWF-012 Success result fields
- CWF-013 Success does not publish failure
- CWF-014 Does not update state before apply
- CWF-015 Maintains target and UUID across workflow

For each row, include:
- Status: Covered / Deferred
- Test file
- Test name
- Notes

If anything is deferred, explain why and which future phase owns it.

10. Test helper improvements

Use existing internal/testutil helpers.

Only add small helper methods if they improve readability, for example:
- ContainsFailureResult()
- ContainsSuccessResult()
- AssertNoSuccessResult()
- CountResultByType()
- ContainsStatusStage()

Do not duplicate fakes inside configure_workflow_test.go if they belong in testutil.

11. Commands to run

After changes, run:

go test ./...

Also run:

go test -race ./...

If CI or local repo supports build:

go build ./...

12. Final response from Codex

After implementation, summarize:

- files added
- files modified
- tests added
- TDD_SPEC.md CWF IDs now covered
- any CWF IDs deferred and why
- whether service.go behavior changed
- whether production runtime behavior changed
- commands run and results
- what remains for Phase 4

Acceptance criteria:

Phase 3 is complete only when:

[ ] CWF-001 is covered
[ ] CWF-002 is either covered or explicitly documented as deferred with reason
[ ] CWF-003 is covered
[ ] CWF-004 is covered
[ ] CWF-005 is covered
[ ] CWF-006 is covered
[ ] CWF-007 is covered
[ ] CWF-008 is covered
[ ] CWF-009 is covered
[ ] CWF-010 is covered
[ ] CWF-011 is covered
[ ] CWF-012 is covered
[ ] CWF-013 is covered
[ ] CWF-014 is covered
[ ] CWF-015 is covered
[ ] success/final-result ordering after state save is tested
[ ] no real VyOS dependency is introduced
[ ] no real NATS dependency is introduced
[ ] go test ./... passes
[ ] go test -race ./... passes
[ ] phase-wise coverage is documented clearly
