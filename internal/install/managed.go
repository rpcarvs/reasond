package install

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const (
	reasoningAuditsBlockBegin = "<!-- REASONING-AUDITS:BEGIN -->"
	reasoningAuditsBlockEnd   = "<!-- REASONING-AUDITS:END -->"
)

var reasoningAuditsBlockPattern = regexp.MustCompile(`(?s)` + regexp.QuoteMeta(reasoningAuditsBlockBegin) + `.*?` + regexp.QuoteMeta(reasoningAuditsBlockEnd))

// NormalizeManagedContent returns the file content that rdit expects after applying
// its managed-file rules to the current content.
func NormalizeManagedContent(targetPath string, existing []byte, expected []byte) ([]byte, error) {
	switch managedFileKind(targetPath) {
	case managedFileContext:
		return normalizeContextFile(existing, expected), nil
	case managedFileHookConfig:
		return normalizeHookConfig(existing, expected)
	default:
		return append([]byte(nil), expected...), nil
	}
}

type managedKind string

const (
	managedFileDirect     managedKind = "direct"
	managedFileContext    managedKind = "context"
	managedFileHookConfig managedKind = "hook_config"
)

func managedFileKind(targetPath string) managedKind {
	clean := filepath.ToSlash(targetPath)

	switch clean {
	case "AGENTS.md", "CLAUDE.md":
		return managedFileContext
	case ".codex/hooks.json", ".claude/settings.json":
		return managedFileHookConfig
	default:
		return managedFileDirect
	}
}

func normalizeContextFile(existing []byte, expected []byte) []byte {
	body := strings.TrimSpace(string(expected))
	block := reasoningAuditsBlockBegin + "\n" + body + "\n" + reasoningAuditsBlockEnd

	content := strings.TrimRight(string(existing), "\n\t ")
	if reasoningAuditsBlockPattern.MatchString(content) {
		content = reasoningAuditsBlockPattern.ReplaceAllString(content, block)
		if strings.TrimSpace(content) == "" {
			return []byte(block + "\n")
		}
		return []byte(strings.TrimRight(content, "\n\t ") + "\n")
	}

	if strings.TrimSpace(content) == "" {
		return []byte(block + "\n")
	}
	return []byte(content + "\n\n" + block + "\n")
}

func normalizeHookConfig(existing []byte, expected []byte) ([]byte, error) {
	var managed map[string]any
	if err := json.Unmarshal(expected, &managed); err != nil {
		return nil, fmt.Errorf("parse bundled json: %w", err)
	}

	current := make(map[string]any)
	if len(bytes.TrimSpace(existing)) > 0 {
		if err := json.Unmarshal(existing, &current); err != nil {
			return nil, fmt.Errorf("parse current json: %w", err)
		}
	}

	mergeHookMaps(current, managed)

	normalized, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal merged json: %w", err)
	}
	return appendJSONNewline(normalized), nil
}

func mergeHookMaps(dst map[string]any, src map[string]any) {
	for key, value := range src {
		if key != "hooks" {
			if _, exists := dst[key]; !exists {
				dst[key] = value
			}
			continue
		}

		srcHooks, ok := value.(map[string]any)
		if !ok {
			continue
		}

		dstHooks, ok := dst[key].(map[string]any)
		if !ok || dstHooks == nil {
			dstHooks = make(map[string]any, len(srcHooks))
			dst[key] = dstHooks
		}

		for hookName, rawEntries := range srcHooks {
			srcEntries, ok := rawEntries.([]any)
			if !ok {
				continue
			}
			existingEntries, _ := dstHooks[hookName].([]any)
			dstHooks[hookName] = mergeJSONArray(existingEntries, srcEntries)
		}
	}
}

func mergeJSONArray(existing []any, managed []any) []any {
	merged := slices.Clone(existing)
	seen := make(map[string]struct{}, len(merged))

	for _, entry := range merged {
		key := canonicalJSON(entry)
		if key == "" {
			continue
		}
		seen[key] = struct{}{}
	}

	for _, entry := range managed {
		key := canonicalJSON(entry)
		if key == "" {
			merged = append(merged, entry)
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, entry)
	}

	return merged
}

func canonicalJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func appendJSONNewline(content []byte) []byte {
	trimmed := bytes.TrimRight(content, "\n")
	return append(trimmed, '\n')
}
