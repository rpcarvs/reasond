package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rpcarvs/reasond/internal/testutil"
)

func TestEnsureLayoutUsesPartialFixtureIdempotently(t *testing.T) {
	t.Parallel()

	root := testutil.CopyFixtureTree(t, "repos/partial-codex")
	if err := os.MkdirAll(filepath.Join(root, ".reasond"), 0o755); err != nil {
		t.Fatalf("seed existing .reasond directory: %v", err)
	}

	result, err := EnsureLayout(root)
	if err != nil {
		t.Fatalf("ensure layout: %v", err)
	}

	if result.RuntimeDirCreated {
		t.Fatalf("expected existing .reasond directory to be reused")
	}
	if !result.StagingDirCreated {
		t.Fatalf("expected .reasond_tmp directory to be created")
	}
	if !result.ArchiveDirCreated {
		t.Fatalf("expected reasond_audits directory to be created")
	}
	if result.GitIgnoreCreated {
		t.Fatalf("expected existing .gitignore to be reused")
	}
	if len(result.GitIgnoreAdded) != 1 || result.GitIgnoreAdded[0] != ".reasond_tmp/" {
		t.Fatalf("unexpected gitignore additions: %v", result.GitIgnoreAdded)
	}
	if len(result.GitIgnorePresent) != 1 || result.GitIgnorePresent[0] != ".reasond/" {
		t.Fatalf("unexpected gitignore present entries: %v", result.GitIgnorePresent)
	}

	content, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if string(content) != ".reasond/\n.reasond_tmp/\n" {
		t.Fatalf("unexpected .gitignore content: %q", string(content))
	}
	if info, err := os.Stat(filepath.Join(root, ".reasond_tmp")); err != nil || !info.IsDir() {
		t.Fatalf("expected .reasond_tmp directory to exist, stat err=%v", err)
	}
	if info, err := os.Stat(filepath.Join(root, ".reasond", "reasond_audits")); err != nil || !info.IsDir() {
		t.Fatalf("expected reasond_audits directory to exist, stat err=%v", err)
	}
}
