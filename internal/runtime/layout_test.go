package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rpcarvs/rdit/internal/testutil"
)

func TestEnsureLayoutUsesPartialFixtureIdempotently(t *testing.T) {
	t.Parallel()

	root := testutil.CopyFixtureTree(t, "repos/partial-codex")

	result, err := EnsureLayout(root)
	if err != nil {
		t.Fatalf("ensure layout: %v", err)
	}

	if result.RuntimeDirCreated {
		t.Fatalf("expected existing .rdit directory to be reused")
	}
	if !result.AuditDirCreated {
		t.Fatalf("expected reasoning_audits directory to be created")
	}
	if result.GitIgnoreCreated {
		t.Fatalf("expected existing .gitignore to be reused")
	}
	if len(result.GitIgnoreAdded) != 1 || result.GitIgnoreAdded[0] != "reasoning_audits/" {
		t.Fatalf("unexpected gitignore additions: %v", result.GitIgnoreAdded)
	}
	if len(result.GitIgnorePresent) != 1 || result.GitIgnorePresent[0] != ".rdit/" {
		t.Fatalf("unexpected gitignore present entries: %v", result.GitIgnorePresent)
	}

	content, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if string(content) != ".rdit/\nreasoning_audits/\n" {
		t.Fatalf("unexpected .gitignore content: %q", string(content))
	}
	if info, err := os.Stat(filepath.Join(root, "reasoning_audits")); err != nil || !info.IsDir() {
		t.Fatalf("expected reasoning_audits directory to exist, stat err=%v", err)
	}
}
