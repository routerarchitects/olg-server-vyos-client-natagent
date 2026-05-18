# TESTS_STANDARD_TEMPLATE.md

Read this file first before generating or modifying test cases.

This file is the stable source of truth for:
- test structure
- wording
- formatting
- naming style
- comment style
- positive/negative coverage style
- helper style
- package placement
- scope boundaries for unit tests

Use this file as the fixed base standard for writing tests.

Feature-specific, phase-specific, or PR-specific instructions must be provided in a separate prompt or task file that references this standard.

Do not modify this template for each feature.

---

## Also read before writing tests

Before generating or modifying tests, read:

- `README.md`
- `SPEC.md`, if present
- `REQUIREMENTS_CHECKLIST.md`, if present
- the current package under test
- existing test files in the repository, if present
- related interfaces, contracts, or types used by the code under test

Existing repository test files are the strongest style reference.

---

## Core objective

Generate unit tests that are structurally consistent, readable, minimal, behavior-focused, and aligned to implemented code.

The goal is not to maximize line coverage. The goal is to protect important behavior contracts.

---

## Most important rule

Existing repository test files, if present, are the style reference.

Follow their structure, helper style, assertion style, naming style, and level of detail.

If no existing test style exists yet, use this template and the TC-style structure below.

Do not invent a new test style unless explicitly instructed.

---

## Scope rules

- Test only behavior that is actually implemented.
- Do not write speculative tests for unimplemented behavior.
- Do not add integration, runtime, end-to-end, or external-system tests unless explicitly requested.
- Do not fake coverage for functionality that does not exist yet.
- Keep tests aligned to real code, not ideal future design.
- Keep tests beside the package they test unless the repository already uses a different pattern.
- Prefer package-local tests when private helpers or package-local types make tests clearer.
- Use fake dependencies for service tests.
- Do not introduce mock frameworks unless the repository already uses them.
- Do not add third-party test dependencies unless explicitly approved.

---

## Unit-test boundaries

Unit tests must not require external systems.

Do not start or require:

- NATS server
- databases
- message brokers
- HTTP services
- shell commands
- operating-system daemons
- network probes
- cloud services
- hardware devices
- platform-specific services

Those belong in smoke tests, integration tests, or end-to-end tests.

---

## Required comment format above every test function

Every test function must have this exact comment block format:

```go
/*
TC-<AREA>-<NUMBER>
Type: Positive|Negative|Mixed
Title: <short heading>
Summary:
<2-4 line plain-English summary of the test>

Validates:
  - <point 1>
  - <point 2>
  - <point 3 if needed>
*/
func TestXxx(t *testing.T) {
    ...
}
```

Comment rules:

- Preserve this structure exactly.
- Use simple, clear English.
- Keep wording consistent across the suite.
- Do not switch to a different documentation style.
- Do not omit the comment block.
- Use `Mixed` only when a test intentionally validates both positive and negative behavior in one cohesive scenario.

---

## TC numbering rules

Use a stable area prefix and increasing number.

Recommended format:

```text
TC-<PACKAGE-OR-FEATURE>-001
TC-<PACKAGE-OR-FEATURE>-002
TC-<PACKAGE-OR-FEATURE>-003
```

Examples:

```text
TC-CONFIG-LOADER-001
TC-RUNTIME-LIFECYCLE-001
TC-STATE-STORE-001
TC-ACTION-SERVICE-001
```

Rules:

- Do not reuse numbers within the same area.
- Keep numbering stable after tests are added.
- If a test is removed later, do not renumber unrelated tests unless the suite is deliberately reorganized.
- A feature-specific prompt may define exact TC area prefixes for that task.

---

## Test naming rules

Use `TestXxx...` Go naming.

Keep test names explicit and behavior-oriented.

Good examples:

```go
func TestLoadAppliesDefaultsAndPreservesExplicitValues(t *testing.T)
func TestValidateRejectsUnsupportedAction(t *testing.T)
func TestHandlePublishesFailureWhenExecutorFails(t *testing.T)
func TestSaveAndLoadRoundTripState(t *testing.T)
```

Avoid vague names:

```go
func TestHandle(t *testing.T)
func TestError(t *testing.T)
func TestService(t *testing.T)
func TestBasic(t *testing.T)
```

---

## Coverage rules

Include both positive and negative tests where meaningful.

Prioritize:

- newly added public behavior
- newly added internal helpers
- validation behavior
- serialization and deserialization behavior
- lifecycle behavior
- state transitions
- error behavior
- retry or failure behavior
- behavior around context cancellation
- behavior around file or resource cleanup
- edge cases that directly protect implemented behavior

Avoid:

- duplicate tests that only restate existing coverage
- tests for unrelated cleanup
- tests for future behavior
- tests that assert implementation details not relevant to behavior
- large noisy test matrices when a small focused set is enough

Prefer a small, strong test set over a large noisy one.

---

## Positive test guidance

Positive tests should verify expected successful behavior.

Examples of positive behavior:

- valid input succeeds
- expected state is saved
- expected output is returned
- expected status/result is published
- default values are applied
- explicit values are preserved
- missing optional data is handled correctly
- idempotent or no-op behavior works as intended

---

## Negative test guidance

Negative tests should verify implemented failure behavior.

Examples of negative behavior:

- invalid input is rejected
- nil dependencies are rejected
- context cancellation is honored
- malformed JSON/YAML is rejected
- failed dependency call returns an error
- failure path does not update state
- failure path publishes expected failure output
- unsupported values are rejected
- required values are enforced

Do not assert exact full error strings unless that exact string is part of the contract.

---

## Assertion rules

- Assert behavior first.
- Prefer direct assertions unless a helper clearly reduces duplication.
- Use `t.Helper()` in helpers.
- Use `t.Fatalf` for required preconditions and failed assertions.
- Use `t.Fatal` for simple failure messages.
- Do not assert internal trivia unless that internal detail is the actual behavior being tested.
- If code returns typed errors, assert typed error fields only when that behavior exists.
- If code returns plain errors, do not force typed error assertions.
- When checking error text, prefer `strings.Contains` only for stable behavior callers rely on.
- Avoid brittle assertions on full wrapped error messages.
- Avoid asserting map iteration order.
- Avoid asserting timestamps unless a fixed clock is injected and the timestamp is part of the behavior.

---

## Helper style

Use small helpers only when they improve readability.

Helper rules:

- helpers must call `t.Helper()`
- helpers should have clear names
- helpers should not hide important behavior
- helpers should not make the test harder to understand
- avoid generic assertion frameworks unless already used by the repository

Good helper examples:

```go
func requireNoError(t *testing.T, err error) {
    t.Helper()
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
}
```

```go
func requireStatusStage(t *testing.T, statuses []Status, stage string) {
    t.Helper()
    for _, status := range statuses {
        if status.Stage == stage {
            return
        }
    }
    t.Fatalf("missing status stage %q", stage)
}
```

---

## Fake dependency style

For service tests, prefer small fake structs with fields for:

- configured return values
- configured errors
- call counts
- captured input values
- published outputs
- saved state or written data

Example shape:

```go
type fakeClient struct {
    statuses []StatusEnvelope
    results  []ResultEnvelope

    publishStatusErr error
    publishResultErr error
}
```

Use simple method implementations.

Do not introduce mock frameworks unless explicitly approved.

Do not add third-party dependencies unless explicitly approved.

---

## Table-driven test guidance

Use table-driven tests only where they improve clarity.

Good candidates:

- validation rules with similar setup
- invalid enum values
- missing required fields
- malformed input cases
- simple success/failure variants with shared assertions

Avoid table-driven tests when each case needs very different setup and assertions. In that case, write separate tests.

---

## Time handling rules

When code supports injecting a clock, inject a fixed clock in tests.

Example:

```go
fixedNow := time.Date(2026, 5, 18, 9, 0, 0, 0, time.UTC)
```

Rules:

- do not use `time.Now()` directly when exact time matters
- do not use sleeps in unit tests
- do not assert exact timestamps unless fixed time is injected
- use short contexts only when explicitly testing timeouts or cancellation

---

## Context rules

Use `context.Background()` for normal unit tests.

Use canceled contexts only when testing cancellation behavior.

Rules:

- do not store contexts in fake structs unless required for a specific assertion
- do not use long timeouts in unit tests
- prefer explicit canceled contexts over sleeping/timeouts
- assert cancellation behavior only when implemented

---

## File-system test rules

For file-system tests:

- use `t.TempDir()`
- never write to repository paths
- never write to fixed `/tmp` paths manually unless `t.TempDir()` is not sufficient
- use `filepath.Join`
- verify file contents only when content is the behavior under test
- verify permissions only when permissions are part of the implemented behavior
- rely on Go test cleanup through `t.TempDir()`

Example:

```go
path := filepath.Join(t.TempDir(), "state", "state.json")
```

---

## Serialization test rules

For JSON/YAML or codec behavior:

- test valid input is decoded correctly
- test malformed input returns an error
- test required fields are enforced when implemented
- test output fields that are part of the contract
- do not assert formatting whitespace unless formatting is the behavior under test

---

## Concurrency test rules

Only add concurrency tests when concurrency behavior is implemented and important.

Examples:

- method is documented as safe for concurrent use
- mutex-protected state must be verified
- duplicate lifecycle calls should be safe or rejected
- concurrent calls should not corrupt state

Rules:

- avoid flaky sleeps
- use channels, wait groups, or barriers
- keep concurrency tests small
- run with `go test -race` when concurrency behavior is involved

Do not test future concurrency behavior that has not been implemented.

---

## Modification rules

- Do not rewrite existing tests unless the actual behavior or public contract changed.
- If an existing test must be modified, explain exactly why.
- Keep stable behavior tests unchanged.
- Do not combine unrelated cleanup with test additions.
- Do not refactor production code just to make tests easier unless the refactor is small and improves clarity.
- Do not change smoke scripts unless a test or build failure proves it is required.

---

## Feature-specific prompts

This file is the base standard only.

A feature-specific or phase-specific prompt should provide:

- exact files to test
- exact behavior to cover
- exact TC area prefixes, if needed
- known reviewer comments to address
- expected files to add or modify
- explicit out-of-scope items
- validation commands to run

Feature-specific prompts must not duplicate or rewrite this entire template unless necessary.

---

## Expected output after generating tests

After adding or modifying tests, summarize:

```text
Files added:
- ...

Files modified:
- ...

Positive cases covered:
- ...

Negative cases covered:
- ...

Existing tests changed and why:
- ...

Remaining gaps intentionally deferred:
- ...
```

Do not claim tests passed unless they were actually run.

---

## Required validation commands

At minimum, run:

```bash
gofmt -w <changed-go-files>
go test ./...
go build ./...
```

For concurrency-sensitive changes, also run when practical:

```bash
go test -race ./...
```

If repository-specific smoke or integration scripts are requested, run them only when explicitly asked and when local tooling is available.

If a command cannot be run, state exactly why.

---

## Final verification checklist

Before finishing, verify:

- tests match repository suite style
- header comments match the required format exactly
- TC numbering is stable and unique within each area
- wording is consistent across the suite
- only implemented behavior is tested
- positive and negative coverage are both present where meaningful
- no future behavior is accidentally tested
- no external systems are required by unit tests
- tests remain minimal but sufficient
- no third-party dependencies were added without approval
- `gofmt` was run on changed Go files
- `go test ./...` passes, if run
