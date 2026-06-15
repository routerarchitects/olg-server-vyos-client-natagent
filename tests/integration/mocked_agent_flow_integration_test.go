//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/build"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/routerarchitects/nats-agent-core/agentcore"
	vyosapply "github.com/routerarchitects/olg-renderer-vyos/apply"
	vyosrenderer "github.com/routerarchitects/olg-renderer-vyos/renderer"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/actions"
	placeholderapply "github.com/routerarchitects/olg-server-vyos-client-natagent/internal/apply"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/applyvyos"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/configure"
	internalrenderer "github.com/routerarchitects/olg-server-vyos-client-natagent/internal/renderer"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/renderervyos"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/testutil"
)

const mockedIntegrationWireVersion = "1.0"
const mockedIntegrationAsyncTimeout = 6 * time.Second

var mockedIntegrationSequence atomic.Int64

/*
TC-INTEGRATION-001
Type: Integration
Title: Configure flow with mock backend
Summary:
Starts a real local NATS/JetStream server and wires agentcore configure
handler registration to the configure service with fake renderer/apply
dependencies. A configure submission stores desired config in KV,
publishes the notification, invokes the handler, saves state, and emits
status/result messages through NATS.

Validates:
  - real NATS and JetStream KV path is used
  - fake renderer and apply engine are each called once
  - state is saved with the submitted UUID
  - success status/result preserve target, UUID, and RPC ID
*/
func TestIntegrationConfigureFlowWithMockBackend(t *testing.T) {
	url := startTestNATSServer(t)
	cfg := mockedIntegrationCoreConfig(t, url)
	target := mockedIntegrationTarget(t)
	rpcID := "rpc-configure-mock"
	uuid := "cfg-configure-mock"

	probe := newStartedProbe(t, cfg, target)
	worker := newAgentCoreClient(t, cfg, "worker")
	renderer := &testutil.FakeRenderer{}
	apply := &testutil.FakeApplyEngine{}
	stateStore := &testutil.FakeStateStore{}
	registerConfigureWorker(t, worker, target, renderer, apply, stateStore)
	startAgentCoreClient(t, worker)
	controller := newStartedClient(t, cfg, "controller")

	ack := submitConfigure(t, controller, target, rpcID, uuid, json.RawMessage(`{"interfaces":[]}`))
	if ack.Subject != fmt.Sprintf(cfg.Subjects.ConfigurePattern, target) {
		t.Fatalf("configure ack subject got=%q want=%q", ack.Subject, fmt.Sprintf(cfg.Subjects.ConfigurePattern, target))
	}

	statuses := probe.waitStatuses(t, rpcID, "applied")
	result := probe.waitResult(t, rpcID, "success")

	if renderer.Calls() != 1 || apply.Calls() != 1 {
		t.Fatalf("calls got renderer=%d apply=%d want renderer=1 apply=1", renderer.Calls(), apply.Calls())
	}
	if got := stateStore.CurrentState().AppliedUUID; got != uuid {
		t.Fatalf("state applied_uuid got=%q want=%q", got, uuid)
	}
	assertConfigureSuccessResult(t, result, target, rpcID, uuid)
	assertStatusCorrelation(t, statuses, target, rpcID, uuid)
}

/*
TC-INTEGRATION-002
Type: Integration
Title: Real mock mode uses renderer and apply adapters
Summary:
Runs the real VyOS renderer/apply adapter implementations with fake
adapter backends through the real NATS handler path. This proves the
adapter path is invoked without requiring a real VyOS platform.

Validates:
  - renderervyos.Adapter backend is called
  - applyvyos.Adapter backend is called
  - success result is published through NATS
  - placeholder configure engines are not used for this flow
*/
func TestIntegrationRealModeUsesRendererAndApplyAdapters(t *testing.T) {
	url := startTestNATSServer(t)
	cfg := mockedIntegrationCoreConfig(t, url)
	target := mockedIntegrationTarget(t)
	rpcID := "rpc-real-mock"
	uuid := "cfg-real-mock"

	probe := newStartedProbe(t, cfg, target)
	worker := newAgentCoreClient(t, cfg, "worker")
	renderBackend := &integrationRendererBackend{
		out: vyosrenderer.Output{
			Target:       target,
			ConfigUUID:   uuid,
			RenderedText: "set system host-name real-mock\n",
		},
	}
	renderAdapter, err := renderervyos.NewWithBackend(renderBackend)
	if err != nil {
		t.Fatalf("new renderer adapter: %v", err)
	}
	applyBackend := &testutil.FakeApplyBackend{}
	applyAdapter, err := applyvyos.NewWithBackend(applyBackend)
	if err != nil {
		t.Fatalf("new apply adapter: %v", err)
	}
	stateStore := &testutil.FakeStateStore{}
	registerConfigureWorker(t, worker, target, renderAdapter, applyAdapter, stateStore)
	startAgentCoreClient(t, worker)
	controller := newStartedClient(t, cfg, "controller")

	submitConfigure(t, controller, target, rpcID, uuid, json.RawMessage(`{"interfaces":[]}`))
	result := probe.waitResult(t, rpcID, "success")

	if renderBackend.Calls() != 1 {
		t.Fatalf("renderer adapter backend calls got=%d want=1", renderBackend.Calls())
	}
	if applyBackend.ApplyCalls() != 1 {
		t.Fatalf("apply adapter backend calls got=%d want=1", applyBackend.ApplyCalls())
	}
	assertConfigureSuccessResult(t, result, target, rpcID, uuid)
}

/*
TC-INTEGRATION-003
Type: Integration
Title: Placeholder and real mock flow are equivalent
Summary:
Runs one configure flow with placeholder engines and one with real
adapters backed by fakes. Both flows use real NATS, JetStream KV, and
agentcore handler registration.

Validates:
  - both flows publish success result
  - both flows reach applied status
  - both flows save the requested UUID
  - target, UUID, and RPC ID are preserved in visible output
*/
func TestIntegrationPlaceholderAndRealMockFlowAreEquivalent(t *testing.T) {
	url := startTestNATSServer(t)

	placeholder := runConfigureIntegrationFlow(t, url, "placeholder", func(target, uuid string) (configure.Renderer, configure.ApplyEngine) {
		return internalrenderer.NewPlaceholder(), placeholderapply.NewPlaceholder()
	})
	realMock := runConfigureIntegrationFlow(t, url, "realmock", func(target, uuid string) (configure.Renderer, configure.ApplyEngine) {
		renderBackend := &integrationRendererBackend{
			out: vyosrenderer.Output{
				Target:       target,
				ConfigUUID:   uuid,
				RenderedText: "set system host-name real-mock-equivalent\n",
			},
		}
		renderAdapter, err := renderervyos.NewWithBackend(renderBackend)
		if err != nil {
			t.Fatalf("new renderer adapter: %v", err)
		}
		applyAdapter, err := applyvyos.NewWithBackend(&testutil.FakeApplyBackend{})
		if err != nil {
			t.Fatalf("new apply adapter: %v", err)
		}
		return renderAdapter, applyAdapter
	})

	assertEquivalentConfigureOutcome(t, placeholder)
	assertEquivalentConfigureOutcome(t, realMock)
	if placeholder.result.Result != realMock.result.Result {
		t.Fatalf("result mismatch placeholder=%q real_mock=%q", placeholder.result.Result, realMock.result.Result)
	}
	if terminalStage(placeholder.statuses) != terminalStage(realMock.statuses) {
		t.Fatalf("terminal stage mismatch placeholder=%q real_mock=%q", terminalStage(placeholder.statuses), terminalStage(realMock.statuses))
	}
}

/*
TC-INTEGRATION-004
Type: Integration
Title: Configure failure publishes failure status
Summary:
Runs a real NATS configure submission through a fake renderer that
fails. The failure should be visible through NATS status/result messages
and must not checkpoint the requested UUID.

Validates:
  - failure status and result are published through NATS
  - error_code is render_failed
  - apply is not called
  - state is not falsely updated
  - no success result is published
*/
func TestIntegrationConfigureFailurePublishesFailureStatus(t *testing.T) {
	url := startTestNATSServer(t)
	cfg := mockedIntegrationCoreConfig(t, url)
	target := mockedIntegrationTarget(t)
	rpcID := "rpc-configure-failure"
	uuid := "cfg-configure-failure"

	probe := newStartedProbe(t, cfg, target)
	worker := newAgentCoreClient(t, cfg, "worker")
	renderer := &testutil.FakeRenderer{Err: errors.New("renderer failed")}
	apply := &testutil.FakeApplyEngine{}
	stateStore := &testutil.FakeStateStore{}
	registerConfigureWorker(t, worker, target, renderer, apply, stateStore)
	startAgentCoreClient(t, worker)
	controller := newStartedClient(t, cfg, "controller")

	submitConfigure(t, controller, target, rpcID, uuid, json.RawMessage(`{"interfaces":[]}`))
	statuses := probe.waitStatuses(t, rpcID, "failed")
	result := probe.waitResult(t, rpcID, "failure")

	if result.ErrorCode != "render_failed" {
		t.Fatalf("error_code got=%q want=render_failed", result.ErrorCode)
	}
	if apply.Calls() != 0 {
		t.Fatalf("apply calls got=%d want=0", apply.Calls())
	}
	if got := stateStore.CurrentState().AppliedUUID; got == uuid {
		t.Fatalf("state should not checkpoint failed uuid %q", got)
	}
	assertStatusCorrelation(t, statuses, target, rpcID, uuid)
	probe.assertNoResult(t, rpcID, "success")
}

/*
TC-INTEGRATION-005
Type: Integration
Title: Action trace flow with mock executor
Summary:
Runs a real NATS action submission through agentcore action handler
registration and an action service with a fake trace executor.

Validates:
  - fake trace executor is called once
  - status sequence is received, executing, completed
  - success result is published through NATS
  - target, action, and RPC ID are preserved
*/
func TestIntegrationActionTraceFlowWithMockExecutor(t *testing.T) {
	url := startTestNATSServer(t)
	cfg := mockedIntegrationCoreConfig(t, url)
	target := mockedIntegrationTarget(t)
	rpcID := "rpc-action-trace"

	probe := newStartedProbe(t, cfg, target)
	worker := newAgentCoreClient(t, cfg, "worker")
	executor := &integrationActionExecutor{
		output: actions.Output{
			Message: "fake trace completed",
			Payload: json.RawMessage(`{"executor":"fake_trace","ok":true}`),
		},
	}
	registerActionWorker(t, worker, target, executor)
	startAgentCoreClient(t, worker)
	controller := newStartedClient(t, cfg, "controller")

	ack := submitAction(t, controller, target, rpcID)
	if ack.Subject != fmt.Sprintf(cfg.Subjects.ActionPattern, target, actions.ActionTrace) {
		t.Fatalf("action ack subject got=%q want=%q", ack.Subject, fmt.Sprintf(cfg.Subjects.ActionPattern, target, actions.ActionTrace))
	}

	statuses := probe.waitStatuses(t, rpcID, "completed")
	result := probe.waitResult(t, rpcID, "success")

	if executor.Calls() != 1 {
		t.Fatalf("executor calls got=%d want=1", executor.Calls())
	}
	assertStages(t, statuses, []string{"received", "executing", "completed"})
	if result.CommandType != "action" || result.Action != actions.ActionTrace {
		t.Fatalf("result metadata mismatch: %+v", result)
	}
	if result.Target != target || result.RPCID != rpcID {
		t.Fatalf("result correlation mismatch: %+v", result)
	}
}

/*
TC-INTEGRATION-006
Type: Integration
Title: Status and result subjects receive expected messages
Summary:
Subscribes directly to the configured NATS status/result subjects while
a configure flow runs through agentcore. This verifies the externally
visible subject contract and envelope fields.

Validates:
  - messages arrive on expected status/result subjects
  - status envelope includes version, target, UUID, RPC ID, status, stage, timestamp
  - result envelope includes version, target, UUID, RPC ID, command_type, result, timestamp
*/
func TestIntegrationStatusResultSubjectsReceiveExpectedMessages(t *testing.T) {
	url := startTestNATSServer(t)
	cfg := mockedIntegrationCoreConfig(t, url)
	target := mockedIntegrationTarget(t)
	rpcID := "rpc-subject-contract"
	uuid := "cfg-subject-contract"

	raw := subscribeRawStatusAndResult(t, url, cfg, target)
	worker := newAgentCoreClient(t, cfg, "worker")
	registerConfigureWorker(t, worker, target, &testutil.FakeRenderer{}, &testutil.FakeApplyEngine{}, &testutil.FakeStateStore{})
	startAgentCoreClient(t, worker)
	controller := newStartedClient(t, cfg, "controller")

	submitConfigure(t, controller, target, rpcID, uuid, json.RawMessage(`{"interfaces":[]}`))

	statusMsg := raw.waitStatus(t, "applied")
	resultMsg := raw.waitResult(t, "success")

	wantStatusSubject := fmt.Sprintf(cfg.Subjects.StatusPattern, target)
	if statusMsg.subject != wantStatusSubject {
		t.Fatalf("status subject got=%q want=%q", statusMsg.subject, wantStatusSubject)
	}
	if statusMsg.status.Version == "" || statusMsg.status.RPCID != rpcID || statusMsg.status.Target != target || statusMsg.status.UUID != uuid || statusMsg.status.Stage == "" || statusMsg.status.Timestamp.IsZero() {
		t.Fatalf("status envelope missing expected fields: %+v", statusMsg.status)
	}

	wantResultSubject := fmt.Sprintf(cfg.Subjects.ResultPattern, target)
	if resultMsg.subject != wantResultSubject {
		t.Fatalf("result subject got=%q want=%q", resultMsg.subject, wantResultSubject)
	}
	if resultMsg.result.Version == "" || resultMsg.result.RPCID != rpcID || resultMsg.result.Target != target || resultMsg.result.UUID != uuid || resultMsg.result.CommandType != "configure" || resultMsg.result.Result != "success" || resultMsg.result.Timestamp.IsZero() {
		t.Fatalf("result envelope missing expected fields: %+v", resultMsg.result)
	}
}

/*
TC-INTEGRATION-007
Type: Integration
Title: Configure reads desired config from KV
Summary:
Submits configure through agentcore so the desired config is stored in
JetStream KV and the configure notification acts only as a trigger.
The fake renderer should receive the desired payload loaded from KV.

Validates:
  - desired payload is read from KV by the handler path
  - renderer input payload equals the KV-stored payload
  - configure succeeds through NATS
*/
func TestIntegrationConfigureReadsDesiredConfigFromKV(t *testing.T) {
	url := startTestNATSServer(t)
	cfg := mockedIntegrationCoreConfig(t, url)
	target := mockedIntegrationTarget(t)
	rpcID := "rpc-kv-read"
	uuid := "cfg-kv-read"
	payload := json.RawMessage(`{"interfaces":[{"name":"eth0","role":"wan"}],"services":{"ssh":true}}`)

	probe := newStartedProbe(t, cfg, target)
	worker := newAgentCoreClient(t, cfg, "worker")
	renderer := &testutil.FakeRenderer{}
	registerConfigureWorker(t, worker, target, renderer, &testutil.FakeApplyEngine{}, &testutil.FakeStateStore{})
	startAgentCoreClient(t, worker)
	controller := newStartedClient(t, cfg, "controller")

	submitConfigure(t, controller, target, rpcID, uuid, payload)
	probe.waitResult(t, rpcID, "success")

	input, ok := renderer.LastInput()
	if !ok {
		t.Fatal("expected renderer input")
	}
	if string(input.Record.Payload) != string(payload) {
		t.Fatalf("renderer payload got=%s want=%s", string(input.Record.Payload), string(payload))
	}
}

/*
TC-INTEGRATION-008
Type: Integration
Title: Missing desired config publishes failure
Summary:
Publishes a configure notification directly without creating a matching
KV record. The real handler path should attempt to load desired config,
publish failure, and stop before apply.

Validates:
  - failure status and result are received through NATS
  - current load failure code is visible
  - apply is not called
  - success result is not published
*/
func TestIntegrationMissingDesiredConfigPublishesFailure(t *testing.T) {
	url := startTestNATSServer(t)
	cfg := mockedIntegrationCoreConfig(t, url)
	target := mockedIntegrationTarget(t)
	rpcID := "rpc-missing-desired"
	uuid := "cfg-missing-desired"

	probe := newStartedProbe(t, cfg, target)
	worker := newAgentCoreClient(t, cfg, "worker")
	apply := &testutil.FakeApplyEngine{}
	registerConfigureWorker(t, worker, target, &testutil.FakeRenderer{}, apply, &testutil.FakeStateStore{})
	startAgentCoreClient(t, worker)

	publishConfigureNotification(t, url, cfg, target, rpcID, uuid)
	probe.waitStatuses(t, rpcID, "failed")
	result := probe.waitResult(t, rpcID, "failure")

	if result.ErrorCode != "load_desired_failed" && result.ErrorCode != "desired_config_missing" {
		t.Fatalf("error_code got=%q want load_desired_failed or desired_config_missing", result.ErrorCode)
	}
	if apply.Calls() != 0 {
		t.Fatalf("apply calls got=%d want=0", apply.Calls())
	}
	probe.assertNoResult(t, rpcID, "success")
}

/*
TC-INTEGRATION-009
Type: Recovery
Title: Agent core connection failure handled
Summary:
Attempts to start an agentcore client against an unused local NATS port.
The operation should return a clear startup error within the context
deadline instead of hanging.

Validates:
  - startup returns error for unavailable NATS
  - error is returned before test timeout
*/
func TestIntegrationAgentCoreConnectionFailureHandled(t *testing.T) {
	listener, url := reserveUnavailableNATSURL(t)
	defer listener.Close()

	cfg := mockedIntegrationCoreConfig(t, url)
	cfg.NATS.ConnectTimeout = 100 * time.Millisecond
	cfg.NATS.RetryOnFailedConnect = false
	cfg.NATS.MaxReconnects = 0

	client := newAgentCoreClient(t, cfg, "connection-failure")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := client.Start(ctx)
	if err == nil {
		t.Fatal("expected connection error, got nil")
	}
}

type configureOutcome struct {
	statuses []agentcore.StatusEnvelope
	result   agentcore.ResultEnvelope
	state    string
	target   string
	uuid     string
	rpcID    string
}

type configureEnginesFactory func(target, uuid string) (configure.Renderer, configure.ApplyEngine)

func runConfigureIntegrationFlow(t *testing.T, url, label string, factory configureEnginesFactory) configureOutcome {
	t.Helper()

	cfg := mockedIntegrationCoreConfig(t, url)
	target := mockedIntegrationTarget(t)
	rpcID := "rpc-" + label
	uuid := "cfg-" + label

	probe := newStartedProbe(t, cfg, target)
	worker := newAgentCoreClient(t, cfg, "worker-"+label)
	renderer, applier := factory(target, uuid)
	stateStore := &testutil.FakeStateStore{}
	registerConfigureWorker(t, worker, target, renderer, applier, stateStore)
	startAgentCoreClient(t, worker)
	controller := newStartedClient(t, cfg, "controller-"+label)

	submitConfigure(t, controller, target, rpcID, uuid, json.RawMessage(`{"interfaces":[]}`))
	statuses := probe.waitStatuses(t, rpcID, "applied")
	result := probe.waitResult(t, rpcID, "success")

	return configureOutcome{
		statuses: statuses,
		result:   result,
		state:    stateStore.CurrentState().AppliedUUID,
		target:   target,
		uuid:     uuid,
		rpcID:    rpcID,
	}
}

func assertEquivalentConfigureOutcome(t *testing.T, out configureOutcome) {
	t.Helper()

	assertConfigureSuccessResult(t, out.result, out.target, out.rpcID, out.uuid)
	if terminalStage(out.statuses) != "applied" {
		t.Fatalf("terminal stage got=%q want=applied", terminalStage(out.statuses))
	}
	if out.state != out.uuid {
		t.Fatalf("state got=%q want=%q", out.state, out.uuid)
	}
	assertStatusCorrelation(t, out.statuses, out.target, out.rpcID, out.uuid)
}

type integrationProbe struct {
	statuses chan agentcore.StatusEnvelope
	results  chan agentcore.ResultEnvelope
	client   *agentcore.Client
}

func newStartedProbe(t *testing.T, cfg agentcore.Config, target string) *integrationProbe {
	t.Helper()

	client := newAgentCoreClient(t, cfg, "probe")
	probe := &integrationProbe{
		statuses: make(chan agentcore.StatusEnvelope, 32),
		results:  make(chan agentcore.ResultEnvelope, 16),
		client:   client,
	}
	if err := client.RegisterStatusHandler(target, func(ctx context.Context, msg agentcore.StatusEnvelope) error {
		probe.statuses <- msg
		return nil
	}); err != nil {
		t.Fatalf("register status handler: %v", err)
	}
	if err := client.RegisterResultHandler(target, func(ctx context.Context, msg agentcore.ResultEnvelope) error {
		probe.results <- msg
		return nil
	}); err != nil {
		t.Fatalf("register result handler: %v", err)
	}
	startAgentCoreClient(t, client)
	return probe
}

func (p *integrationProbe) waitStatuses(t *testing.T, rpcID, terminalStage string) []agentcore.StatusEnvelope {
	t.Helper()

	var seen []agentcore.StatusEnvelope
	deadline := time.After(mockedIntegrationAsyncTimeout)
	for {
		select {
		case status := <-p.statuses:
			if status.RPCID != rpcID {
				continue
			}
			seen = append(seen, status)
			if status.Stage == terminalStage {
				return seen
			}
		case <-deadline:
			t.Fatalf("timed out waiting for status stage %q for rpc_id %q; seen=%+v", terminalStage, rpcID, seen)
		}
	}
}

func (p *integrationProbe) waitResult(t *testing.T, rpcID, result string) agentcore.ResultEnvelope {
	t.Helper()

	var seen []agentcore.ResultEnvelope
	deadline := time.After(mockedIntegrationAsyncTimeout)
	for {
		select {
		case got := <-p.results:
			if got.RPCID != rpcID {
				continue
			}
			seen = append(seen, got)
			if got.Result == result {
				return got
			}
		case <-deadline:
			t.Fatalf("timed out waiting for result %q for rpc_id %q; seen=%+v", result, rpcID, seen)
		}
	}
}

func (p *integrationProbe) assertNoResult(t *testing.T, rpcID, result string) {
	t.Helper()

	timer := time.NewTimer(mockedIntegrationAsyncTimeout)
	defer timer.Stop()
	for {
		select {
		case got := <-p.results:
			if got.RPCID == rpcID && got.Result == result {
				t.Fatalf("unexpected result %q for rpc_id %q: %+v", result, rpcID, got)
			}
		case <-timer.C:
			return
		}
	}
}

func registerConfigureWorker(t *testing.T, client *agentcore.Client, target string, renderer configure.Renderer, applier configure.ApplyEngine, stateStore configure.StateStore) *configure.Service {
	t.Helper()

	svc, err := configure.NewService(configure.Dependencies{
		Client:      client,
		StateStore:  stateStore,
		Renderer:    renderer,
		ApplyEngine: applier,
		Now:         mockedIntegrationNow,
	})
	if err != nil {
		t.Fatalf("new configure service: %v", err)
	}
	if err := client.RegisterConfigureHandler(target, svc.Handle); err != nil {
		t.Fatalf("register configure handler: %v", err)
	}
	return svc
}

func registerActionWorker(t *testing.T, client *agentcore.Client, target string, executor actions.Executor) *actions.Service {
	t.Helper()

	svc, err := actions.NewService(actions.Dependencies{
		Client: client,
		Enabled: []string{
			actions.ActionTrace,
		},
		Executors: map[string]actions.Executor{
			actions.ActionTrace: executor,
		},
		Now: mockedIntegrationNow,
	})
	if err != nil {
		t.Fatalf("new action service: %v", err)
	}
	if err := client.RegisterActionHandler(target, actions.ActionTrace, svc.Handle); err != nil {
		t.Fatalf("register action handler: %v", err)
	}
	return svc
}

func submitConfigure(t *testing.T, client *agentcore.Client, target, rpcID, uuid string, payload json.RawMessage) *agentcore.SubmissionAck {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	ack, err := client.SubmitConfigure(ctx, agentcore.ConfigureCommand{
		Version:   mockedIntegrationWireVersion,
		RPCID:     rpcID,
		Target:    target,
		UUID:      uuid,
		Payload:   payload,
		Timestamp: mockedIntegrationNow(),
	})
	if err != nil {
		t.Fatalf("submit configure: %v", err)
	}
	return ack
}

func submitAction(t *testing.T, client *agentcore.Client, target, rpcID string) *agentcore.SubmissionAck {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	ack, err := client.SubmitAction(ctx, agentcore.ActionCommand{
		Version:     mockedIntegrationWireVersion,
		RPCID:       rpcID,
		Target:      target,
		CommandType: "action",
		Action:      actions.ActionTrace,
		Payload:     json.RawMessage(`{"host":"8.8.8.8"}`),
		Timestamp:   mockedIntegrationNow(),
	})
	if err != nil {
		t.Fatalf("submit action: %v", err)
	}
	return ack
}

func publishConfigureNotification(t *testing.T, url string, cfg agentcore.Config, target, rpcID, uuid string) {
	t.Helper()

	nc, err := nats.Connect(url, nats.Name("mocked-integration-missing-desired-publisher"), nats.NoReconnect())
	if err != nil {
		t.Fatalf("connect direct nats: %v", err)
	}
	defer nc.Close()

	msg := agentcore.ConfigureNotification{
		Version:     mockedIntegrationWireVersion,
		RPCID:       rpcID,
		Target:      target,
		CommandType: "configure",
		UUID:        uuid,
		KVBucket:    cfg.KV.Bucket,
		KVKey:       fmt.Sprintf(cfg.KV.KeyPattern, target),
		Timestamp:   mockedIntegrationNow(),
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal configure notification: %v", err)
	}
	if err := nc.Publish(fmt.Sprintf(cfg.Subjects.ConfigurePattern, target), payload); err != nil {
		t.Fatalf("publish configure notification: %v", err)
	}
	if err := nc.FlushTimeout(2 * time.Second); err != nil {
		t.Fatalf("flush configure notification: %v", err)
	}
}

type rawObserver struct {
	statuses chan rawStatus
	results  chan rawResult
}

type rawStatus struct {
	subject string
	status  agentcore.StatusEnvelope
}

type rawResult struct {
	subject string
	result  agentcore.ResultEnvelope
}

func subscribeRawStatusAndResult(t *testing.T, url string, cfg agentcore.Config, target string) *rawObserver {
	t.Helper()

	nc, err := nats.Connect(url, nats.Name("mocked-integration-raw-observer"), nats.NoReconnect())
	if err != nil {
		t.Fatalf("connect direct nats: %v", err)
	}
	t.Cleanup(nc.Close)

	observer := &rawObserver{
		statuses: make(chan rawStatus, 16),
		results:  make(chan rawResult, 8),
	}
	if _, err := nc.Subscribe(fmt.Sprintf(cfg.Subjects.StatusPattern, target), func(msg *nats.Msg) {
		var envelope agentcore.StatusEnvelope
		if err := json.Unmarshal(msg.Data, &envelope); err == nil {
			observer.statuses <- rawStatus{subject: msg.Subject, status: envelope}
		}
	}); err != nil {
		t.Fatalf("subscribe raw status: %v", err)
	}
	if _, err := nc.Subscribe(fmt.Sprintf(cfg.Subjects.ResultPattern, target), func(msg *nats.Msg) {
		var envelope agentcore.ResultEnvelope
		if err := json.Unmarshal(msg.Data, &envelope); err == nil {
			observer.results <- rawResult{subject: msg.Subject, result: envelope}
		}
	}); err != nil {
		t.Fatalf("subscribe raw result: %v", err)
	}
	if err := nc.FlushTimeout(2 * time.Second); err != nil {
		t.Fatalf("flush raw subscriptions: %v", err)
	}
	return observer
}

func (o *rawObserver) waitStatus(t *testing.T, stage string) rawStatus {
	t.Helper()

	deadline := time.After(mockedIntegrationAsyncTimeout)
	for {
		select {
		case msg := <-o.statuses:
			if msg.status.Stage == stage {
				return msg
			}
		case <-deadline:
			t.Fatalf("timed out waiting for raw status stage %q", stage)
		}
	}
}

func (o *rawObserver) waitResult(t *testing.T, result string) rawResult {
	t.Helper()

	deadline := time.After(mockedIntegrationAsyncTimeout)
	for {
		select {
		case msg := <-o.results:
			if msg.result.Result == result {
				return msg
			}
		case <-deadline:
			t.Fatalf("timed out waiting for raw result %q", result)
		}
	}
}

type integrationRendererBackend struct {
	out vyosrenderer.Output
	err error

	mu     sync.Mutex
	calls  int
	inputs []vyosrenderer.Input
}

func (b *integrationRendererBackend) Render(ctx context.Context, input vyosrenderer.Input) (vyosrenderer.Output, error) {
	b.mu.Lock()
	b.calls++
	b.inputs = append(b.inputs, input)
	out := b.out
	err := b.err
	b.mu.Unlock()

	if err != nil {
		return vyosrenderer.Output{}, err
	}
	return out, nil
}

func (b *integrationRendererBackend) Calls() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

type integrationActionExecutor struct {
	output actions.Output
	err    error

	mu     sync.Mutex
	calls  int
	inputs []agentcore.ActionCommand
}

func (e *integrationActionExecutor) Execute(ctx context.Context, msg agentcore.ActionCommand) (actions.Output, error) {
	e.mu.Lock()
	e.calls++
	e.inputs = append(e.inputs, msg)
	out := e.output
	err := e.err
	e.mu.Unlock()

	if err != nil {
		return actions.Output{}, err
	}
	return out, nil
}

func (e *integrationActionExecutor) Calls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func startTestNATSServer(t *testing.T) string {
	t.Helper()

	bin, err := resolveNATSServerBinary()
	if err != nil {
		t.Fatalf("nats-server binary is required for integration tests: %v", err)
	}

	dataDir := t.TempDir()
	clientPortPattern := regexp.MustCompile(`Listening for client connections on .*:(\d+)`)
	attemptLogs := make([]string, 0, 2)
	for _, portArg := range []string{"0", "-1"} {
		var logs bytes.Buffer
		cmd := exec.Command(bin, "-js", "-a", "127.0.0.1", "-p", portArg, "-sd", dataDir)
		cmd.Stdout = &logs
		cmd.Stderr = &logs
		if err := cmd.Start(); err != nil {
			t.Fatalf("start nats-server: %v", err)
		}

		var (
			waitErr error
			waitMu  sync.Mutex
			exited  = make(chan struct{})
		)
		go func() {
			err := cmd.Wait()
			waitMu.Lock()
			waitErr = err
			waitMu.Unlock()
			close(exited)
		}()

		stop := func() {
			if cmd.Process == nil {
				return
			}
			_ = cmd.Process.Signal(os.Interrupt)
			select {
			case <-exited:
			case <-time.After(2 * time.Second):
				_ = cmd.Process.Kill()
				<-exited
			}
		}

		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case <-exited:
				waitMu.Lock()
				err := waitErr
				waitMu.Unlock()
				attemptLogs = append(attemptLogs, fmt.Sprintf("-p %s exited before ready: %v%s\nlogs:\n%s", portArg, err, natsPortSelectionHint(portArg, logs.String()), logs.String()))
				goto nextCandidate
			default:
			}

			matches := clientPortPattern.FindStringSubmatch(logs.String())
			if len(matches) != 2 {
				time.Sleep(50 * time.Millisecond)
				continue
			}

			if portArg == "0" && matches[1] == "4222" {
				attemptLogs = append(attemptLogs, fmt.Sprintf("-p %s did not produce an OS-assigned port%s\nlogs:\n%s", portArg, natsPortSelectionHint(portArg, logs.String()), logs.String()))
				stop()
				goto nextCandidate
			}

			url := "nats://127.0.0.1:" + matches[1]
			nc, err := nats.Connect(url, nats.Name("mocked-integration-ready-check"), nats.NoReconnect(), nats.Timeout(200*time.Millisecond))
			if err == nil {
				nc.Close()
				t.Cleanup(stop)
				return url
			}
			time.Sleep(50 * time.Millisecond)
		}

		attemptLogs = append(attemptLogs, fmt.Sprintf("-p %s did not become ready%s\nlogs:\n%s", portArg, natsPortSelectionHint(portArg, logs.String()), logs.String()))
		stop()

	nextCandidate:
	}

	t.Fatalf("nats-server did not become ready with an OS-assigned client port after trying -p 0 and -p -1\n%s", strings.Join(attemptLogs, "\n\n"))
	return ""
}

func newStartedClient(t *testing.T, cfg agentcore.Config, name string) *agentcore.Client {
	t.Helper()

	client := newAgentCoreClient(t, cfg, name)
	startAgentCoreClient(t, client)
	return client
}

func newAgentCoreClient(t *testing.T, cfg agentcore.Config, name string) *agentcore.Client {
	t.Helper()

	cfg.AgentName = "mocked-integration-" + name
	cfg.NATS.ClientName = "mocked-integration-" + name
	client, err := agentcore.New(cfg)
	if err != nil {
		t.Fatalf("new agentcore client %q: %v", name, err)
	}
	return client
}

func startAgentCoreClient(t *testing.T, client *agentcore.Client) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("start agentcore client: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = client.Close(ctx)
	})
}

func mockedIntegrationCoreConfig(t *testing.T, url string) agentcore.Config {
	t.Helper()

	id := nextMockedIntegrationID()
	return agentcore.Config{
		AgentName: "mocked-integration-agent",
		Version:   mockedIntegrationWireVersion,
		NATS: agentcore.NATSConfig{
			Servers:              []string{url},
			ClientName:           "mocked-integration-client",
			ConnectTimeout:       1 * time.Second,
			RetryOnFailedConnect: false,
			MaxReconnects:        0,
			ReconnectWait:        100 * time.Millisecond,
		},
		JetStream: agentcore.JetStreamConfig{
			DefaultTimeout: 2 * time.Second,
		},
		Subjects: agentcore.SubjectConfig{
			ConfigurePattern: id + ".cmd.configure.%s",
			ActionPattern:    id + ".cmd.action.%s.%s",
			ResultPattern:    id + ".result.%s",
			StatusPattern:    id + ".status.%s",
			HealthPattern:    id + ".health.%s",
		},
		KV: agentcore.KVConfig{
			Bucket:           "cfg_" + id,
			KeyPattern:       "desired.%s",
			AutoCreateBucket: true,
			History:          1,
			Storage:          "file",
			Replicas:         1,
		},
		Timeouts: agentcore.TimeoutConfig{
			PublishTimeout:   2 * time.Second,
			SubscribeTimeout: 2 * time.Second,
			KVTimeout:        2 * time.Second,
			ShutdownTimeout:  2 * time.Second,
			HandlerWarnAfter: 500 * time.Millisecond,
		},
		Retry: agentcore.RetryConfig{
			PublishAttempts: 1,
			PublishBackoff:  50 * time.Millisecond,
		},
		Execution: agentcore.ExecutionConfig{
			HandlerMode: "sync",
		},
	}
}

func mockedIntegrationTarget(t *testing.T) string {
	t.Helper()
	return "vyos_" + nextMockedIntegrationID()
}

func reserveUnavailableNATSURL(t *testing.T) (net.Listener, string) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for reserved unavailable nats url: %v", err)
	}

	return listener, "nats://" + listener.Addr().String()
}

func resolveNATSServerBinary() (string, error) {
	if gopathBin := filepath.Join(build.Default.GOPATH, "bin", "nats-server"); build.Default.GOPATH != "" {
		if info, err := os.Stat(gopathBin); err == nil && !info.IsDir() {
			return gopathBin, nil
		}
	}
	return exec.LookPath("nats-server")
}

func natsPortSelectionHint(portArg, logs string) string {
	if portArg == "0" && strings.Contains(logs, "127.0.0.1:4222") {
		return "; requested -p 0 but this nats-server binary still tried 127.0.0.1:4222, so the helper is retrying with -p -1 for compatibility"
	}
	return ""
}

func nextMockedIntegrationID() string {
	return "p7" + strconv.FormatInt(mockedIntegrationSequence.Add(1), 10)
}

func mockedIntegrationNow() time.Time {
	return time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)
}

func assertConfigureSuccessResult(t *testing.T, result agentcore.ResultEnvelope, target, rpcID, uuid string) {
	t.Helper()

	if result.Result != "success" || result.CommandType != "configure" {
		t.Fatalf("result metadata mismatch: %+v", result)
	}
	if result.Target != target || result.RPCID != rpcID || result.UUID != uuid {
		t.Fatalf("result correlation mismatch got target=%q rpc_id=%q uuid=%q want target=%q rpc_id=%q uuid=%q", result.Target, result.RPCID, result.UUID, target, rpcID, uuid)
	}
}

func assertStatusCorrelation(t *testing.T, statuses []agentcore.StatusEnvelope, target, rpcID, uuid string) {
	t.Helper()

	if len(statuses) == 0 {
		t.Fatal("expected statuses")
	}
	for _, status := range statuses {
		if status.Target != target || status.RPCID != rpcID || status.UUID != uuid {
			t.Fatalf("status correlation mismatch: %+v", status)
		}
	}
}

func assertStages(t *testing.T, statuses []agentcore.StatusEnvelope, want []string) {
	t.Helper()

	got := make([]string, 0, len(statuses))
	for _, status := range statuses {
		got = append(got, status.Stage)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("stages got=%v want=%v", got, want)
	}
}

func terminalStage(statuses []agentcore.StatusEnvelope) string {
	if len(statuses) == 0 {
		return ""
	}
	return statuses[len(statuses)-1].Stage
}

var _ applyvyos.Backend = (*testutil.FakeApplyBackend)(nil)
var _ applyvyos.Preparer = (*testutil.FakeApplyBackend)(nil)
var _ renderervyos.Backend = (*integrationRendererBackend)(nil)
var _ actions.Executor = (*integrationActionExecutor)(nil)
var _ configure.ApplyEngine = (*applyvyos.Adapter)(nil)
var _ configure.Renderer = (*renderervyos.Adapter)(nil)
var _ configure.ApplyEngine = (*placeholderapply.Placeholder)(nil)
var _ configure.Renderer = (*internalrenderer.Placeholder)(nil)
var _ = vyosapply.Plan{}
