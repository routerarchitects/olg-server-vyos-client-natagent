package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/*
TC-CONFIG-VALIDATE-001
Type: Positive
Title: Default configure mode is placeholder
Summary:
Loads the default application config.
The configure backend should default to placeholder mode so CI and
local non-VyOS runs remain safe.

Validates:
  - default configure mode is placeholder
  - default config validates successfully
*/
func TestDefaultConfigureModeIsPlaceholder(t *testing.T) {
	cfg := DefaultAppConfig()
	if cfg.Agent.Configure.Mode != "placeholder" {
		t.Fatalf("default configure mode got=%q want=placeholder", cfg.Agent.Configure.Mode)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should validate: %v", err)
	}
}

/*
TC-CONFIG-VALIDATE-002
Type: Positive
Title: Default debug flags are false
Summary:
Loads the default application config.
Full payload, rendered command, and apply plan logging should be
disabled unless explicitly enabled for lab debugging.

Validates:
  - log_payloads defaults to false
  - log_rendered defaults to false
  - log_apply_plan defaults to false
*/
func TestDefaultDebugFlagsAreFalse(t *testing.T) {
	cfg := DefaultAppConfig()
	if cfg.Agent.Debug.LogPayloads {
		t.Fatal("default log_payloads got=true want=false")
	}
	if cfg.Agent.Debug.LogRendered {
		t.Fatal("default log_rendered got=true want=false")
	}
	if cfg.Agent.Debug.LogApplyPlan {
		t.Fatal("default log_apply_plan got=true want=false")
	}
}

/*
TC-CONFIG-VALIDATE-003
Type: Positive
Title: Supported configure modes validate
Summary:
Checks each supported configure backend mode.
Both placeholder and real modes should pass validation because they
are valid runtime choices.

Validates:
  - placeholder configure mode is accepted
  - real configure mode is accepted
*/
func TestValidateConfigureModeAcceptsSupportedValues(t *testing.T) {
	for _, mode := range []string{"placeholder", "real"} {
		t.Run(mode, func(t *testing.T) {
			cfg := DefaultAppConfig()
			cfg.Agent.Configure.Mode = mode
			if err := cfg.Validate(); err != nil {
				t.Fatalf("configure mode %q should validate: %v", mode, err)
			}
		})
	}
}

/*
TC-CONFIG-VALIDATE-004
Type: Negative
Title: Unknown configure mode is rejected
Summary:
Sets configure mode to an unsupported value.
Validation should fail fast with an error that identifies the
invalid configure mode field.

Validates:
  - unknown configure mode returns an error
  - error mentions agent.configure.mode
*/
func TestValidateConfigureModeRejectsUnknownValue(t *testing.T) {
	cfg := DefaultAppConfig()
	cfg.Agent.Configure.Mode = "bogus"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "agent.configure.mode") {
		t.Fatalf("error %q does not mention agent.configure.mode", err.Error())
	}
}

/*
TC-CONFIG-VALIDATE-005
Type: Positive
Title: Save after commit setting is preserved
Summary:
Sets the real apply save flag on the application config.
Validation should not alter the value because this flag controls
the renderer apply engine save behavior.

Validates:
  - save_after_commit true is accepted
  - validation does not mutate save_after_commit
*/
func TestValidatePreservesApplySaveAfterCommit(t *testing.T) {
	cfg := DefaultAppConfig()
	cfg.Agent.Apply.SaveAfterCommit = true

	if err := cfg.Validate(); err != nil {
		t.Fatalf("config should validate: %v", err)
	}
	if !cfg.Agent.Apply.SaveAfterCommit {
		t.Fatal("save_after_commit was not preserved")
	}
}

/*
TC-CONFIG-VALIDATE-006
Type: Positive
Title: YAML loads without legacy backend mode fields
Summary:
Loads a YAML config that omits agent.renderer.mode and agent.apply.mode.
The loader should accept the config because agent.configure.mode is
the only active backend selector.

Validates:
  - config loads without agent.renderer.mode
  - config loads without agent.apply.mode
  - apply save_after_commit value is preserved
*/
func TestLoadConfigWithoutLegacyBackendModeFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`
agent:
  configure:
    mode: real
  apply:
    save_after_commit: true
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Agent.Configure.Mode != "real" {
		t.Fatalf("configure mode got=%q want=real", cfg.Agent.Configure.Mode)
	}
	if !cfg.Agent.Apply.SaveAfterCommit {
		t.Fatal("save_after_commit got=false want=true")
	}
}

/*
TC-CONFIG-VALIDATE-007
Type: Positive
Title: Supported action modes validate
Summary:
Checks each supported action mode.
Both placeholder and real modes should pass validation.

Validates:
  - placeholder action mode is accepted
  - real action mode is accepted
*/
func TestValidateActionModeAcceptsSupportedValues(t *testing.T) {
	for _, mode := range []string{"placeholder", "real"} {
		t.Run(mode, func(t *testing.T) {
			cfg := DefaultAppConfig()
			cfg.Agent.Actions.Mode = mode
			if err := cfg.Validate(); err != nil {
				t.Fatalf("action mode %q should validate: %v", mode, err)
			}
		})
	}
}

/*
TC-CONFIG-VALIDATE-008
Type: Negative
Title: Unknown action mode is rejected
Summary:
Sets action mode to an unsupported value.
Validation should fail fast with an error that identifies the
invalid action mode field.

Validates:
  - unknown action mode returns an error
  - error mentions agent.actions.mode
*/
func TestValidateActionModeRejectsUnknownValue(t *testing.T) {
	cfg := DefaultAppConfig()
	cfg.Agent.Actions.Mode = "bogus"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "agent.actions.mode") {
		t.Fatalf("error %q does not mention agent.actions.mode", err.Error())
	}
}
