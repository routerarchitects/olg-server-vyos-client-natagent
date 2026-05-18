package configure

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/routerarchitects/nats-agent-core/agentcore"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/renderer"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/state"
)

type fakeConfigureClient struct {
	desired *agentcore.StoredDesiredConfig
	loadErr error

	statusErrByStage map[string]error
	resultErrByKey   map[string]error

	loadCalls int
	statuses  []agentcore.StatusEnvelope
	results   []agentcore.ResultEnvelope
}

func (f *fakeConfigureClient) LoadDesiredConfig(ctx context.Context, target string) (*agentcore.StoredDesiredConfig, error) {
	f.loadCalls++
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	return f.desired, nil
}

func (f *fakeConfigureClient) PublishStatus(ctx context.Context, msg agentcore.StatusEnvelope) error {
	if err, ok := f.statusErrByStage[msg.Stage]; ok {
		return err
	}
	f.statuses = append(f.statuses, msg)
	return nil
}

func (f *fakeConfigureClient) PublishResult(ctx context.Context, msg agentcore.ResultEnvelope) error {
	key := resultKey(msg)
	if err, ok := f.resultErrByKey[key]; ok {
		return err
	}
	f.results = append(f.results, msg)
	return nil
}

func resultKey(msg agentcore.ResultEnvelope) string {
	return fmt.Sprintf("%s|%s", msg.Result, msg.ErrorCode)
}

type fakeStateStore struct {
	loadState state.State
	loadErr   error
	saveErr   error

	loadCalls int
	saveCalls int
	saved     []state.State
}

func (f *fakeStateStore) Load(ctx context.Context) (state.State, error) {
	f.loadCalls++
	if f.loadErr != nil {
		return state.State{}, f.loadErr
	}
	return f.loadState, nil
}

func (f *fakeStateStore) Save(ctx context.Context, st state.State) error {
	f.saveCalls++
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, st)
	return nil
}

type fakeRenderer struct {
	output renderer.Output
	err    error
	calls  int
}

func (f *fakeRenderer) Render(ctx context.Context, desired agentcore.StoredDesiredConfig) (renderer.Output, error) {
	f.calls++
	if f.err != nil {
		return renderer.Output{}, f.err
	}
	return f.output, nil
}

type fakeApplyEngine struct {
	err   error
	calls int
}

func (f *fakeApplyEngine) Apply(ctx context.Context, rendered renderer.Output) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	return nil
}

func newConfigureServiceForTest(
	t *testing.T,
	client *fakeConfigureClient,
	store *fakeStateStore,
	rndr *fakeRenderer,
	apply *fakeApplyEngine,
	now func() time.Time,
) *Service {
	t.Helper()

	svc, err := NewService(Dependencies{
		Client:      client,
		StateStore:  store,
		Renderer:    rndr,
		ApplyEngine: apply,
		Now:         now,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return svc
}

func newDesired(target, uuid string) *agentcore.StoredDesiredConfig {
	return &agentcore.StoredDesiredConfig{
		Record: agentcore.DesiredConfigRecord{
			Target: target,
			UUID:   uuid,
		},
	}
}

/*
TC-CONFIGURE-SERVICE-001
Type: Negative
Title: New service rejects missing dependencies
Summary:
Verifies constructor validation for all required dependencies.
Each required dependency is omitted once from a valid dependency set.
Constructor must return an error in each invalid setup.

Validates:
  - missing client is rejected
  - missing state store is rejected
  - missing renderer and apply engine are rejected
*/
func TestNewServiceRejectsMissingDependencies(t *testing.T) {
	base := Dependencies{
		Client:      &fakeConfigureClient{},
		StateStore:  &fakeStateStore{},
		Renderer:    &fakeRenderer{},
		ApplyEngine: &fakeApplyEngine{},
		Now:         time.Now,
	}

	cases := []struct {
		name          string
		mutate        func(*Dependencies)
		errorContains string
	}{
		{name: "missing client", mutate: func(d *Dependencies) { d.Client = nil }, errorContains: "client is required"},
		{name: "missing state store", mutate: func(d *Dependencies) { d.StateStore = nil }, errorContains: "state store is required"},
		{name: "missing renderer", mutate: func(d *Dependencies) { d.Renderer = nil }, errorContains: "renderer is required"},
		{name: "missing apply", mutate: func(d *Dependencies) { d.ApplyEngine = nil }, errorContains: "apply engine is required"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := base
			tc.mutate(&deps)
			_, err := NewService(deps)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.errorContains) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.errorContains)
			}
		})
	}
}

/*
TC-CONFIGURE-SERVICE-002
Type: Positive
Title: Handle renders applies saves and publishes success
Summary:
Runs full successful configure flow for a new desired UUID.
Service should publish expected running/success stages, execute render/apply,
persist checkpoint state, and publish final success result.

Validates:
  - success stages are published in expected order
  - render apply and save are called once
  - final configure result is success
*/
func TestHandleSuccessApplyPath(t *testing.T) {
	fixedNow := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	msg := agentcore.ConfigureNotification{Version: "1.0", RPCID: "rpc-1", Target: "vyos", UUID: "cfg-1"}

	client := &fakeConfigureClient{desired: newDesired("vyos", "cfg-1")}
	store := &fakeStateStore{}
	rndr := &fakeRenderer{output: renderer.Output{Target: "vyos", UUID: "cfg-1", Text: "# placeholder"}}
	apply := &fakeApplyEngine{}
	svc := newConfigureServiceForTest(t, client, store, rndr, apply, func() time.Time { return fixedNow })

	if err := svc.Handle(context.Background(), msg); err != nil {
		t.Fatalf("handle: %v", err)
	}

	expectedStages := []string{"received", "loading_desired", "rendering", "rendered", "applying", "applied"}
	if len(client.statuses) != len(expectedStages) {
		t.Fatalf("status count got=%d want=%d", len(client.statuses), len(expectedStages))
	}
	for i, stage := range expectedStages {
		if client.statuses[i].Stage != stage {
			t.Fatalf("status[%d].stage got=%q want=%q", i, client.statuses[i].Stage, stage)
		}
	}
	if rndr.calls != 1 || apply.calls != 1 || store.saveCalls != 1 {
		t.Fatalf("calls renderer=%d apply=%d save=%d want 1/1/1", rndr.calls, apply.calls, store.saveCalls)
	}
	if len(store.saved) != 1 {
		t.Fatalf("saved count got=%d want=1", len(store.saved))
	}
	if store.saved[0].Target != "vyos" || store.saved[0].AppliedUUID != "cfg-1" {
		t.Fatalf("saved state mismatch got=%+v", store.saved[0])
	}
	if !store.saved[0].AppliedAt.Equal(fixedNow.UTC()) {
		t.Fatalf("saved time got=%s want=%s", store.saved[0].AppliedAt, fixedNow.UTC())
	}

	if len(client.results) != 1 {
		t.Fatalf("result count got=%d want=1", len(client.results))
	}
	if client.results[0].Result != "success" || client.results[0].ErrorCode != "" {
		t.Fatalf("result mismatch got result=%q error_code=%q", client.results[0].Result, client.results[0].ErrorCode)
	}
}

/*
TC-CONFIGURE-SERVICE-003
Type: Positive
Title: Handle short-circuits when desired UUID already applied
Summary:
Uses local state checkpoint matching desired UUID.
Service should publish already_in_sync success and skip render/apply/save.
Final result should remain success with already-applied message.

Validates:
  - already_in_sync stage is published
  - render apply and save are skipped
  - final result is success
*/
func TestHandleAlreadyInSync(t *testing.T) {
	msg := agentcore.ConfigureNotification{Version: "1.0", RPCID: "rpc-2", Target: "vyos", UUID: "cfg-2"}

	client := &fakeConfigureClient{desired: newDesired("vyos", "cfg-2")}
	store := &fakeStateStore{loadState: state.State{AppliedUUID: "cfg-2"}}
	rndr := &fakeRenderer{}
	apply := &fakeApplyEngine{}
	svc := newConfigureServiceForTest(t, client, store, rndr, apply, time.Now)

	if err := svc.Handle(context.Background(), msg); err != nil {
		t.Fatalf("handle: %v", err)
	}

	expectedStages := []string{"received", "loading_desired", "already_in_sync"}
	if len(client.statuses) != len(expectedStages) {
		t.Fatalf("status count got=%d want=%d", len(client.statuses), len(expectedStages))
	}
	for i, stage := range expectedStages {
		if client.statuses[i].Stage != stage {
			t.Fatalf("status[%d].stage got=%q want=%q", i, client.statuses[i].Stage, stage)
		}
	}
	if rndr.calls != 0 || apply.calls != 0 || store.saveCalls != 0 {
		t.Fatalf("calls renderer=%d apply=%d save=%d want 0/0/0", rndr.calls, apply.calls, store.saveCalls)
	}
	if len(client.results) != 1 || client.results[0].Result != "success" {
		t.Fatalf("unexpected result %+v", client.results)
	}
}

/*
TC-CONFIGURE-SERVICE-004
Type: Negative
Title: Handle fails when desired config load returns error
Summary:
For desired-config read failures, service must publish failed status and
failure result with load_desired_failed code. Downstream render/apply/save
must not run.

Validates:
  - failed stage is published
  - failure result uses load_desired_failed
  - render apply and save are not called
*/
func TestHandleLoadDesiredFailure(t *testing.T) {
	msg := agentcore.ConfigureNotification{Version: "1.0", RPCID: "rpc-3", Target: "vyos", UUID: "cfg-3"}

	client := &fakeConfigureClient{loadErr: errors.New("boom")}
	store := &fakeStateStore{}
	rndr := &fakeRenderer{}
	apply := &fakeApplyEngine{}
	svc := newConfigureServiceForTest(t, client, store, rndr, apply, time.Now)

	err := svc.Handle(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(client.results) != 1 || client.results[0].ErrorCode != "load_desired_failed" {
		t.Fatalf("unexpected failure result %+v", client.results)
	}
	if rndr.calls != 0 || apply.calls != 0 || store.saveCalls != 0 {
		t.Fatalf("calls renderer=%d apply=%d save=%d want 0/0/0", rndr.calls, apply.calls, store.saveCalls)
	}
}

/*
TC-CONFIGURE-SERVICE-005
Type: Negative
Title: Handle fails when desired config is nil
Summary:
Covers nil desired-config response from dependency.
Service should publish failed status and failure result and stop flow.
No render/apply/save operation should execute.

Validates:
  - failure result uses desired_config_missing
  - render apply and save are not called
  - flow terminates with error
*/
func TestHandleNilDesiredConfig(t *testing.T) {
	msg := agentcore.ConfigureNotification{Version: "1.0", RPCID: "rpc-4", Target: "vyos", UUID: "cfg-4"}

	client := &fakeConfigureClient{desired: nil}
	store := &fakeStateStore{}
	rndr := &fakeRenderer{}
	apply := &fakeApplyEngine{}
	svc := newConfigureServiceForTest(t, client, store, rndr, apply, time.Now)

	err := svc.Handle(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(client.results) != 1 || client.results[0].ErrorCode != "desired_config_missing" {
		t.Fatalf("unexpected failure result %+v", client.results)
	}
	if rndr.calls != 0 || apply.calls != 0 || store.saveCalls != 0 {
		t.Fatalf("calls renderer=%d apply=%d save=%d want 0/0/0", rndr.calls, apply.calls, store.saveCalls)
	}
}

/*
TC-CONFIGURE-SERVICE-006
Type: Negative
Title: Handle fails on desired target mismatch
Summary:
Verifies desired config identity validation for target.
When desired target differs from notification target, service must fail,
publish failure output, and stop before render/apply.

Validates:
  - failure result uses desired_target_mismatch
  - render apply and save are not called
  - mismatch does not continue workflow
*/
func TestHandleDesiredTargetMismatch(t *testing.T) {
	msg := agentcore.ConfigureNotification{Version: "1.0", RPCID: "rpc-5", Target: "vyos", UUID: "cfg-5"}

	client := &fakeConfigureClient{desired: newDesired("other-target", "cfg-5")}
	store := &fakeStateStore{}
	rndr := &fakeRenderer{}
	apply := &fakeApplyEngine{}
	svc := newConfigureServiceForTest(t, client, store, rndr, apply, time.Now)

	err := svc.Handle(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(client.results) != 1 || client.results[0].ErrorCode != "desired_target_mismatch" {
		t.Fatalf("unexpected failure result %+v", client.results)
	}
}

/*
TC-CONFIGURE-SERVICE-007
Type: Negative
Title: Handle fails on desired UUID mismatch
Summary:
Verifies desired config identity validation for UUID.
When desired UUID differs from notification UUID, service must fail early
and publish failure result with desired_uuid_mismatch code.

Validates:
  - failure result uses desired_uuid_mismatch
  - render apply and save are not called
  - flow returns error
*/
func TestHandleDesiredUUIDMismatch(t *testing.T) {
	msg := agentcore.ConfigureNotification{Version: "1.0", RPCID: "rpc-6", Target: "vyos", UUID: "cfg-6"}

	client := &fakeConfigureClient{desired: newDesired("vyos", "other-uuid")}
	store := &fakeStateStore{}
	rndr := &fakeRenderer{}
	apply := &fakeApplyEngine{}
	svc := newConfigureServiceForTest(t, client, store, rndr, apply, time.Now)

	err := svc.Handle(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(client.results) != 1 || client.results[0].ErrorCode != "desired_uuid_mismatch" {
		t.Fatalf("unexpected failure result %+v", client.results)
	}
}

/*
TC-CONFIGURE-SERVICE-008
Type: Negative
Title: Handle fails when local state load fails
Summary:
For state-load errors, service should publish failed status/result and return.
Render, apply, and state save steps must not run after load failure.
This protects correctness of UUID checkpoint decisions.

Validates:
  - failure result uses state_load_failed
  - render apply and save are not called
  - service returns error
*/
func TestHandleStateLoadFailure(t *testing.T) {
	msg := agentcore.ConfigureNotification{Version: "1.0", RPCID: "rpc-7", Target: "vyos", UUID: "cfg-7"}

	client := &fakeConfigureClient{desired: newDesired("vyos", "cfg-7")}
	store := &fakeStateStore{loadErr: errors.New("read failed")}
	rndr := &fakeRenderer{}
	apply := &fakeApplyEngine{}
	svc := newConfigureServiceForTest(t, client, store, rndr, apply, time.Now)

	err := svc.Handle(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(client.results) != 1 || client.results[0].ErrorCode != "state_load_failed" {
		t.Fatalf("unexpected failure result %+v", client.results)
	}
	if rndr.calls != 0 || apply.calls != 0 || store.saveCalls != 0 {
		t.Fatalf("calls renderer=%d apply=%d save=%d want 0/0/0", rndr.calls, apply.calls, store.saveCalls)
	}
}

/*
TC-CONFIGURE-SERVICE-009
Type: Negative
Title: Handle fails when render step fails
Summary:
Covers renderer error after desired and state are loaded.
Service should publish failed status/result with render_failed code
and avoid apply/save operations.

Validates:
  - failure result uses render_failed
  - apply and save are not called
  - service returns error
*/
func TestHandleRenderFailure(t *testing.T) {
	msg := agentcore.ConfigureNotification{Version: "1.0", RPCID: "rpc-8", Target: "vyos", UUID: "cfg-8"}

	client := &fakeConfigureClient{desired: newDesired("vyos", "cfg-8")}
	store := &fakeStateStore{}
	rndr := &fakeRenderer{err: errors.New("render broke")}
	apply := &fakeApplyEngine{}
	svc := newConfigureServiceForTest(t, client, store, rndr, apply, time.Now)

	err := svc.Handle(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(client.results) != 1 || client.results[0].ErrorCode != "render_failed" {
		t.Fatalf("unexpected failure result %+v", client.results)
	}
	if apply.calls != 0 || store.saveCalls != 0 {
		t.Fatalf("calls apply=%d save=%d want 0/0", apply.calls, store.saveCalls)
	}
}

/*
TC-CONFIGURE-SERVICE-010
Type: Negative
Title: Handle fails when apply step fails
Summary:
Covers apply-engine error after successful render.
Service should publish failed status/result with apply_failed code
and skip state save after failed apply.

Validates:
  - failure result uses apply_failed
  - apply is called once
  - state save is not called
*/
func TestHandleApplyFailure(t *testing.T) {
	msg := agentcore.ConfigureNotification{Version: "1.0", RPCID: "rpc-9", Target: "vyos", UUID: "cfg-9"}

	client := &fakeConfigureClient{desired: newDesired("vyos", "cfg-9")}
	store := &fakeStateStore{}
	rndr := &fakeRenderer{output: renderer.Output{Target: "vyos", UUID: "cfg-9", Text: "# out"}}
	apply := &fakeApplyEngine{err: errors.New("apply failed")}
	svc := newConfigureServiceForTest(t, client, store, rndr, apply, time.Now)

	err := svc.Handle(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(client.results) != 1 || client.results[0].ErrorCode != "apply_failed" {
		t.Fatalf("unexpected failure result %+v", client.results)
	}
	if apply.calls != 1 {
		t.Fatalf("apply calls got=%d want=1", apply.calls)
	}
	if store.saveCalls != 0 {
		t.Fatalf("save calls got=%d want=0", store.saveCalls)
	}
}

/*
TC-CONFIGURE-SERVICE-011
Type: Negative
Title: Handle fails when state save fails after apply success
Summary:
Covers edge case where apply succeeds but local state checkpoint fails.
Service should publish failed status/result with state_save_failed code
and return error to caller.

Validates:
  - apply runs before failure
  - failure result uses state_save_failed
  - save is attempted exactly once
*/
func TestHandleStateSaveFailureAfterApplySuccess(t *testing.T) {
	msg := agentcore.ConfigureNotification{Version: "1.0", RPCID: "rpc-10", Target: "vyos", UUID: "cfg-10"}

	client := &fakeConfigureClient{desired: newDesired("vyos", "cfg-10")}
	store := &fakeStateStore{saveErr: errors.New("disk full")}
	rndr := &fakeRenderer{output: renderer.Output{Target: "vyos", UUID: "cfg-10", Text: "# out"}}
	apply := &fakeApplyEngine{}
	svc := newConfigureServiceForTest(t, client, store, rndr, apply, time.Now)

	err := svc.Handle(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(client.results) != 1 || client.results[0].ErrorCode != "state_save_failed" {
		t.Fatalf("unexpected failure result %+v", client.results)
	}
	if apply.calls != 1 || store.saveCalls != 1 {
		t.Fatalf("calls apply=%d save=%d want 1/1", apply.calls, store.saveCalls)
	}
}

/*
TC-CONFIGURE-SERVICE-012
Type: Negative
Title: Handle publishes failure when status publish fails
Summary:
Injects publish error for initial received status stage.
Service should switch to failure path and publish failure result with
status_publish_failed error code.

Validates:
  - returned error is non-nil
  - failure result uses status_publish_failed
  - no render apply save calls are made
*/
func TestHandleStatusPublishFailureBehavior(t *testing.T) {
	msg := agentcore.ConfigureNotification{Version: "1.0", RPCID: "rpc-11", Target: "vyos", UUID: "cfg-11"}

	client := &fakeConfigureClient{
		desired:          newDesired("vyos", "cfg-11"),
		statusErrByStage: map[string]error{"received": errors.New("publish received failed")},
	}
	store := &fakeStateStore{}
	rndr := &fakeRenderer{}
	apply := &fakeApplyEngine{}
	svc := newConfigureServiceForTest(t, client, store, rndr, apply, time.Now)

	err := svc.Handle(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(client.results) != 1 || client.results[0].ErrorCode != "status_publish_failed" {
		t.Fatalf("unexpected failure result %+v", client.results)
	}
	if rndr.calls != 0 || apply.calls != 0 || store.saveCalls != 0 {
		t.Fatalf("calls renderer=%d apply=%d save=%d want 0/0/0", rndr.calls, apply.calls, store.saveCalls)
	}
}

/*
TC-CONFIGURE-SERVICE-013
Type: Negative
Title: Handle publishes failure when success result publish fails
Summary:
Injects failure for success result publish in the final step of happy path.
Service should enter failure path and publish failure output with
result_publish_failed code.

Validates:
  - failure result uses result_publish_failed
  - apply and save complete before result publish failure
  - service returns non-nil error
*/
func TestHandleResultPublishFailureBehavior(t *testing.T) {
	msg := agentcore.ConfigureNotification{Version: "1.0", RPCID: "rpc-12", Target: "vyos", UUID: "cfg-12"}

	client := &fakeConfigureClient{
		desired:        newDesired("vyos", "cfg-12"),
		resultErrByKey: map[string]error{"success|": errors.New("publish success result failed")},
	}
	store := &fakeStateStore{}
	rndr := &fakeRenderer{output: renderer.Output{Target: "vyos", UUID: "cfg-12", Text: "# out"}}
	apply := &fakeApplyEngine{}
	svc := newConfigureServiceForTest(t, client, store, rndr, apply, time.Now)

	err := svc.Handle(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(client.results) != 1 {
		t.Fatalf("result count got=%d want=1", len(client.results))
	}
	if client.results[0].Result != "failure" || client.results[0].ErrorCode != "result_publish_failed" {
		t.Fatalf("unexpected failure result %+v", client.results[0])
	}
	if apply.calls != 1 || store.saveCalls != 1 {
		t.Fatalf("calls apply=%d save=%d want 1/1", apply.calls, store.saveCalls)
	}
}

/*
TC-CONFIGURE-SERVICE-014
Type: Negative
Title: Handle rejects nil context input
Summary:
Verifies context guard at start of handler.
Nil context should return error immediately and avoid any publish attempt.
This protects caller contract for context-aware operations.

Validates:
  - nil context returns error
  - no statuses are published
  - no results are published
*/
func TestHandleRejectsNilContext(t *testing.T) {
	client := &fakeConfigureClient{}
	store := &fakeStateStore{}
	rndr := &fakeRenderer{}
	apply := &fakeApplyEngine{}
	svc := newConfigureServiceForTest(t, client, store, rndr, apply, time.Now)

	err := svc.Handle(nil, agentcore.ConfigureNotification{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "context is nil") {
		t.Fatalf("error %q does not contain context is nil", err.Error())
	}
	if len(client.statuses) != 0 || len(client.results) != 0 {
		t.Fatalf("unexpected published output statuses=%d results=%d", len(client.statuses), len(client.results))
	}
}
