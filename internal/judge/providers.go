package judge

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

const (
	ProviderOllama = "ollama"
	ProviderCodex  = "codex"
	ProviderClaude = "claude"
)

// ProviderDefinition is one canonical judge-provider definition used by init,
// settings validation, runtime selection, and storage partitioning.
type ProviderDefinition struct {
	// ID is the stable normalized provider id persisted in settings and storage.
	ID string
	// Label is the user-facing provider name shown in prompts and the TUI.
	Label string
	// DefaultModel is the preferred default model when the provider exposes a
	// stable built-in catalog. Dynamic providers may leave this empty.
	DefaultModel string
	// Models is the stable built-in model catalog for static providers.
	Models []string
	// DynamicModels reports whether the provider loads its model catalog at
	// runtime instead of using the Models field.
	DynamicModels bool
	// RunTable is the SQLite table that stores judge batch metadata.
	RunTable string
	// FindingsTable is the SQLite table that stores normalized findings.
	FindingsTable string
}

var providerDefinitions = []ProviderDefinition{
	{
		ID:            ProviderOllama,
		Label:         "Ollama(local)",
		DefaultModel:  "",
		DynamicModels: true,
		RunTable:      "audit_runs_ollama",
		FindingsTable: "audit_findings_ollama",
	},
	{
		ID:            ProviderCodex,
		Label:         "Codex",
		DefaultModel:  "gpt-5.4-mini",
		Models:        []string{"gpt-5.4-mini", "gpt-5.1-codex-mini", "gpt-5.3-codex", "gpt-5.4"},
		RunTable:      "audit_runs_codex",
		FindingsTable: "audit_findings_codex",
	},
	{
		ID:            ProviderClaude,
		Label:         "Claude Code",
		DefaultModel:  "claude-haiku-4-5",
		Models:        []string{"claude-haiku-4-5", "claude-sonnet-4-6", "claude-opus-4-6"},
		RunTable:      "audit_runs_claude",
		FindingsTable: "audit_findings_claude",
	},
}

// Providers returns the canonical judge providers in stable user-facing order.
func Providers() []ProviderDefinition {
	out := make([]ProviderDefinition, 0, len(providerDefinitions))
	for _, provider := range providerDefinitions {
		out = append(out, ProviderDefinition{
			ID:            provider.ID,
			Label:         provider.Label,
			DefaultModel:  provider.DefaultModel,
			Models:        slices.Clone(provider.Models),
			DynamicModels: provider.DynamicModels,
			RunTable:      provider.RunTable,
			FindingsTable: provider.FindingsTable,
		})
	}
	return out
}

// ProviderIDs returns the canonical judge provider ids in stable order.
func ProviderIDs() []string {
	ids := make([]string, 0, len(providerDefinitions))
	for _, provider := range providerDefinitions {
		ids = append(ids, provider.ID)
	}
	return ids
}

// DefaultProvider returns the migration-friendly default judge provider.
func DefaultProvider() string {
	return ProviderCodex
}

// Definition returns one provider definition by normalized id.
func Definition(provider string) (ProviderDefinition, error) {
	normalized := strings.ToLower(strings.TrimSpace(provider))
	if normalized == "" {
		normalized = DefaultProvider()
	}
	for _, candidate := range providerDefinitions {
		if candidate.ID == normalized {
			return ProviderDefinition{
				ID:            candidate.ID,
				Label:         candidate.Label,
				DefaultModel:  candidate.DefaultModel,
				Models:        slices.Clone(candidate.Models),
				DynamicModels: candidate.DynamicModels,
				RunTable:      candidate.RunTable,
				FindingsTable: candidate.FindingsTable,
			}, nil
		}
	}
	return ProviderDefinition{}, fmt.Errorf("unsupported judge provider %q", provider)
}

// NormalizeProvider validates and canonicalizes a judge provider id.
func NormalizeProvider(provider string) (string, error) {
	definition, err := Definition(provider)
	if err != nil {
		return "", err
	}
	return definition.ID, nil
}

// Label returns the user-facing label for one provider.
func Label(provider string) string {
	definition, err := Definition(provider)
	if err != nil {
		return strings.TrimSpace(provider)
	}
	return definition.Label
}

// ModelsForProvider returns the built-in supported model list for one provider.
// Dynamic providers such as Ollama return an error because their selectable
// models come from the current local runtime instead of a fixed catalog.
func ModelsForProvider(provider string) ([]string, error) {
	definition, err := Definition(provider)
	if err != nil {
		return nil, err
	}
	if definition.DynamicModels {
		return nil, fmt.Errorf("%s judge models are discovered dynamically", definition.ID)
	}
	return definition.Models, nil
}

// DefaultModel returns the default model for one provider.
func DefaultModel(provider string) (string, error) {
	definition, err := Definition(provider)
	if err != nil {
		return "", err
	}
	return definition.DefaultModel, nil
}

// SupportsDynamicModels reports whether the provider loads model choices at runtime.
func SupportsDynamicModels(provider string) (bool, error) {
	definition, err := Definition(provider)
	if err != nil {
		return false, err
	}
	return definition.DynamicModels, nil
}

// AvailableModels returns the current selectable models for a provider,
// resolving dynamic local catalogs when needed.
func AvailableModels(ctx context.Context, provider string) ([]string, error) {
	normalized, err := NormalizeProvider(provider)
	if err != nil {
		return nil, err
	}
	switch normalized {
	case ProviderOllama:
		return ListOllamaModels(ctx, "", nil)
	default:
		return ModelsForProvider(normalized)
	}
}
