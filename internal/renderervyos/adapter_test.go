package renderervyos

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/routerarchitects/nats-agent-core/agentcore"
	vyosrenderer "github.com/routerarchitects/olg-renderer-vyos/renderer"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/testutil"
)

type fakeRendererBackend struct {
	input vyosrenderer.Input
	out   vyosrenderer.Output
	err   error
	calls int
}

func (f *fakeRendererBackend) Render(ctx context.Context, input vyosrenderer.Input) (vyosrenderer.Output, error) {
	f.calls++
	f.input = input
	if f.err != nil {
		return vyosrenderer.Output{}, f.err
	}
	return f.out, nil
}

/*
TC-RENDERER-VYOS-001
Type: Positive
Title: Render maps desired config metadata
Summary:
Runs the adapter against a fake renderer backend.
The adapter should map target, config UUID, schema metadata, payload,
and renderer output into the internal renderer shape.

Validates:
  - target and config UUID are mapped
  - schema metadata and unwrapped payload are passed to backend
  - rendered output is mapped back to internal output
*/
func TestRenderMapsDesiredConfigMetadata(t *testing.T) {
	backend := &fakeRendererBackend{
		out: vyosrenderer.Output{
			Target:       "vyos",
			ConfigUUID:   "cfg-1",
			RenderedText: "set interfaces ethernet eth0 address dhcp\n",
		},
	}
	adapter, err := NewWithBackend(backend)
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	desired := desiredWithPayload(`{
		"schema_name": "olg-ucentral",
		"schema_version": "4.2.0",
		"config": {"interfaces": [], "services": {}}
	}`)
	out, err := adapter.Render(context.Background(), desired)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if backend.calls != 1 {
		t.Fatalf("render calls got=%d want=1", backend.calls)
	}
	if backend.input.Target != "vyos" || backend.input.ConfigUUID != "cfg-1" {
		t.Fatalf("identity mapping mismatch: %+v", backend.input)
	}
	if backend.input.SchemaName != "olg-ucentral" || backend.input.SchemaVersion != "4.2.0" {
		t.Fatalf("schema mapping mismatch: %+v", backend.input)
	}
	if string(backend.input.PayloadJSON) != `{"interfaces": [], "services": {}}` {
		t.Fatalf("payload got=%s", string(backend.input.PayloadJSON))
	}
	if out.Target != "vyos" || out.UUID != "cfg-1" || out.Text != backend.out.RenderedText {
		t.Fatalf("output mapping mismatch: %+v", out)
	}
}

/*
TC-RENDERER-VYOS-002
Type: Positive
Title: Build input keeps raw payload
Summary:
Builds renderer input from an unwrapped desired payload.
The adapter should pass the original OLG/uCentral config object
through unchanged when no top-level config wrapper exists.

Validates:
  - raw payload is accepted
  - payload JSON is preserved
*/
func TestBuildInputPassesRawPayloadWhenUnwrapped(t *testing.T) {
	raw := json.RawMessage(`{"interfaces":[],"services":{}}`)
	input, err := BuildInput(desiredWithRawPayload(raw))
	if err != nil {
		t.Fatalf("build input: %v", err)
	}
	if string(input.PayloadJSON) != string(raw) {
		t.Fatalf("payload got=%s want=%s", string(input.PayloadJSON), string(raw))
	}
}

/*
TC-RENDERER-VYOS-003
Type: Positive
Title: Build input unwraps config payload
Summary:
Builds renderer input from a payload wrapped as config.
The adapter should pass only the inner config object to the
external renderer backend.

Validates:
  - top-level config wrapper is recognized
  - inner config object becomes payload JSON
*/
func TestBuildInputUnwrapsConfigPayload(t *testing.T) {
	input, err := BuildInput(desiredWithPayload(`{"config":{"interfaces":[],"services":{}}}`))
	if err != nil {
		t.Fatalf("build input: %v", err)
	}
	if string(input.PayloadJSON) != `{"interfaces":[],"services":{}}` {
		t.Fatalf("payload got=%s", string(input.PayloadJSON))
	}
}

/*
TC-RENDERER-VYOS-004
Type: Positive
Title: Build input does not mutate original payload
Summary:
Builds renderer input and mutates the returned payload buffer.
The original desired config payload should remain unchanged because
the adapter clones JSON bytes before returning them.

Validates:
  - returned payload can be modified by caller
  - original desired payload is unchanged
*/
func TestBuildInputDoesNotMutateOriginalPayload(t *testing.T) {
	raw := json.RawMessage(`{"config":{"interfaces":[]}}`)
	original := string(raw)

	input, err := BuildInput(desiredWithRawPayload(raw))
	if err != nil {
		t.Fatalf("build input: %v", err)
	}
	input.PayloadJSON[0] = '['

	if string(raw) != original {
		t.Fatalf("original payload mutated got=%s want=%s", string(raw), original)
	}
}

/*
TC-RENDERER-VYOS-005
Type: Negative
Title: Render returns wrapped backend errors
Summary:
Runs the adapter with a fake backend that fails rendering.
The adapter should return an error that preserves backend context
and identifies the render stage.

Validates:
  - backend render error is returned
  - error includes vyos render failed context
*/
func TestRenderReturnsWrappedErrors(t *testing.T) {
	backend := &fakeRendererBackend{err: errors.New("boom")}
	adapter, err := NewWithBackend(backend)
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	_, err = adapter.Render(context.Background(), desiredWithPayload(`{"interfaces":[]}`))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "vyos render failed") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("unexpected error: %v", err)
	}
}

/*
TC-RENDERER-VYOS-006
Type: Positive
Title: Command count ignores blank lines
Summary:
Counts rendered command text with blank lines and whitespace.
The helper should count only non-empty command lines for safe
metadata logging.

Validates:
  - non-empty command lines are counted
  - blank and whitespace-only lines are ignored
*/
func TestCountNonEmptyLinesIgnoresBlankLines(t *testing.T) {
	got := countNonEmptyLines("\nset a\n  \nset b\n\t\n")
	if got != 2 {
		t.Fatalf("line count got=%d want=2", got)
	}
}

/*
TC-RENDERER-VYOS-007
Type: Safety
Title: Payload debug logging omits raw payload body
Summary:
Runs the real renderer adapter with payload debug logging enabled and
an input payload that contains a secret value. The adapter should log
payload metadata only and must not emit the raw JSON body.

Validates:
  - render succeeds with payload debug logging enabled
  - logs include payload metadata
  - logs do not include payload_json
  - logs do not include secret payload content
*/
func TestRenderPayloadDebugLoggingOmitsRawPayloadBody(t *testing.T) {
	backend := &fakeRendererBackend{
		out: vyosrenderer.Output{
			Target:       "vyos",
			ConfigUUID:   "cfg-1",
			RenderedText: "set system host-name test\n",
		},
	}
	logs := &testutil.LogCapture{}
	adapter, err := NewWithBackend(
		backend,
		WithLogger(logs),
		WithDebugLogging(DebugLogging{LogPayloads: true}),
	)
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	_, err = adapter.Render(context.Background(), desiredWithPayload(`{"password":"swordfish","interfaces":[],"services":{}}`))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !logs.Contains("payload_size_bytes") {
		t.Fatal("expected payload metadata in logs")
	}
	if logs.Contains("payload_json") {
		t.Fatal("raw payload key leaked to logs")
	}
	if logs.Contains("swordfish") {
		t.Fatal("secret payload content leaked to logs")
	}
}

func desiredWithPayload(payload string) agentcore.StoredDesiredConfig {
	return desiredWithRawPayload(json.RawMessage(payload))
}

func desiredWithRawPayload(payload json.RawMessage) agentcore.StoredDesiredConfig {
	return agentcore.StoredDesiredConfig{
		Record: agentcore.DesiredConfigRecord{
			Target:  "vyos",
			UUID:    "cfg-1",
			Payload: payload,
		},
	}
}
