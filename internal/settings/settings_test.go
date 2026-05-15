package settings

import (
	"os"
	"path/filepath"
	"testing"

	appRuntime "github.com/rpcarvs/reasond/internal/runtime"
)

func TestLoadReturnsDefaultsWhenSettingsFileIsMissing(t *testing.T) {
	t.Parallel()

	loaded, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("load missing settings: %v", err)
	}
	if loaded.DefaultJudgeProvider != DefaultJudgeProvider || loaded.DefaultJudgeModel != DefaultCodexModel {
		t.Fatalf("unexpected defaults: %+v", loaded)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	saved, err := Save(root, Settings{
		DefaultJudgeProvider: "CLAUDE",
		DefaultJudgeModel:    DefaultClaudeModel,
		GitIgnoreReasond:     false,
	})
	if err != nil {
		t.Fatalf("save settings: %v", err)
	}
	if saved.DefaultJudgeProvider != "claude" {
		t.Fatalf("expected provider normalization, got %+v", saved)
	}
	if saved.GitIgnoreReasond {
		t.Fatalf("expected gitignore preference to round-trip as false")
	}

	loaded, err := Load(root)
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if loaded != saved {
		t.Fatalf("loaded settings mismatch: got %+v want %+v", loaded, saved)
	}

	if _, err := os.Stat(filepath.Join(root, appRuntime.DirectoryName, appRuntime.SettingsFileName)); err != nil {
		t.Fatalf("stat settings file: %v", err)
	}
}

func TestLoadRejectsInvalidSettings(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	settingsDir := filepath.Join(root, appRuntime.DirectoryName)
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("create settings dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(settingsDir, appRuntime.SettingsFileName), []byte(`{"default_judge_provider":"bad","default_judge_model":"x"}`), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	if _, err := Load(root); err == nil {
		t.Fatalf("expected invalid provider error")
	}
}

func TestLoadDefaultsGitIgnorePreferenceWhenFieldMissing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	settingsDir := filepath.Join(root, appRuntime.DirectoryName)
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("create settings dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(settingsDir, appRuntime.SettingsFileName), []byte(`{"default_judge_provider":"codex","default_judge_model":"gpt-5.4-mini"}`), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	loaded, err := Load(root)
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if !loaded.GitIgnoreReasond {
		t.Fatalf("expected missing gitignore field to default to true")
	}
}

func TestValidateRejectsModelForWrongProvider(t *testing.T) {
	t.Parallel()

	if _, err := Validate(Settings{
		DefaultJudgeProvider: "claude",
		DefaultJudgeModel:    DefaultCodexModel,
	}); err == nil {
		t.Fatalf("expected model/provider validation error")
	}
}

func TestValidateAcceptsNonEmptyOllamaModel(t *testing.T) {
	t.Parallel()

	validated, err := Validate(Settings{
		DefaultJudgeProvider: "ollama",
		DefaultJudgeModel:    "glm4:9b",
	})
	if err != nil {
		t.Fatalf("validate ollama settings: %v", err)
	}
	if validated.DefaultJudgeProvider != "ollama" || validated.DefaultJudgeModel != "glm4:9b" {
		t.Fatalf("unexpected validated ollama settings: %+v", validated)
	}
}

func TestModelsForProviderReturnsCopy(t *testing.T) {
	t.Parallel()

	models, err := ModelsForProvider("codex")
	if err != nil {
		t.Fatalf("models for provider: %v", err)
	}
	models[0] = "mutated"

	again, err := ModelsForProvider("codex")
	if err != nil {
		t.Fatalf("models for provider again: %v", err)
	}
	if again[0] == "mutated" {
		t.Fatalf("expected models copy")
	}
}

func TestModelsForProviderRejectsDynamicOllamaCatalog(t *testing.T) {
	t.Parallel()

	if _, err := ModelsForProvider("ollama"); err == nil {
		t.Fatalf("expected ollama static models query to fail")
	}
}
