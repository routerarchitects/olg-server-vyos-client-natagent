# Phase 7 Mocked Integration Coverage

This table maps `TDD_SPEC.md` Phase 7 mocked integration requirements to the current tagged Go integration tests.

## Integration Tests

| ID | Status | Test file or script | Test name or CI step | Notes |
|---|---|---|---|---|
| INT-001 | Covered | `tests/integration/mocked_agent_flow_integration_test.go` | `TestIntegrationConfigureFlowWithMockBackend` | Uses real local NATS, JetStream KV, agentcore configure handler registration, fake renderer/apply engine, fake state, and observes success status/result through NATS. |
| INT-002 | Covered | `tests/integration/mocked_agent_flow_integration_test.go` | `TestIntegrationRealModeUsesRendererAndApplyAdapters` | Uses real `renderervyos.Adapter` and `applyvyos.Adapter` with fake adapter backends to prove adapter invocation without real VyOS. |
| INT-003 | Covered | `tests/integration/mocked_agent_flow_integration_test.go` | `TestIntegrationPlaceholderAndRealMockFlowAreEquivalent` | Compares visible status/result/state behavior between placeholder engines and real adapters backed by fakes. |
| INT-004 | Covered | `tests/integration/mocked_agent_flow_integration_test.go` | `TestIntegrationConfigureFailurePublishesFailureStatus` | Renderer failure publishes failure status/result through NATS, does not apply, and does not checkpoint requested UUID. |
| INT-005 | Covered | `tests/integration/mocked_agent_flow_integration_test.go` | `TestIntegrationActionTraceFlowWithMockExecutor` | Uses real NATS action submission and agentcore action handler registration with a fake trace executor. |
| INT-006 | Covered | `tests/integration/mocked_agent_flow_integration_test.go` | `TestIntegrationStatusResultSubjectsReceiveExpectedMessages` | Direct NATS subscribers verify expected status/result subjects and envelope fields. |
| INT-007 | Covered | `tests/integration/mocked_agent_flow_integration_test.go` | `TestIntegrationConfigureReadsDesiredConfigFromKV` | `SubmitConfigure` stores desired config in JetStream KV; renderer input proves the handler loaded payload from KV. |
| INT-008 | Covered | `tests/integration/mocked_agent_flow_integration_test.go` | `TestIntegrationMissingDesiredConfigPublishesFailure` | Publishes configure notification without KV record; failure is observed through NATS and apply is not called. Current service reports this as `load_desired_failed` because agentcore returns a config-not-found load error. |
| INT-009 | Covered | `tests/integration/mocked_agent_flow_integration_test.go` | `TestIntegrationAgentCoreConnectionFailureHandled` | Starts an agentcore client against an unused local port with short timeout and verifies startup returns an error without hanging. |

## CI/CD Mapping

| Item | Value |
|---|---|
| CI job | `Smoke Tests` |
| CI step | `Mocked integration tests` |
| Command | `go test -tags=integration ./...` |
| Timeout | `5` minutes |
| NATS setup | The existing `Install nats-server` step runs `go install github.com/nats-io/nats-server/v2@v2.10.22` and adds `$(go env GOPATH)/bin` to `PATH`. |
| Local run command | `go test -tags=integration ./...` |
| Local prerequisite | `nats-server` must be available in `PATH`; install with `go install github.com/nats-io/nats-server/v2@v2.10.22` if needed. |

## Scope Notes

- The tests start their own local `nats-server` processes with JetStream enabled and per-test temporary storage.
- No real VyOS device, real platform apply, real trace, or rtty dependency is introduced.
- Existing smoke scripts remain in CI. They prove CLI/script paths; Phase 7 Go tests add focused assertions with fake backends.
