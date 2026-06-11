package renderervyos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/routerarchitects/nats-agent-core/agentcore"
	vyosrenderer "github.com/routerarchitects/olg-renderer-vyos/renderer"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/renderer"
)

type Backend interface {
	Render(ctx context.Context, input vyosrenderer.Input) (vyosrenderer.Output, error)
}

type Adapter struct {
	backend Backend
	logger  agentcore.Logger
	debug   DebugLogging
}

type Option func(*Adapter)

type DebugLogging struct {
	LogPayloads bool
	LogRendered bool
}

func WithLogger(logger agentcore.Logger) Option {
	return func(a *Adapter) {
		a.logger = logger
	}
}

func WithDebugLogging(debug DebugLogging) Option {
	return func(a *Adapter) {
		a.debug = debug
	}
}

func New(opts ...Option) (*Adapter, error) {
	backend, err := vyosrenderer.New()
	if err != nil {
		return nil, fmt.Errorf("create vyos renderer: %w", err)
	}
	return NewWithBackend(backend, opts...)
}

func NewWithBackend(backend Backend, opts ...Option) (*Adapter, error) {
	if backend == nil {
		return nil, errors.New("vyos renderer backend is required")
	}
	a := &Adapter{backend: backend}
	for _, opt := range opts {
		if opt != nil {
			opt(a)
		}
	}
	return a, nil
}

func (a *Adapter) Render(ctx context.Context, desired agentcore.StoredDesiredConfig) (renderer.Output, error) {
	if a == nil || a.backend == nil {
		return renderer.Output{}, errors.New("vyos renderer adapter is not initialized")
	}

	input, err := BuildInput(desired)
	if err != nil {
		return renderer.Output{}, err
	}
	a.logInfo("vyos renderer input prepared",
		"target", input.Target,
		"uuid", input.ConfigUUID,
		"schema_name", input.SchemaName,
		"schema_version", input.SchemaVersion,
		"payload_size_bytes", len(input.PayloadJSON),
	)
	if a.debug.LogPayloads {
		a.logDebug("vyos renderer input payload summary",
			"target", input.Target,
			"uuid", input.ConfigUUID,
			"schema_name", input.SchemaName,
			"schema_version", input.SchemaVersion,
			"payload_size_bytes", len(input.PayloadJSON),
			"payload_body_omitted", true,
		)
	}

	out, err := a.backend.Render(ctx, input)
	if err != nil {
		return renderer.Output{}, fmt.Errorf("vyos render failed: %w", err)
	}
	a.logInfo("vyos renderer output produced",
		"target", out.Target,
		"uuid", out.ConfigUUID,
		"schema_name", out.SchemaName,
		"schema_version", out.SchemaVersion,
		"rendered_size_bytes", len(out.RenderedText),
		"rendered_command_count", countNonEmptyLines(out.RenderedText),
	)
	if a.debug.LogRendered {
		a.logDebug("vyos renderer output commands produced",
			"target", out.Target,
			"uuid", out.ConfigUUID,
			"schema_name", out.SchemaName,
			"schema_version", out.SchemaVersion,
			"rendered_commands", out.RenderedText,
		)
	}

	return renderer.Output{
		Target: out.Target,
		UUID:   out.ConfigUUID,
		Text:   out.RenderedText,
	}, nil
}

func BuildInput(desired agentcore.StoredDesiredConfig) (vyosrenderer.Input, error) {
	payload, schemaName, schemaVersion, err := payloadAndMetadata(desired.Record.Payload)
	if err != nil {
		return vyosrenderer.Input{}, err
	}

	return vyosrenderer.Input{
		Target:        desired.Record.Target,
		ConfigUUID:    desired.Record.UUID,
		SchemaName:    schemaName,
		SchemaVersion: schemaVersion,
		PayloadJSON:   payload,
	}, nil
}

func payloadAndMetadata(raw json.RawMessage) (json.RawMessage, string, string, error) {
	info := vyosrenderer.GetInfo()
	schemaName := info.SupportedSchemaName
	schemaVersion := firstSupportedSchemaVersion(info)
	payload := cloneRaw(raw)

	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, "", "", fmt.Errorf("decode desired payload metadata: %w", err)
	}

	mergeMetadata(root, &schemaName, &schemaVersion)
	if wrapped, ok := root["config"]; ok {
		payload = cloneRaw(wrapped)

		var configRoot map[string]json.RawMessage
		if err := json.Unmarshal(wrapped, &configRoot); err == nil {
			mergeMetadata(configRoot, &schemaName, &schemaVersion)
		}
	}

	return payload, schemaName, schemaVersion, nil
}

func firstSupportedSchemaVersion(info vyosrenderer.Info) string {
	if len(info.SupportedSchemaVersions) == 0 {
		return ""
	}
	return info.SupportedSchemaVersions[0]
}

func mergeMetadata(obj map[string]json.RawMessage, schemaName, schemaVersion *string) {
	if value, ok := stringField(obj, "schema_name"); ok {
		*schemaName = value
	}
	if value, ok := stringField(obj, "schema_version"); ok {
		*schemaVersion = value
	}

	rawSchema, ok := obj["schema"]
	if !ok {
		return
	}
	var schema map[string]json.RawMessage
	if err := json.Unmarshal(rawSchema, &schema); err != nil {
		return
	}
	if value, ok := stringField(schema, "name"); ok {
		*schemaName = value
	}
	if value, ok := stringField(schema, "version"); ok {
		*schemaVersion = value
	}
}

func stringField(obj map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := obj[key]
	if !ok {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func (a *Adapter) logInfo(msg string, kv ...any) {
	if a == nil || a.logger == nil {
		return
	}
	a.logger.Info(msg, kv...)
}

func (a *Adapter) logDebug(msg string, kv ...any) {
	if a == nil || a.logger == nil {
		return
	}
	a.logger.Debug(msg, kv...)
}

func countNonEmptyLines(text string) int {
	count := 0
	inLine := false
	for _, r := range text {
		switch r {
		case '\n', '\r':
			if inLine {
				count++
				inLine = false
			}
		case ' ', '\t':
			continue
		default:
			inLine = true
		}
	}
	if inLine {
		count++
	}
	return count
}
