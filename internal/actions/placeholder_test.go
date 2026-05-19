package actions

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/routerarchitects/nats-agent-core/agentcore"
)

/*
TC-ACTION-PLACEHOLDER-001
Type: Positive
Title: Placeholder trace executor returns deterministic JSON output
Summary:
Executes placeholder trace twice with the same valid command.
Output should be stable, non-empty, and valid JSON with expected fields.

Validates:
  - output payload is non-empty and valid JSON
  - output message is deterministic
  - output payload is deterministic for same input
*/
func TestPlaceholderTraceExecutorReturnsDeterministicJSON(t *testing.T) {
	exec := NewPlaceholderTraceExecutor()
	msg := agentcore.ActionCommand{
		Target:  "vyos",
		Action:  ActionTrace,
		RPCID:   "rpc-trace-1",
		Payload: json.RawMessage(`{"host":"8.8.8.8"}`),
	}

	out1, err := exec.Execute(context.Background(), msg)
	if err != nil {
		t.Fatalf("execute first: %v", err)
	}
	out2, err := exec.Execute(context.Background(), msg)
	if err != nil {
		t.Fatalf("execute second: %v", err)
	}

	if len(out1.Payload) == 0 {
		t.Fatal("payload is empty")
	}
	if !json.Valid(out1.Payload) {
		t.Fatalf("payload is not valid json: %s", string(out1.Payload))
	}
	if out1.Message != out2.Message {
		t.Fatalf("message mismatch first=%q second=%q", out1.Message, out2.Message)
	}
	if string(out1.Payload) != string(out2.Payload) {
		t.Fatalf("payload mismatch first=%s second=%s", string(out1.Payload), string(out2.Payload))
	}

	var got map[string]any
	if err := json.Unmarshal(out1.Payload, &got); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if got["executor"] != "placeholder_trace" {
		t.Fatalf("executor field got=%v want=placeholder_trace", got["executor"])
	}
	if got["action"] != ActionTrace {
		t.Fatalf("action field got=%v want=%s", got["action"], ActionTrace)
	}
	if got["target"] != "vyos" {
		t.Fatalf("target field got=%v want=vyos", got["target"])
	}
}

/*
TC-ACTION-PLACEHOLDER-002
Type: Negative
Title: Placeholder trace executor rejects nil context
Summary:
Calls Execute with nil context.
Executor should fail fast with a clear validation error.

Validates:
  - nil context returns error
  - error mentions context is nil
*/
func TestPlaceholderTraceExecutorRejectsNilContext(t *testing.T) {
	exec := NewPlaceholderTraceExecutor()

	_, err := exec.Execute(nil, agentcore.ActionCommand{
		Target:  "vyos",
		Action:  ActionTrace,
		RPCID:   "rpc-nil-ctx",
		Payload: json.RawMessage(`{"host":"8.8.8.8"}`),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "context is nil") {
		t.Fatalf("error %q does not contain context is nil", err.Error())
	}
}

/*
TC-ACTION-PLACEHOLDER-003
Type: Negative
Title: Placeholder trace executor rejects canceled context
Summary:
Uses a canceled context and calls Execute.
Executor should return context cancellation error.

Validates:
  - canceled context returns error
  - error includes context canceled
*/
func TestPlaceholderTraceExecutorRejectsCanceledContext(t *testing.T) {
	exec := NewPlaceholderTraceExecutor()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := exec.Execute(ctx, agentcore.ActionCommand{
		Target:  "vyos",
		Action:  ActionTrace,
		RPCID:   "rpc-canceled-ctx",
		Payload: json.RawMessage(`{"host":"8.8.8.8"}`),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled in error chain, got %v", err)
	}
}

/*
TC-ACTION-PLACEHOLDER-004
Type: Negative
Title: Placeholder trace executor rejects invalid command fields
Summary:
Covers missing target, missing rpc_id, unsupported action, and invalid payload.
Each invalid input should fail with a non-nil error.

Validates:
  - missing target fails
  - missing rpc_id fails
  - unsupported action fails
  - invalid payload fails with ErrInvalidActionPayload
*/
func TestPlaceholderTraceExecutorRejectsInvalidCommand(t *testing.T) {
	exec := NewPlaceholderTraceExecutor()

	cases := []struct {
		name          string
		msg           agentcore.ActionCommand
		errorContains string
		isErr         error
	}{
		{
			name: "missing target",
			msg: agentcore.ActionCommand{
				Action:  ActionTrace,
				RPCID:   "rpc-missing-target",
				Payload: json.RawMessage(`{"host":"8.8.8.8"}`),
			},
			errorContains: "target is empty",
		},
		{
			name: "missing rpc id",
			msg: agentcore.ActionCommand{
				Target:  "vyos",
				Action:  ActionTrace,
				Payload: json.RawMessage(`{"host":"8.8.8.8"}`),
			},
			errorContains: "rpc id is empty",
		},
		{
			name: "unsupported action",
			msg: agentcore.ActionCommand{
				Target:  "vyos",
				Action:  "ping",
				RPCID:   "rpc-unsupported",
				Payload: json.RawMessage(`{"host":"8.8.8.8"}`),
			},
			errorContains: "unsupported action",
		},
		{
			name: "invalid payload",
			msg: agentcore.ActionCommand{
				Target:  "vyos",
				Action:  ActionTrace,
				RPCID:   "rpc-invalid-payload",
				Payload: json.RawMessage(`not-json`),
			},
			errorContains: ErrInvalidActionPayload.Error(),
			isErr:         ErrInvalidActionPayload,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := exec.Execute(context.Background(), tc.msg)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.errorContains) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.errorContains)
			}
			if tc.isErr != nil && !errors.Is(err, tc.isErr) {
				t.Fatalf("expected error to wrap %v, got %v", tc.isErr, err)
			}
		})
	}
}
