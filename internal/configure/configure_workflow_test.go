package configure

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/routerarchitects/nats-agent-core/agentcore"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/renderer"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/testutil"
)

/*
TC-CONFIGURE-WORKFLOW-001
Type: Positive
Title: Configure success applies and saves state
Summary:
Runs the configure service with a valid desired config for a new UUID.
The workflow should render once, apply once, save checkpoint state once,
and publish successful configure output without publishing failure.

Validates:
  - renderer is called exactly once
  - apply is called exactly once
  - state is saved with target and UUID
  - success status/result is published
  - no failure result is published
*/
func TestConfigureWorkflowSuccessAppliesAndSavesState(t *testing.T) {
	fixture := newPhase3WorkflowFixture(t, "cfg-phase3-success")

	if err := fixture.service.Handle(context.Background(), fixture.msg); err != nil {
		t.Fatalf("handle: %v", err)
	}

	if got := fixture.renderer.Calls(); got != 1 {
		t.Fatalf("renderer calls got=%d want=1", got)
	}
	if got := fixture.apply.Calls(); got != 1 {
		t.Fatalf("apply calls got=%d want=1", got)
	}
	if got := fixture.store.SaveCalls(); got != 1 {
		t.Fatalf("save calls got=%d want=1", got)
	}
	saved, ok := fixture.store.LastSavedState()
	if !ok {
		t.Fatal("expected saved state")
	}
	if saved.Target != fixture.msg.Target || saved.AppliedUUID != fixture.msg.UUID {
		t.Fatalf("saved state got=%+v want target=%q uuid=%q", saved, fixture.msg.Target, fixture.msg.UUID)
	}
	if !fixture.client.ContainsStatus("success", "applied") {
		t.Fatal("expected applied success status")
	}
	if !fixture.client.ContainsResult("success", "configure") {
		t.Fatal("expected configure success result")
	}
	assertNoFailureResult(t, fixture.client)
}

/*
TC-CONFIGURE-WORKFLOW-002
Type: Safety
Title: Configure saves state after apply
Summary:
Records workflow events on the successful configure path.
The local state checkpoint must be written only after rendered config
has been applied.

Validates:
  - render happens before apply
  - apply happens before state save
  - state is not checkpointed before apply
*/
func TestConfigureWorkflowSavesStateAfterApply(t *testing.T) {
	fixture := newPhase3WorkflowFixture(t, "cfg-phase3-order")

	if err := fixture.service.Handle(context.Background(), fixture.msg); err != nil {
		t.Fatalf("handle: %v", err)
	}

	assertEventOrder(t, fixture.events, "render", "apply")
	assertEventOrder(t, fixture.events, "apply", "state_save")
}

/*
TC-CONFIGURE-WORKFLOW-003
Type: Safety
Title: Configure publishes success after state save
Summary:
Records publish and state-save events on the successful configure path.
Final success status and success result should be published only after
the local UUID checkpoint has been saved.

Validates:
  - apply happens before state save
  - success status is published after state save
  - success result is published after state save
*/
func TestConfigureWorkflowPublishesSuccessAfterStateSave(t *testing.T) {
	fixture := newPhase3WorkflowFixture(t, "cfg-phase3-publish")

	if err := fixture.service.Handle(context.Background(), fixture.msg); err != nil {
		t.Fatalf("handle: %v", err)
	}

	assertEventOrder(t, fixture.events, "apply", "state_save")
	assertEventOrder(t, fixture.events, "state_save", "publish_success_status")
	assertEventOrder(t, fixture.events, "state_save", "publish_success")
}

/*
TC-CONFIGURE-WORKFLOW-004
Type: Positive
Title: Already in sync skips side effects
Summary:
Seeds local state with the same UUID as the incoming desired config.
The configure service should report already-in-sync success and skip
render, apply, and state save.

Validates:
  - renderer is not called
  - apply is not called
  - state save is not called
  - already_in_sync success is published
  - no failure result is published
*/
func TestConfigureWorkflowAlreadyInSyncSkipsApply(t *testing.T) {
	fixture := newPhase3WorkflowFixture(t, "cfg-phase3-sync")
	fixture.store.Current.AppliedUUID = fixture.msg.UUID

	if err := fixture.service.Handle(context.Background(), fixture.msg); err != nil {
		t.Fatalf("handle: %v", err)
	}

	if got := fixture.renderer.Calls(); got != 0 {
		t.Fatalf("renderer calls got=%d want=0", got)
	}
	if got := fixture.apply.Calls(); got != 0 {
		t.Fatalf("apply calls got=%d want=0", got)
	}
	if got := fixture.store.SaveCalls(); got != 0 {
		t.Fatalf("save calls got=%d want=0", got)
	}
	if !fixture.client.ContainsStatus("success", "already_in_sync") {
		t.Fatal("expected already_in_sync success status")
	}
	result, ok := fixture.client.LastResult()
	if !ok {
		t.Fatal("expected success result")
	}
	if result.Result != "success" || result.CommandType != "configure" || result.Message != "desired config already applied" {
		t.Fatalf("unexpected already-in-sync result: %+v", result)
	}
	assertNoFailureResult(t, fixture.client)
}

/*
TC-CONFIGURE-WORKFLOW-005
Type: Positive
Title: Repeated same UUID is idempotent
Summary:
Runs configure twice with the same desired UUID.
The first call should apply and save state; the second call should
detect the saved UUID and skip render/apply/save.

Validates:
  - first call renders, applies, and saves once
  - second call does not render again
  - second call does not apply again
  - second call does not save state again
  - second call publishes already-in-sync success
*/
func TestConfigureWorkflowRepeatedSameUUIDIsIdempotent(t *testing.T) {
	fixture := newPhase3WorkflowFixture(t, "cfg-phase3-repeat")

	if err := fixture.service.Handle(context.Background(), fixture.msg); err != nil {
		t.Fatalf("first handle: %v", err)
	}
	if got := fixture.renderer.Calls(); got != 1 {
		t.Fatalf("renderer calls after first handle got=%d want=1", got)
	}
	if got := fixture.apply.Calls(); got != 1 {
		t.Fatalf("apply calls after first handle got=%d want=1", got)
	}
	if got := fixture.store.SaveCalls(); got != 1 {
		t.Fatalf("save calls after first handle got=%d want=1", got)
	}

	if err := fixture.service.Handle(context.Background(), fixture.msg); err != nil {
		t.Fatalf("second handle: %v", err)
	}
	if got := fixture.renderer.Calls(); got != 1 {
		t.Fatalf("renderer calls after second handle got=%d want still 1", got)
	}
	if got := fixture.apply.Calls(); got != 1 {
		t.Fatalf("apply calls after second handle got=%d want still 1", got)
	}
	if got := fixture.store.SaveCalls(); got != 1 {
		t.Fatalf("save calls after second handle got=%d want still 1", got)
	}
	if !fixture.client.ContainsStatus("success", "already_in_sync") {
		t.Fatal("expected already_in_sync status on repeated UUID")
	}
	results := fixture.client.Results()
	if len(results) != 2 {
		t.Fatalf("result count got=%d want=2", len(results))
	}
	if results[0].Result != "success" || results[1].Result != "success" {
		t.Fatalf("unexpected results: %+v", results)
	}
	if results[1].Message != "desired config already applied" {
		t.Fatalf("second result message got=%q want already applied", results[1].Message)
	}
	assertNoFailureResult(t, fixture.client)
}

/*
TC-CONFIGURE-WORKFLOW-006
Type: Negative
Title: Invalid desired payload fails at renderer boundary
Summary:
Loads a desired config with malformed JSON payload.
The configure service should pass the payload through to the renderer
contract, which rejects invalid JSON after desired and local state are
loaded but before apply or state-save side effects run.

Validates:
  - desired config is loaded once
  - state load is called once
  - renderer is called once
  - apply is not called
  - state save is not called
  - failure result uses render_failed
*/
func TestConfigureWorkflowInvalidDesiredConfigFailsAtRendererBoundary(t *testing.T) {
	fixture := newPhase3WorkflowFixture(t, "cfg-phase3-invalid")
	invalid := testutil.DesiredConfig(fixture.msg.Target, fixture.msg.UUID, testutil.InvalidPayload())
	fixture.client.Desired = &invalid
	fixture.renderer.Validate = func(desired agentcore.StoredDesiredConfig) error {
		if !json.Valid(desired.Record.Payload) {
			return errors.New("invalid desired payload json")
		}
		return nil
	}

	err := fixture.service.Handle(context.Background(), fixture.msg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if got := fixture.client.LoadCalls(); got != 1 {
		t.Fatalf("desired load calls got=%d want=1", got)
	}
	if got := fixture.store.LoadCalls(); got != 1 {
		t.Fatalf("state load calls got=%d want=1", got)
	}
	if got := fixture.renderer.Calls(); got != 1 {
		t.Fatalf("renderer calls got=%d want=1", got)
	}
	if got := fixture.apply.Calls(); got != 0 {
		t.Fatalf("apply calls got=%d want=0", got)
	}
	if got := fixture.store.SaveCalls(); got != 0 {
		t.Fatalf("save calls got=%d want=0", got)
	}
	result, ok := fixture.client.LastResult()
	if !ok {
		t.Fatal("expected failure result")
	}
	if result.Result != "failure" || result.ErrorCode != "render_failed" {
		t.Fatalf("failure result got=%+v want result=failure error_code=render_failed", result)
	}
	if result.Target != fixture.msg.Target || result.UUID != fixture.msg.UUID || result.RPCID != fixture.msg.RPCID {
		t.Fatalf("failure result lost correlation data: %+v", result)
	}
	assertNoSuccessResult(t, fixture.client)
}

/*
TC-CONFIGURE-WORKFLOW-007
Type: Negative
Title: Missing desired config fails before side effects
Summary:
Configures the fake client to return nil desired config.
The configure service should publish a safe failure and stop before
state load, render, apply, or state save.

Validates:
  - desired config is loaded once
  - state load is not called
  - renderer is not called
  - apply is not called
  - state save is not called
  - failure result uses desired_config_missing
*/
func TestConfigureWorkflowMissingDesiredConfigFailsBeforeSideEffects(t *testing.T) {
	fixture := newPhase3WorkflowFixture(t, "cfg-phase3-missing")
	fixture.client.Desired = nil

	err := fixture.service.Handle(context.Background(), fixture.msg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	assertNoConfigureSideEffectsAfterDesiredLoad(t, fixture)
	result := assertFailureResult(t, fixture.client, "desired_config_missing")
	assertResultCorrelation(t, result, fixture.msg.Target, fixture.msg.UUID, fixture.msg.RPCID)
	assertNoSuccessResult(t, fixture.client)
}

/*
TC-CONFIGURE-WORKFLOW-008
Type: Safety
Title: Wrong target fails before side effects
Summary:
Loads a desired config whose target differs from the configure
notification target. The service should fail safely using notification
correlation data and stop before side effects.

Validates:
  - state load is not called
  - renderer is not called
  - apply is not called
  - state save is not called
  - failure result uses desired_target_mismatch
  - no success result is published
*/
func TestConfigureWorkflowWrongTargetFailsBeforeSideEffects(t *testing.T) {
	fixture := newPhase3WorkflowFixture(t, "cfg-phase3-wrong-target")
	desired := testutil.DesiredConfig("other-target", fixture.msg.UUID, testutil.MinimalDesiredConfig().Record.Payload)
	fixture.client.Desired = &desired

	err := fixture.service.Handle(context.Background(), fixture.msg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	assertNoConfigureSideEffectsAfterDesiredLoad(t, fixture)
	result := assertFailureResult(t, fixture.client, "desired_target_mismatch")
	assertResultCorrelation(t, result, fixture.msg.Target, fixture.msg.UUID, fixture.msg.RPCID)
	assertNoSuccessResult(t, fixture.client)
}

/*
TC-CONFIGURE-WORKFLOW-009
Type: Safety
Title: Desired UUID mismatch fails before side effects
Summary:
Loads a desired config whose UUID differs from the configure
notification UUID. The service should fail safely and stop before
state load, render, apply, or state save.

Validates:
  - state load is not called
  - renderer is not called
  - apply is not called
  - state save is not called
  - failure result uses desired_uuid_mismatch
  - no success result is published
*/
func TestConfigureWorkflowDesiredUUIDMismatchFailsBeforeSideEffects(t *testing.T) {
	fixture := newPhase3WorkflowFixture(t, "cfg-phase3-uuid-mismatch")
	desired := testutil.DesiredConfig(fixture.msg.Target, "cfg-other", testutil.MinimalDesiredConfig().Record.Payload)
	fixture.client.Desired = &desired

	err := fixture.service.Handle(context.Background(), fixture.msg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	assertNoConfigureSideEffectsAfterDesiredLoad(t, fixture)
	result := assertFailureResult(t, fixture.client, "desired_uuid_mismatch")
	assertResultCorrelation(t, result, fixture.msg.Target, fixture.msg.UUID, fixture.msg.RPCID)
	assertNoSuccessResult(t, fixture.client)
}

/*
TC-CONFIGURE-WORKFLOW-010
Type: Negative
Title: Empty target fails before side effects
Summary:
Uses an empty notification target while the fake client still has a
non-empty desired target available.
The configure service should reject the target before local state,
desired lookup, renderer, apply, or state-save side effects run.

Validates:
  - desired config is not loaded
  - state load is not called
  - renderer is not called
  - apply is not called
  - state save is not called
  - failure result uses notification_target_invalid
  - no success result is published
*/
func TestConfigureWorkflowEmptyTargetFailsBeforeSideEffects(t *testing.T) {
	fixture := newPhase3WorkflowFixture(t, "cfg-phase3-empty-target")
	fixture.msg.Target = ""
	desired := testutil.DesiredConfig("vyos", fixture.msg.UUID, testutil.MinimalDesiredConfig().Record.Payload)
	fixture.client.Desired = &desired

	err := fixture.service.Handle(context.Background(), fixture.msg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if got := fixture.client.LoadCalls(); got != 0 {
		t.Fatalf("desired load calls got=%d want=0", got)
	}
	if got := fixture.store.LoadCalls(); got != 0 {
		t.Fatalf("state load calls got=%d want=0", got)
	}
	if got := fixture.renderer.Calls(); got != 0 {
		t.Fatalf("renderer calls got=%d want=0", got)
	}
	if got := fixture.apply.Calls(); got != 0 {
		t.Fatalf("apply calls got=%d want=0", got)
	}
	if got := fixture.store.SaveCalls(); got != 0 {
		t.Fatalf("save calls got=%d want=0", got)
	}
	result := assertFailureResult(t, fixture.client, "notification_target_invalid")
	assertResultCorrelation(t, result, "", fixture.msg.UUID, fixture.msg.RPCID)
	assertNoSuccessResult(t, fixture.client)
}

/*
TC-CONFIGURE-WORKFLOW-011
Type: Negative
Title: Empty UUID fails before side effects
Summary:
Uses an empty notification UUID while the fake client still has a
non-empty desired UUID available.
The configure service should reject the UUID before local state,
desired lookup, renderer, apply, or state-save side effects run.

Validates:
  - desired config is not loaded
  - state load is not called
  - renderer is not called
  - apply is not called
  - state save is not called
  - failure result uses notification_uuid_invalid
  - failure result preserves correlation fields
*/
func TestConfigureWorkflowEmptyUUIDFailsBeforeSideEffects(t *testing.T) {
	fixture := newPhase3WorkflowFixture(t, "cfg-phase3-empty")
	fixture.msg.UUID = ""
	invalid := testutil.DesiredConfig(fixture.msg.Target, "cfg-non-empty", testutil.MinimalDesiredConfig().Record.Payload)
	fixture.client.Desired = &invalid

	err := fixture.service.Handle(context.Background(), fixture.msg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if got := fixture.client.LoadCalls(); got != 0 {
		t.Fatalf("desired load calls got=%d want=0", got)
	}
	if got := fixture.store.LoadCalls(); got != 0 {
		t.Fatalf("state load calls got=%d want=0", got)
	}
	if got := fixture.renderer.Calls(); got != 0 {
		t.Fatalf("renderer calls got=%d want=0", got)
	}
	if got := fixture.apply.Calls(); got != 0 {
		t.Fatalf("apply calls got=%d want=0", got)
	}
	if got := fixture.store.SaveCalls(); got != 0 {
		t.Fatalf("save calls got=%d want=0", got)
	}
	result, ok := fixture.client.LastResult()
	if !ok {
		t.Fatal("expected failure result")
	}
	if result.Result != "failure" || result.ErrorCode != "notification_uuid_invalid" {
		t.Fatalf("failure result got=%+v want result=failure error_code=notification_uuid_invalid", result)
	}
	if result.Target != fixture.msg.Target || result.UUID != "" || result.RPCID != fixture.msg.RPCID {
		t.Fatalf("failure result lost correlation data: %+v", result)
	}
}

/*
TC-CONFIGURE-WORKFLOW-012
Type: Positive
Title: New UUID triggers render apply and state update
Summary:
Seeds local state with an older applied UUID and handles a new desired
UUID. The service should treat the config as new, render/apply it, and
save the new UUID checkpoint.

Validates:
  - renderer is called exactly once
  - apply is called exactly once
  - state is saved exactly once
  - saved state contains the new UUID
  - success result is published
  - no failure result is published
*/
func TestConfigureWorkflowNewUUIDTriggersRenderApplyAndStateUpdate(t *testing.T) {
	fixture := newPhase3WorkflowFixture(t, "cfg-phase3-new")
	fixture.store.Current.AppliedUUID = "cfg-old"

	if err := fixture.service.Handle(context.Background(), fixture.msg); err != nil {
		t.Fatalf("handle: %v", err)
	}

	if got := fixture.renderer.Calls(); got != 1 {
		t.Fatalf("renderer calls got=%d want=1", got)
	}
	if got := fixture.apply.Calls(); got != 1 {
		t.Fatalf("apply calls got=%d want=1", got)
	}
	if got := fixture.store.SaveCalls(); got != 1 {
		t.Fatalf("save calls got=%d want=1", got)
	}
	saved, ok := fixture.store.LastSavedState()
	if !ok {
		t.Fatal("expected saved state")
	}
	if saved.AppliedUUID != fixture.msg.UUID {
		t.Fatalf("saved uuid got=%q want=%q", saved.AppliedUUID, fixture.msg.UUID)
	}
	if !fixture.client.ContainsResult("success", "configure") {
		t.Fatal("expected configure success result")
	}
	assertNoFailureResult(t, fixture.client)
}

/*
TC-CONFIGURE-WORKFLOW-013
Type: Positive
Title: Configure success preserves correlation identifiers
Summary:
Runs a successful configure workflow and inspects result, renderer input,
and apply input. Target, UUID, and RPC ID should be preserved across the
workflow.

Validates:
  - success result preserves rpc_id, target, and uuid
  - renderer input preserves target and uuid
  - apply input preserves target and uuid
*/
func TestConfigureWorkflowSuccessPreservesCorrelationIdentifiers(t *testing.T) {
	fixture := newPhase3WorkflowFixture(t, "cfg-phase3-correlation")

	if err := fixture.service.Handle(context.Background(), fixture.msg); err != nil {
		t.Fatalf("handle: %v", err)
	}

	result, ok := fixture.client.LastResult()
	if !ok {
		t.Fatal("expected result")
	}
	if result.RPCID != fixture.msg.RPCID || result.Target != fixture.msg.Target || result.UUID != fixture.msg.UUID {
		t.Fatalf("result correlation got rpc_id=%q target=%q uuid=%q", result.RPCID, result.Target, result.UUID)
	}
	input, ok := fixture.renderer.LastInput()
	if !ok {
		t.Fatal("expected renderer input")
	}
	if input.Record.Target != fixture.msg.Target || input.Record.UUID != fixture.msg.UUID {
		t.Fatalf("renderer input got target=%q uuid=%q", input.Record.Target, input.Record.UUID)
	}
	applied, ok := fixture.apply.LastInput()
	if !ok {
		t.Fatal("expected apply input")
	}
	if applied.Target != fixture.msg.Target || applied.UUID != fixture.msg.UUID {
		t.Fatalf("apply input got target=%q uuid=%q", applied.Target, applied.UUID)
	}
}

type phase3WorkflowFixture struct {
	msg      agentcore.ConfigureNotification
	client   *testutil.FakeConfigureClient
	store    *testutil.FakeStateStore
	renderer *testutil.FakeRenderer
	apply    *testutil.FakeApplyEngine
	events   *testutil.EventRecorder
	service  *Service
}

func newPhase3WorkflowFixture(t *testing.T, uuid string) phase3WorkflowFixture {
	t.Helper()

	events := &testutil.EventRecorder{}
	msg := testutil.MinimalConfigureNotification()
	msg.UUID = uuid
	msg.RPCID = "rpc-" + uuid

	desired := testutil.DesiredConfig(msg.Target, msg.UUID, testutil.MinimalDesiredConfig().Record.Payload)
	client := &testutil.FakeConfigureClient{
		Desired: &desired,
		Events:  events,
	}
	client.StatusRecorder.Events = events
	client.ResultRecorder.Events = events

	store := &testutil.FakeStateStore{Events: events}
	rndr := &testutil.FakeRenderer{
		Output: renderer.Output{
			Target: msg.Target,
			UUID:   msg.UUID,
			Text:   "set system host-name phase3\n",
		},
		UseOutput: true,
		Events:    events,
	}
	apply := &testutil.FakeApplyEngine{Events: events}

	svc, err := NewService(Dependencies{
		Client:      client,
		StateStore:  store,
		Renderer:    rndr,
		ApplyEngine: apply,
		Now: func() time.Time {
			return time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	return phase3WorkflowFixture{
		msg:      msg,
		client:   client,
		store:    store,
		renderer: rndr,
		apply:    apply,
		events:   events,
		service:  svc,
	}
}

func assertNoFailureResult(t *testing.T, recorder *testutil.FakeConfigureClient) {
	t.Helper()

	for _, result := range recorder.Results() {
		if result.Result == "failure" {
			t.Fatalf("unexpected failure result: %+v", result)
		}
	}
}

func assertNoSuccessResult(t *testing.T, recorder *testutil.FakeConfigureClient) {
	t.Helper()

	for _, result := range recorder.Results() {
		if result.Result == "success" {
			t.Fatalf("unexpected success result: %+v", result)
		}
	}
}

func assertFailureResult(t *testing.T, recorder *testutil.FakeConfigureClient, wantCode string) agentcore.ResultEnvelope {
	t.Helper()

	result, ok := recorder.LastResult()
	if !ok {
		t.Fatal("expected failure result")
	}
	if result.Result != "failure" || result.ErrorCode != wantCode {
		t.Fatalf("failure result got=%+v want result=failure error_code=%s", result, wantCode)
	}
	return result
}

func assertResultCorrelation(t *testing.T, result agentcore.ResultEnvelope, target, uuid, rpcID string) {
	t.Helper()

	if result.Target != target || result.UUID != uuid || result.RPCID != rpcID {
		t.Fatalf("result lost correlation data: %+v want target=%q uuid=%q rpc_id=%q", result, target, uuid, rpcID)
	}
}

func assertNoConfigureSideEffectsAfterDesiredLoad(t *testing.T, fixture phase3WorkflowFixture) {
	t.Helper()

	if got := fixture.client.LoadCalls(); got != 1 {
		t.Fatalf("desired load calls got=%d want=1", got)
	}
	if got := fixture.store.LoadCalls(); got != 0 {
		t.Fatalf("state load calls got=%d want=0", got)
	}
	if got := fixture.renderer.Calls(); got != 0 {
		t.Fatalf("renderer calls got=%d want=0", got)
	}
	if got := fixture.apply.Calls(); got != 0 {
		t.Fatalf("apply calls got=%d want=0", got)
	}
	if got := fixture.store.SaveCalls(); got != 0 {
		t.Fatalf("save calls got=%d want=0", got)
	}
}

func assertEventOrder(t *testing.T, recorder *testutil.EventRecorder, before string, after string) {
	t.Helper()

	beforeIndex := recorder.Index(before)
	if beforeIndex < 0 {
		t.Fatalf("missing event %q in %v", before, recorder.Events())
	}
	afterIndex := recorder.Index(after)
	if afterIndex < 0 {
		t.Fatalf("missing event %q in %v", after, recorder.Events())
	}
	if beforeIndex >= afterIndex {
		t.Fatalf("event order got %q at %d and %q at %d in %v", before, beforeIndex, after, afterIndex, recorder.Events())
	}
}
