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

	script := `#!/usr/bin/env bash
set -euo pipefail

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
}
