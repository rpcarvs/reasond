package judge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeRunnerParsesStructuredEnvelope(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	binaryPath := filepath.Join(root, "fake-claude.sh")
	argsPath := filepath.Join(root, "args.txt")
	pwdPath := filepath.Join(root, "pwd.txt")

	script := `#!/usr/bin/env bash
set -euo pipefail

pwd > "` + pwdPath + `"
printf '%s\n' "$@" > "` + argsPath + `"
printf '{"structured_output":{"findings":[{"title":"A","issue":"B","why":"C","how":"D","score":0.4}]}}'
`
	if err := os.WriteFile(binaryPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude script: %v", err)
	}

	response, err := (ClaudeRunner{
		BinaryPath: binaryPath,
	}).Run(
		context.Background(),
		root,
		"claude-haiku-4-5",
		"# User Prompt\n\nDo it.\n\n# Reasoning by Claude\n\nThe agent did it.",
	)
	if err != nil {
		t.Fatalf("run claude runner: %v", err)
	}

	if len(response.Findings) != 1 {
		t.Fatalf("expected one finding, got %d", len(response.Findings))
	}
	if response.Findings[0].Title != "A" || response.Findings[0].Score != 0.4 {
		t.Fatalf("unexpected finding: %+v", response.Findings[0])
	}

	argsContent, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read captured args: %v", err)
	}
	if strings.Contains(string(argsContent), "--fallback-model") {
		t.Fatalf("claude runner unexpectedly passed fallback model: %s", string(argsContent))
	}
	if strings.Contains(string(argsContent), "--bare") {
		t.Fatalf("claude runner used --bare, which disables OAuth/keychain login: %s", string(argsContent))
	}
	for _, expected := range []string{
		"--disable-slash-commands",
		"--tools",
		"--mcp-config",
		"--strict-mcp-config",
		"# User Prompt",
	} {
		if !strings.Contains(string(argsContent), expected) {
			t.Fatalf("expected claude args to contain %q in:\n%s", expected, string(argsContent))
		}
	}
	if strings.Contains(string(argsContent), root) {
		t.Fatalf("claude args leaked repository root:\n%s", string(argsContent))
	}

	pwdContent, err := os.ReadFile(pwdPath)
	if err != nil {
		t.Fatalf("read captured pwd: %v", err)
	}
	if strings.TrimSpace(string(pwdContent)) == root {
		t.Fatalf("claude runner executed inside repository root")
	}
}
