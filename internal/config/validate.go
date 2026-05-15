package config

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

func (c AppConfig) Validate() error {
	if strings.TrimSpace(c.Agent.Target) == "" {
		return fmt.Errorf("agent.target is required")
	}
	if strings.TrimSpace(c.Agent.StateFile) == "" {
		return fmt.Errorf("agent.state_file is required")
	}
	if c.Agent.Renderer.Mode != "placeholder" {
		return fmt.Errorf("agent.renderer.mode must be placeholder")
	}
	if c.Agent.Apply.Mode != "placeholder" {
		return fmt.Errorf("agent.apply.mode must be placeholder")
	}
	for _, action := range c.Agent.Actions.Enabled {
		if action != "trace" {
			return fmt.Errorf("agent.actions.enabled contains unsupported action %q", action)
		}
	}

	if len(c.AgentCore.NATS.Servers) == 0 {
		return fmt.Errorf("agentcore.nats.servers must not be empty")
	}
	hasServer := false
	for _, server := range c.AgentCore.NATS.Servers {
		if strings.TrimSpace(server) != "" {
			hasServer = true
			break
		}
	}
	if !hasServer {
		return fmt.Errorf("agentcore.nats.servers must contain at least one non-empty server")
	}
	if c.AgentCore.NATS.RetryOnFailedConnect {
		return fmt.Errorf("agentcore.nats.retry_on_failed_connect is not supported in this phase")
	}
	if c.AgentCore.NATS.MaxReconnects < -1 {
		return fmt.Errorf("agentcore.nats.max_reconnects must be -1 or greater")
	}
	if c.AgentCore.NATS.ReconnectBufSize < 0 {
		return fmt.Errorf("agentcore.nats.reconnect_buf_size must not be negative")
	}

	subjectRules := []struct {
		field        string
		pattern      string
		placeholders int
	}{
		{
			field:        "agentcore.subjects.configure_pattern",
			pattern:      c.AgentCore.Subjects.ConfigurePattern,
			placeholders: 1,
		},
		{
			field:        "agentcore.subjects.action_pattern",
			pattern:      c.AgentCore.Subjects.ActionPattern,
			placeholders: 2,
		},
		{
			field:        "agentcore.subjects.result_pattern",
			pattern:      c.AgentCore.Subjects.ResultPattern,
			placeholders: 1,
		},
		{
			field:        "agentcore.subjects.status_pattern",
			pattern:      c.AgentCore.Subjects.StatusPattern,
			placeholders: 1,
		},
		{
			field:        "agentcore.subjects.health_pattern",
			pattern:      c.AgentCore.Subjects.HealthPattern,
			placeholders: 1,
		},
	}
	for _, rule := range subjectRules {
		if err := validateSubjectPattern(rule.field, rule.pattern, rule.placeholders); err != nil {
			return err
		}
	}

	if strings.TrimSpace(c.AgentCore.KV.Bucket) == "" {
		return fmt.Errorf("agentcore.kv.bucket is required")
	}
	if err := validateKeyPattern(c.AgentCore.KV.KeyPattern); err != nil {
		return err
	}
	if c.AgentCore.KV.History < 1 || c.AgentCore.KV.History > 64 {
		return fmt.Errorf("agentcore.kv.history must be between 1 and 64")
	}
	if c.AgentCore.KV.MaxValueSize < 0 {
		return fmt.Errorf("agentcore.kv.max_value_size must not be negative")
	}
	switch c.AgentCore.KV.Storage {
	case "file", "memory":
	default:
		return fmt.Errorf("agentcore.kv.storage must be either file or memory")
	}
	if c.AgentCore.KV.Replicas < 1 {
		return fmt.Errorf("agentcore.kv.replicas must be at least 1")
	}
	if c.AgentCore.Retry.PublishAttempts < 1 {
		return fmt.Errorf("agentcore.retry.publish_attempts must be at least 1")
	}

	if c.AgentCore.Execution.HandlerMode != "sync" {
		return fmt.Errorf("agentcore.execution.handler_mode must be sync")
	}

	requiredDurations := []struct {
		field string
		value string
	}{
		{field: "agentcore.nats.connect_timeout", value: c.AgentCore.NATS.ConnectTimeout},
		{field: "agentcore.nats.reconnect_wait", value: c.AgentCore.NATS.ReconnectWait},
		{field: "agentcore.jetstream.default_timeout", value: c.AgentCore.JetStream.DefaultTimeout},
		{field: "agentcore.timeouts.publish_timeout", value: c.AgentCore.Timeouts.PublishTimeout},
		{field: "agentcore.timeouts.subscribe_timeout", value: c.AgentCore.Timeouts.SubscribeTimeout},
		{field: "agentcore.timeouts.kv_timeout", value: c.AgentCore.Timeouts.KVTimeout},
		{field: "agentcore.timeouts.shutdown_timeout", value: c.AgentCore.Timeouts.ShutdownTimeout},
		{field: "agentcore.timeouts.handler_warn_after", value: c.AgentCore.Timeouts.HandlerWarnAfter},
		{field: "agentcore.retry.publish_backoff", value: c.AgentCore.Retry.PublishBackoff},
	}
	for _, item := range requiredDurations {
		if _, err := parseDurationField(item.field, item.value, false); err != nil {
			return err
		}
	}

	if _, err := parseDurationField("agentcore.kv.ttl", c.AgentCore.KV.TTL, true); err != nil {
		return err
	}

	return nil
}

func validateSubjectPattern(fieldName string, pattern string, placeholders int) error {
	if pattern == "" {
		return fmt.Errorf("%s is required", fieldName)
	}
	if strings.TrimSpace(pattern) != pattern {
		return fmt.Errorf("%s must not contain leading or trailing whitespace", fieldName)
	}
	if err := validateNoWhitespace(fieldName, pattern); err != nil {
		return err
	}
	if strings.ContainsAny(pattern, "*>") {
		return fmt.Errorf("%s must not contain wildcard tokens * or >", fieldName)
	}
	if err := validateFormatPlaceholders(fieldName, pattern, placeholders); err != nil {
		return err
	}
	return nil
}

func validateKeyPattern(pattern string) error {
	fieldName := "agentcore.kv.key_pattern"
	if pattern == "" {
		return fmt.Errorf("%s is required", fieldName)
	}
	if strings.TrimSpace(pattern) != pattern {
		return fmt.Errorf("%s must not contain leading or trailing whitespace", fieldName)
	}
	if err := validateNoWhitespace(fieldName, pattern); err != nil {
		return err
	}
	if err := validateFormatPlaceholders(fieldName, pattern, 1); err != nil {
		return err
	}
	return nil
}

func validateNoWhitespace(fieldName string, value string) error {
	if strings.IndexFunc(value, unicode.IsSpace) >= 0 {
		return fmt.Errorf("%s must not contain whitespace", fieldName)
	}
	return nil
}

func validateFormatPlaceholders(fieldName, value string, placeholders int) error {
	count := 0
	for i := 0; i < len(value); i++ {
		if value[i] != '%' {
			continue
		}
		if i+1 >= len(value) {
			return fmt.Errorf("%s contains unsupported format directive %%", fieldName)
		}
		next := value[i+1]
		if next != 's' {
			return fmt.Errorf("%s contains unsupported format directive %%%c", fieldName, next)
		}
		count++
		i++
	}
	if count != placeholders {
		return fmt.Errorf("%s must contain exactly %d %%s placeholder(s)", fieldName, placeholders)
	}
	return nil
}

func parseDurationField(fieldName, raw string, optional bool) (time.Duration, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		if optional {
			return 0, nil
		}
		return 0, fmt.Errorf("%s is required", fieldName)
	}

	d, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, fmt.Errorf("%s is not a valid duration: %w", fieldName, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("%s must not be negative", fieldName)
	}
	return d, nil
}
