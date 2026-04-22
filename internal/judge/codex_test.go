package judge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCodexRunnerParsesStructuredOutput(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	binaryPath := filepath.Join(root, "fake-codex.sh")

	script := `#!/usr/bin/env bash
set -euo pipefail

if [[ "$1" != "exec" ]]; then
  exit 9
fi

schema=""
last=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output-schema)
      schema="$2"
      shift 2
      ;;
    --output-last-message)
      last="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done

if [[ -z "$schema" || ! -f "$schema" ]]; then
  exit 7
fi
if [[ -z "$last" ]]; then
  exit 8
fi

printf '{"findings":[{"title":"A","issue":"B","why":"C","how":"D","score":0.6}]}' > "$last"
`
	if err := os.WriteFile(binaryPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex script: %v", err)
	}

	response, err := (CodexRunner{BinaryPath: binaryPath}).Run(
		context.Background(),
		root,
		"gpt-5.4-mini",
		"# User Prompt\n\nDo it.\n\n# Reasoning by Codex\n\nThe agent did it.",
	)
	if err != nil {
		t.Fatalf("run codex runner: %v", err)
	}

	if len(response.Findings) != 1 {
		t.Fatalf("expected one finding, got %d", len(response.Findings))
	}
	if response.Findings[0].Title != "A" || response.Findings[0].Score != 0.6 {
		t.Fatalf("unexpected finding: %+v", response.Findings[0])
	}
}
