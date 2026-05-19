package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/routerarchitects/nats-agent-core/agentcore"
)

type fakeActionClient struct {
	statusErrByStage map[string]error
	resultErrByKey   map[string]error
	strictContext    bool

	statuses []agentcore.StatusEnvelope
	results  []agentcore.ResultEnvelope
}

func (f *fakeActionClient) PublishStatus(ctx context.Context, msg agentcore.StatusEnvelope) error {
	if f.strictContext && ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if err, ok := f.statusErrByStage[msg.Stage]; ok {
		return err
	}
	f.statuses = append(f.statuses, msg)
	return nil
}

func (f *fakeActionClient) PublishResult(ctx context.Context, msg agentcore.ResultEnvelope) error {
	if f.strictContext && ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	key := fmt.Sprintf("%s|%s", msg.Result, msg.ErrorCode)
	if err, ok := f.resultErrByKey[key]; ok {
		return err
	}
	f.results = append(f.results, msg)
	return nil
}

type fakeActionExecutor struct {
	output  Output
	err     error
	calls   int
	lastMsg agentcore.ActionCommand
}

func (f *fakeActionExecutor) Execute(ctx context.Context, msg agentcore.ActionCommand) (Output, error) {
	f.calls++
	f.lastMsg = msg
	if f.err != nil {
		return Output{}, f.err
	}
	return f.output, nil
}

func newActionServiceForTest(
	t *testing.T,
	client *fakeActionClient,
	exec map[string]Executor,
	enabled []string,
	now func() time.Time,
) *Service {
	t.Helper()

	svc, err := NewService(Dependencies{
		Client:    client,
		Executors: exec,
		Enabled:   enabled,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("new action service: %v", err)
	}
	return svc
}

/*
TC-ACTION-SERVICE-001
Type: Negative
Title: New service rejects invalid dependencies
Summary:
Validates constructor guards for required dependencies and invalid executor maps.
Each case mutates one field from a valid baseline and expects a clear error.

Validates:
  - missing client is rejected
  - empty executors is rejected
  - nil executor entry is rejected
*/
func TestNewServiceRejectsInvalidDependencies(t *testing.T) {
	base := Dependencies{
		Client: &fakeActionClient{},
		Enabled: []string{
			ActionTrace,
		},
		Executors: map[string]Executor{
			ActionTrace: &fakeActionExecutor{},
		},
		Now: time.Now,
	}

	cases := []struct {
		name          string
		mutate        func(*Dependencies)
		errorContains string
	}{
		{
			name: "missing client",
			mutate: func(d *Dependencies) {
				d.Client = nil
			},
			errorContains: "client is required",
		},
		{
			name: "empty executors",
			mutate: func(d *Dependencies) {
				d.Executors = map[string]Executor{}
			},
			errorContains: "at least one executor is required",
		},
		{
			name: "nil executor entry",
			mutate: func(d *Dependencies) {
				d.Executors = map[string]Executor{ActionTrace: nil}
			},
			errorContains: "executor \"trace\" is nil",
		},
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
TC-ACTION-SERVICE-002
Type: Positive
Title: Handle success executes trace and publishes running-to-success flow
Summary:
Runs the happy path for enabled trace action with a fake executor.
Service should publish received/executing/completed statuses and one success result.

Validates:
  - executor is called once
  - payload is passed to executor
  - success result includes action metadata and executor payload
  - status stages are emitted in expected order
*/
func TestHandleSuccessPublishesStatusesAndSuccessResult(t *testing.T) {
	fixedNow := time.Date(2026, 5, 18, 16, 0, 0, 0, time.UTC)
	inPayload := json.RawMessage(`{"host":"1.1.1.1"}`)
	outPayload := json.RawMessage(`{"executor":"placeholder_trace","ok":true}`)
	msg := agentcore.ActionCommand{
		Version: "1.0",
		RPCID:   "rpc-action-1",
		Target:  "vyos",
		Action:  ActionTrace,
		Payload: inPayload,
	}

	client := &fakeActionClient{}
	exec := &fakeActionExecutor{
		output: Output{
			Message: "placeholder trace action completed",
			Payload: outPayload,
		},
	}
	svc := newActionServiceForTest(
		t,
		client,
		map[string]Executor{ActionTrace: exec},
		[]string{ActionTrace},
		func() time.Time { return fixedNow },
	)

	if err := svc.Handle(context.Background(), msg); err != nil {
		t.Fatalf("handle: %v", err)
	}

	if exec.calls != 1 {
		t.Fatalf("executor calls got=%d want=1", exec.calls)
	}
	if string(exec.lastMsg.Payload) != string(inPayload) {
		t.Fatalf("executor payload got=%s want=%s", string(exec.lastMsg.Payload), string(inPayload))
	}

	wantStages := []string{"received", "executing", "completed"}
	if len(client.statuses) != len(wantStages) {
		t.Fatalf("status count got=%d want=%d", len(client.statuses), len(wantStages))
	}
	for i, stage := range wantStages {
		if client.statuses[i].Stage != stage {
			t.Fatalf("status[%d].stage got=%q want=%q", i, client.statuses[i].Stage, stage)
		}
	}

	if len(client.results) != 1 {
		t.Fatalf("result count got=%d want=1", len(client.results))
	}
	got := client.results[0]
	if got.CommandType != "action" || got.Action != ActionTrace || got.Result != "success" {
		t.Fatalf("result metadata mismatch got=%+v", got)
	}
	if got.Target != msg.Target || got.RPCID != msg.RPCID {
		t.Fatalf("result identity mismatch got target=%q rpc_id=%q", got.Target, got.RPCID)
	}
	if string(got.Payload) != string(outPayload) {
		t.Fatalf("result payload got=%s want=%s", string(got.Payload), string(outPayload))
	}
	if !got.Timestamp.Equal(fixedNow.UTC()) {
		t.Fatalf("result timestamp got=%s want=%s", got.Timestamp, fixedNow.UTC())
	}
}

/*
TC-ACTION-SERVICE-003
Type: Negative
Title: Handle rejects nil context
Summary:
Calls Handle with nil context.
Service should fail fast before publishing or executing any action.

Validates:
  - nil context returns error
  - executor is not called
  - no status or result is published
*/
func TestHandleRejectsNilContext(t *testing.T) {
	client := &fakeActionClient{}
	exec := &fakeActionExecutor{}
	svc := newActionServiceForTest(
		t,
		client,
		map[string]Executor{ActionTrace: exec},
		[]string{ActionTrace},
		time.Now,
	)

	err := svc.Handle(nil, agentcore.ActionCommand{Target: "vyos", Action: ActionTrace, RPCID: "rpc-2"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "context is nil") {
		t.Fatalf("error %q does not contain context is nil", err.Error())
	}
	if exec.calls != 0 {
		t.Fatalf("executor calls got=%d want=0", exec.calls)
	}
	if len(client.statuses) != 0 || len(client.results) != 0 {
		t.Fatalf("expected no publish calls, got status=%d result=%d", len(client.statuses), len(client.results))
	}
}

/*
TC-ACTION-SERVICE-004
Type: Negative
Title: Handle fails for canceled context
Summary:
Uses a canceled context with a fake client that enforces context cancellation.
Initial status publish fails and the service returns an aggregated error.

Validates:
  - canceled context returns error
  - error includes context canceled
  - executor is not called
*/
func TestHandleCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := &fakeActionClient{strictContext: true}
	exec := &fakeActionExecutor{}
	svc := newActionServiceForTest(
		t,
		client,
		map[string]Executor{ActionTrace: exec},
		[]string{ActionTrace},
		time.Now,
	)

	err := svc.Handle(ctx, agentcore.ActionCommand{
		Target:  "vyos",
		Action:  ActionTrace,
		RPCID:   "rpc-canceled",
		Payload: json.RawMessage(`{"host":"1.1.1.1"}`),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled in error chain, got %v", err)
	}
	if exec.calls != 0 {
		t.Fatalf("executor calls got=%d want=0", exec.calls)
	}
}

/*
TC-ACTION-SERVICE-005
Type: Negative
Title: Handle fails when action is disabled
Summary:
Submits trace action while enabled list excludes trace.
Service should publish a failure result with disabled_action code.

Validates:
  - disabled_action code is returned
  - executor is not called
  - failed status is published
*/
func TestHandleDisabledAction(t *testing.T) {
	client := &fakeActionClient{}
	exec := &fakeActionExecutor{}
	svc := newActionServiceForTest(
		t,
		client,
		map[string]Executor{ActionTrace: exec},
		[]string{"other"},
		time.Now,
	)

	err := svc.Handle(context.Background(), agentcore.ActionCommand{
		Target:  "vyos",
		Action:  ActionTrace,
		RPCID:   "rpc-disabled",
		Payload: json.RawMessage(`{"host":"1.1.1.1"}`),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(client.results) != 1 {
		t.Fatalf("result count got=%d want=1", len(client.results))
	}
	if client.results[0].ErrorCode != "disabled_action" {
		t.Fatalf("error code got=%q want=%q", client.results[0].ErrorCode, "disabled_action")
	}
	if exec.calls != 0 {
		t.Fatalf("executor calls got=%d want=0", exec.calls)
	}
	if len(client.statuses) == 0 || client.statuses[len(client.statuses)-1].Stage != "failed" {
		t.Fatalf("last status stage got=%q want=%q", client.statuses[len(client.statuses)-1].Stage, "failed")
	}
}

/*
TC-ACTION-SERVICE-006
Type: Negative
Title: Handle fails for unsupported action without executor
Summary:
Enables an action name but omits matching executor from executor map.
Service should emit unsupported_action failure before execute step.

Validates:
  - unsupported_action code is returned
  - executor map miss is surfaced
  - failed status is published
*/
func TestHandleUnsupportedActionWithoutExecutor(t *testing.T) {
	client := &fakeActionClient{}
	exec := &fakeActionExecutor{}
	svc := newActionServiceForTest(
		t,
		client,
		map[string]Executor{ActionTrace: exec},
		[]string{ActionTrace, "ping"},
		time.Now,
	)

	err := svc.Handle(context.Background(), agentcore.ActionCommand{
		Target:  "vyos",
		Action:  "ping",
		RPCID:   "rpc-unsupported",
		Payload: json.RawMessage(`{"target":"8.8.8.8"}`),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(client.results) != 1 {
		t.Fatalf("result count got=%d want=1", len(client.results))
	}
	if client.results[0].ErrorCode != "unsupported_action" {
		t.Fatalf("error code got=%q want=%q", client.results[0].ErrorCode, "unsupported_action")
	}
}

/*
TC-ACTION-SERVICE-007
Type: Negative
Title: Handle maps invalid payload executor errors
Summary:
Injects ErrInvalidActionPayload from executor.
Service should publish failure result with invalid_action_payload.

Validates:
  - invalid_action_payload code is used
  - executor is called once
  - failed status is published
*/
func TestHandleInvalidPayloadErrorMapping(t *testing.T) {
	client := &fakeActionClient{}
	exec := &fakeActionExecutor{err: fmt.Errorf("%w: missing host", ErrInvalidActionPayload)}
	svc := newActionServiceForTest(
		t,
		client,
		map[string]Executor{ActionTrace: exec},
		[]string{ActionTrace},
		time.Now,
	)

	err := svc.Handle(context.Background(), agentcore.ActionCommand{
		Target:  "vyos",
		Action:  ActionTrace,
		RPCID:   "rpc-invalid-payload",
		Payload: json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(client.results) != 1 {
		t.Fatalf("result count got=%d want=1", len(client.results))
	}
	if client.results[0].ErrorCode != "invalid_action_payload" {
		t.Fatalf("error code got=%q want=%q", client.results[0].ErrorCode, "invalid_action_payload")
	}
	if exec.calls != 1 {
		t.Fatalf("executor calls got=%d want=1", exec.calls)
	}
}

/*
TC-ACTION-SERVICE-008
Type: Negative
Title: Handle maps generic executor failures
Summary:
Injects a generic executor error.
Service should publish action_execute_failed failure result.

Validates:
  - action_execute_failed code is used
  - executor is called once
  - failure path publishes final result
*/
func TestHandleExecutorFailureMapping(t *testing.T) {
	client := &fakeActionClient{}
	exec := &fakeActionExecutor{err: errors.New("executor failed")}
	svc := newActionServiceForTest(
		t,
		client,
		map[string]Executor{ActionTrace: exec},
		[]string{ActionTrace},
		time.Now,
	)

	err := svc.Handle(context.Background(), agentcore.ActionCommand{
		Target:  "vyos",
		Action:  ActionTrace,
		RPCID:   "rpc-exec-fail",
		Payload: json.RawMessage(`{"host":"1.1.1.1"}`),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(client.results) != 1 {
		t.Fatalf("result count got=%d want=1", len(client.results))
	}
	if client.results[0].ErrorCode != "action_execute_failed" {
		t.Fatalf("error code got=%q want=%q", client.results[0].ErrorCode, "action_execute_failed")
	}
}

/*
TC-ACTION-SERVICE-009
Type: Negative
Title: Handle reports status publish failures
Summary:
Injects publish failure for executing stage.
Service should fail with status_publish_failed and publish failure result.

Validates:
  - status_publish_failed code is used
  - executor is not called when executing status publish fails
  - failure path still publishes result
*/
func TestHandleStatusPublishFailureBehavior(t *testing.T) {
	client := &fakeActionClient{
		statusErrByStage: map[string]error{
			"executing": errors.New("publish executing failed"),
		},
	}
	exec := &fakeActionExecutor{}
	svc := newActionServiceForTest(
		t,
		client,
		map[string]Executor{ActionTrace: exec},
		[]string{ActionTrace},
		time.Now,
	)

	err := svc.Handle(context.Background(), agentcore.ActionCommand{
		Target:  "vyos",
		Action:  ActionTrace,
		RPCID:   "rpc-status-fail",
		Payload: json.RawMessage(`{"host":"1.1.1.1"}`),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(client.results) != 1 {
		t.Fatalf("result count got=%d want=1", len(client.results))
	}
	if client.results[0].ErrorCode != "status_publish_failed" {
		t.Fatalf("error code got=%q want=%q", client.results[0].ErrorCode, "status_publish_failed")
	}
	if exec.calls != 0 {
		t.Fatalf("executor calls got=%d want=0", exec.calls)
	}
}

/*
TC-ACTION-SERVICE-010
Type: Negative
Title: Handle reports success-result publish failure
Summary:
Injects failure when publishing success result at end of happy path.
Service should enter failure path and emit result_publish_failed.

Validates:
  - executor runs and completed status is emitted
  - result_publish_failed code is used
  - failed status is published after publish failure
*/
func TestHandleSuccessResultPublishFailureBehavior(t *testing.T) {
	client := &fakeActionClient{
		resultErrByKey: map[string]error{
			"success|": errors.New("publish success result failed"),
		},
	}
	exec := &fakeActionExecutor{
		output: Output{
			Message: "ok",
			Payload: json.RawMessage(`{"ok":true}`),
		},
	}
	svc := newActionServiceForTest(
		t,
		client,
		map[string]Executor{ActionTrace: exec},
		[]string{ActionTrace},
		time.Now,
	)

	err := svc.Handle(context.Background(), agentcore.ActionCommand{
		Target:  "vyos",
		Action:  ActionTrace,
		RPCID:   "rpc-result-fail",
		Payload: json.RawMessage(`{"host":"1.1.1.1"}`),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(client.results) != 1 {
		t.Fatalf("result count got=%d want=1", len(client.results))
	}
	if client.results[0].ErrorCode != "result_publish_failed" {
		t.Fatalf("error code got=%q want=%q", client.results[0].ErrorCode, "result_publish_failed")
	}
	if len(client.statuses) == 0 || client.statuses[len(client.statuses)-1].Stage != "failed" {
		t.Fatalf("last status stage got=%q want=%q", client.statuses[len(client.statuses)-1].Stage, "failed")
	}
}
