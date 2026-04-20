package codexconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Status describes the effective state of the global Codex hooks feature flag.
type Status string

const (
	StatusEnabled Status = "enabled"
	StatusAdded   Status = "added"
	StatusBlocked Status = "blocked"
)

// Result reports the config file path and the final hooks state observed or applied.
type Result struct {
	Path   string
	Status Status
}

// Manager edits or inspects the user's global Codex configuration.
type Manager struct {
	HomeDir string
}

var codexHooksPattern = regexp.MustCompile(`^codex_hooks\s*=\s*(true|false)\s*(?:#.*)?$`)

// EnsureHooksEnabled verifies or adds the global Codex hook feature flag.
func (m Manager) EnsureHooksEnabled() (Result, error) {
	homeDir := m.HomeDir
	if homeDir == "" {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return Result{}, fmt.Errorf("resolve home dir: %w", err)
		}
	}

	configDir := filepath.Join(homeDir, ".codex")
	configPath := filepath.Join(configDir, "config.toml")

	content, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("read %q: %w", configPath, err)
	}

	updated, status, err := ensureCodexHooksSetting(string(content))
	if err != nil {
		return Result{}, err
	}
	if status != StatusAdded {
		return Result{Path: configPath, Status: status}, nil
	}

	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create %q: %w", configDir, err)
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		return Result{}, fmt.Errorf("write %q: %w", configPath, err)
	}

	return Result{Path: configPath, Status: status}, nil
}

func ensureCodexHooksSetting(content string) (string, Status, error) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	hasTrailingNewline := strings.HasSuffix(normalized, "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}

	featuresStart := -1
	featuresEnd := len(lines)

	for index, line := range lines {
		trimmed := strings.TrimSpace(line)

		if isSectionHeader(trimmed) {
			if trimmed == "[features]" {
				featuresStart = index
				featuresEnd = len(lines)
				continue
			}
			if featuresStart >= 0 && index > featuresStart {
				featuresEnd = index
				break
			}
		}

		if featuresStart >= 0 && index > featuresStart {
			match := codexHooksPattern.FindStringSubmatch(trimmed)
			if len(match) == 0 {
				continue
			}
			if match[1] == "true" {
				return content, StatusEnabled, nil
			}
			return content, StatusBlocked, nil
		}
	}

	insertLine := "codex_hooks = true"

	if featuresStart >= 0 {
		updatedLines := insertAfter(lines, featuresEnd, insertLine)
		return joinLines(updatedLines, hasTrailingNewline), StatusAdded, nil
	}

	updatedLines := appendLines(lines, "[features]", insertLine)
	return joinLines(updatedLines, true), StatusAdded, nil
}

func isSectionHeader(line string) bool {
	return strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]")
}

func insertAfter(lines []string, index int, value string) []string {
	result := make([]string, 0, len(lines)+1)
	result = append(result, lines[:index]...)
	result = append(result, value)
	result = append(result, lines[index:]...)
	return result
}

func appendLines(lines []string, values ...string) []string {
	result := append([]string{}, lines...)
	if len(result) > 0 && result[len(result)-1] == "" {
		result = result[:len(result)-1]
	}
	return append(result, values...)
}

func joinLines(lines []string, trailingNewline bool) string {
	if len(lines) == 0 {
		return ""
	}

	joined := strings.Join(lines, "\n")
	if trailingNewline && !strings.HasSuffix(joined, "\n") {
		joined += "\n"
	}
	return joined
}
