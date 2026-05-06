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
	reasoningAuditBlockBegin = "<!-- REASONING-AUDIT:BEGIN -->"
	reasoningAuditBlockEnd   = "<!-- REASONING-AUDIT:END -->"
	reasoningDebugBlockBegin = "<!-- REASONING-DEBUG:BEGIN -->"
	reasoningDebugBlockEnd   = "<!-- REASONING-DEBUG:END -->"
)

var managedContextBlocks = []managedContextBlock{
	{
		pattern: regexp.MustCompile(`(?s)` + regexp.QuoteMeta(reasoningAuditBlockBegin) + `.*?` + regexp.QuoteMeta(reasoningAuditBlockEnd)),
	},
	{
		pattern: regexp.MustCompile(`(?s)` + regexp.QuoteMeta(reasoningDebugBlockBegin) + `.*?` + regexp.QuoteMeta(reasoningDebugBlockEnd)),
	},
}

type managedContextBlock struct {
	pattern *regexp.Regexp
}

// NormalizeManagedContent returns the file content that reasond expects after applying
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
	case "AGENTS.md":
		return managedFileContext
	case ".codex/hooks.json", ".claude/settings.json":
		return managedFileHookConfig
	default:
		return managedFileDirect
	}
}

func normalizeContextFile(existing []byte, expected []byte) []byte {
	blocks := expectedContextBlocks(expected)

	content := strings.TrimRight(string(existing), "\n\t ")
	for _, block := range blocks {
		if block.pattern.MatchString(content) {
			content = block.pattern.ReplaceAllString(content, block.content)
			continue
		}
		if strings.TrimSpace(content) == "" {
			content = block.content
			continue
		}
		content = strings.TrimRight(content, "\n\t ") + "\n\n" + block.content
	}

	return []byte(strings.TrimRight(content, "\n\t ") + "\n")
}

type expectedContextBlock struct {
	content string
	pattern *regexp.Regexp
}

func expectedContextBlocks(expected []byte) []expectedContextBlock {
	body := strings.TrimSpace(string(expected))
	if !strings.Contains(body, reasoningAuditBlockBegin) {
		body = reasoningAuditBlockBegin + "\n" + body + "\n" + reasoningAuditBlockEnd
	}

	blocks := make([]expectedContextBlock, 0, len(managedContextBlocks))
	for _, block := range managedContextBlocks {
		content := block.pattern.FindString(body)
		if strings.TrimSpace(content) == "" {
			continue
		}
		blocks = append(blocks, expectedContextBlock{
			content: content,
			pattern: block.pattern,
		})
	}
	return blocks
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
