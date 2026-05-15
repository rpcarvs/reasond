package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rpcarvs/reasond/internal/judge"
	appRuntime "github.com/rpcarvs/reasond/internal/runtime"
)

const (
	DefaultJudgeProvider = judge.ProviderCodex
	DefaultCodexModel    = "gpt-5.4-mini"
	DefaultClaudeModel   = "claude-haiku-4-5"
)

// Settings stores repository-local reasond preferences.
type Settings struct {
	// DefaultJudgeProvider is the normalized provider id used by agent-facing
	// judge commands when no provider is chosen interactively.
	DefaultJudgeProvider string `json:"default_judge_provider"`
	// DefaultJudgeModel is the saved default model for DefaultJudgeProvider.
	DefaultJudgeModel string `json:"default_judge_model"`
	// GitIgnoreReasond controls whether reasond-managed runtime paths should be
	// required in the repository .gitignore.
	GitIgnoreReasond bool `json:"git_ignore_reasond"`
}

// Defaults returns the migration-friendly judge settings used when no settings file exists.
func Defaults() Settings {
	defaultModel, _ := judge.DefaultModel(DefaultJudgeProvider)
	return Settings{
		DefaultJudgeProvider: DefaultJudgeProvider,
		DefaultJudgeModel:    defaultModel,
		GitIgnoreReasond:     true,
	}
}

// ModelsForProvider returns the static supported model list for a provider.
// Dynamic providers such as Ollama return an error because their model choices
// are discovered at runtime.
func ModelsForProvider(provider string) ([]string, error) {
	return judge.ModelsForProvider(provider)
}

// AvailableModels returns selectable models for a provider, including
// dynamically discovered local models such as Ollama installs.
func AvailableModels(ctx context.Context, provider string) ([]string, error) {
	return judge.AvailableModels(ctx, provider)
}

// NormalizeProvider validates and canonicalizes a judge provider.
func NormalizeProvider(provider string) (string, error) {
	return judge.NormalizeProvider(provider)
}

// Validate checks that settings reference a supported provider/model combination.
func Validate(input Settings) (Settings, error) {
	provider, err := NormalizeProvider(input.DefaultJudgeProvider)
	if err != nil {
		return Settings{}, err
	}
	model := strings.TrimSpace(input.DefaultJudgeModel)
	if model == "" {
		return Settings{}, fmt.Errorf("default judge model is required")
	}

	dynamicModels, err := judge.SupportsDynamicModels(provider)
	if err != nil {
		return Settings{}, err
	}
	if dynamicModels {
		return Settings{
			DefaultJudgeProvider: provider,
			DefaultJudgeModel:    model,
			GitIgnoreReasond:     input.GitIgnoreReasond,
		}, nil
	}

	models, err := ModelsForProvider(provider)
	if err != nil {
		return Settings{}, err
	}
	if !slices.Contains(models, model) {
		return Settings{}, fmt.Errorf("unsupported %s judge model %q", provider, model)
	}

	return Settings{
		DefaultJudgeProvider: provider,
		DefaultJudgeModel:    model,
		GitIgnoreReasond:     input.GitIgnoreReasond,
	}, nil
}

// Load reads repository-local settings or returns migration-friendly defaults
// when no settings file exists yet.
func Load(rootDir string) (Settings, error) {
	path, err := settingsPath(rootDir)
	if err != nil {
		return Settings{}, err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Defaults(), nil
		}
		return Settings{}, fmt.Errorf("read settings: %w", err)
	}

	type storedSettings struct {
		DefaultJudgeProvider string `json:"default_judge_provider"`
		DefaultJudgeModel    string `json:"default_judge_model"`
		GitIgnoreReasond     *bool  `json:"git_ignore_reasond"`
	}

	var loaded storedSettings
	if err := json.Unmarshal(content, &loaded); err != nil {
		return Settings{}, fmt.Errorf("decode settings: %w", err)
	}

	merged := Defaults()
	if loaded.DefaultJudgeProvider != "" {
		merged.DefaultJudgeProvider = loaded.DefaultJudgeProvider
	}
	if loaded.DefaultJudgeModel != "" {
		merged.DefaultJudgeModel = loaded.DefaultJudgeModel
	}
	if loaded.GitIgnoreReasond != nil {
		merged.GitIgnoreReasond = *loaded.GitIgnoreReasond
	}
	return Validate(merged)
}

// Save validates and writes repository-local settings to .reasond/settings.json.
func Save(rootDir string, input Settings) (Settings, error) {
	validated, err := Validate(input)
	if err != nil {
		return Settings{}, err
	}

	path, err := settingsPath(rootDir)
	if err != nil {
		return Settings{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Settings{}, fmt.Errorf("create settings directory: %w", err)
	}

	content, err := json.MarshalIndent(validated, "", "  ")
	if err != nil {
		return Settings{}, fmt.Errorf("encode settings: %w", err)
	}
	content = append(content, '\n')
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return Settings{}, fmt.Errorf("write settings: %w", err)
	}

	return validated, nil
}

func settingsPath(rootDir string) (string, error) {
	rootDir, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("resolve root dir: %w", err)
	}
	return appRuntime.SettingsPath(rootDir), nil
}
