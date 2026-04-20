package codexconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureHooksEnabledAddsMissingSetting(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()

	result, err := (Manager{HomeDir: homeDir}).EnsureHooksEnabled()
	if err != nil {
		t.Fatalf("ensure hooks enabled: %v", err)
	}
	if result.Status != StatusAdded {
		t.Fatalf("expected status %q, got %q", StatusAdded, result.Status)
	}

	content, err := os.ReadFile(filepath.Join(homeDir, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(content) != "[features]\ncodex_hooks = true\n" {
		t.Fatalf("unexpected config content: %q", string(content))
	}
}

func TestEnsureHooksEnabledLeavesTrueSettingUntouched(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, ".codex")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "config.toml")
	initial := "[features]\ncodex_hooks = true\n"
	if err := os.WriteFile(configPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	result, err := (Manager{HomeDir: homeDir}).EnsureHooksEnabled()
	if err != nil {
		t.Fatalf("ensure hooks enabled: %v", err)
	}
	if result.Status != StatusEnabled {
		t.Fatalf("expected status %q, got %q", StatusEnabled, result.Status)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(content) != initial {
		t.Fatalf("expected config to remain unchanged, got %q", string(content))
	}
}

func TestEnsureHooksEnabledLeavesFalseSettingUntouched(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, ".codex")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "config.toml")
	initial := "[features]\ncodex_hooks = false\n"
	if err := os.WriteFile(configPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	result, err := (Manager{HomeDir: homeDir}).EnsureHooksEnabled()
	if err != nil {
		t.Fatalf("ensure hooks enabled: %v", err)
	}
	if result.Status != StatusBlocked {
		t.Fatalf("expected status %q, got %q", StatusBlocked, result.Status)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(content) != initial {
		t.Fatalf("expected config to remain unchanged, got %q", string(content))
	}
}
