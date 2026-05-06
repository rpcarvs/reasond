package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rpcarvs/reasond/internal/processing"
	appRuntime "github.com/rpcarvs/reasond/internal/runtime"
)

const (
	DefaultJudgeProvider = processing.ProviderCodex
	DefaultCodexModel    = "gpt-5.4-mini"
	DefaultClaudeModel   = "claude-haiku-4-5"
)

var providerModels = map[string][]string{
	processing.ProviderCodex: {
		DefaultCodexModel,
		"gpt-5.1-codex-mini",
		"gpt-5.3-codex",
		"gpt-5.4",
	},
	processing.ProviderClaude: {
		DefaultClaudeModel,
		"claude-sonnet-4-6",
		"claude-opus-4-6",
	},
}

// Settings stores repository-local reasond preferences.
type Settings struct {
	DefaultJudgeProvider string `json:"default_judge_provider"`
	DefaultJudgeModel    string `json:"default_judge_model"`
}

// Defaults returns the migration-friendly judge settings used when no settings file exists.
func Defaults() Settings {
	return Settings{
		DefaultJudgeProvider: DefaultJudgeProvider,
		DefaultJudgeModel:    DefaultCodexModel,
	}
}

// ModelsForProvider returns supported judge model choices for a provider.
func ModelsForProvider(provider string) ([]string, error) {
	normalized, err := NormalizeProvider(provider)
	if err != nil {
		return nil, err
	}
	return slices.Clone(providerModels[normalized]), nil
}

// NormalizeProvider validates and canonicalizes a judge provider.
func NormalizeProvider(provider string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case processing.ProviderCodex:
		return processing.ProviderCodex, nil
	case processing.ProviderClaude:
		return processing.ProviderClaude, nil
	default:
		return "", fmt.Errorf("unsupported judge provider %q", provider)
	}
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

	models := providerModels[provider]
	if !slices.Contains(models, model) {
		return Settings{}, fmt.Errorf("unsupported %s judge model %q", provider, model)
	}

	return Settings{
		DefaultJudgeProvider: provider,
		DefaultJudgeModel:    model,
	}, nil
}

// Load reads repository-local settings or returns defaults when no file exists.
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

	var loaded Settings
	if err := json.Unmarshal(content, &loaded); err != nil {
		return Settings{}, fmt.Errorf("decode settings: %w", err)
	}
	return Validate(loaded)
}

// Save validates and writes repository-local settings.
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
