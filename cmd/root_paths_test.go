package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	assetbundle "github.com/rpcarvs/reasond/cmd/assets"
	"github.com/rpcarvs/reasond/internal/settings"
)

func TestRunInitRequestFromSubdirInitializesGitRoot(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)

	subdir := filepath.Join(root, "internal", "nested")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("create subdir: %v", err)
	}

	restore := chdirForRootPathTest(t, subdir)
	defer restore()

	var out bytes.Buffer
	err := runInitRequest(&out, initRequest{
		Providers: []assetbundle.Provider{assetbundle.ProviderClaude},
		Settings: settings.Settings{
			DefaultJudgeProvider: "claude",
			DefaultJudgeModel:    settings.DefaultClaudeModel,
		},
	})
	if err != nil {
		t.Fatalf("run init request from subdir: %v\n%s", err, out.String())
	}

	for _, path := range []string{
		filepath.Join(root, ".claude", "settings.json"),
		filepath.Join(root, ".reasond", "settings.json"),
		filepath.Join(root, ".reasond_tmp"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("stat initialized root path %q: %v", path, err)
		}
	}
}

func TestRunRootTUIUsesGitRootFromSubdir(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("resolve root symlink: %v", err)
	}

	subdir := filepath.Join(root, "pkg", "nested")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("create subdir: %v", err)
	}

	restore := chdirForRootPathTest(t, subdir)
	defer restore()

	originalRunTUI := runTUI
	t.Cleanup(func() {
		runTUI = originalRunTUI
	})

	var gotRoot string
	runTUI = func(rootDir string) error {
		gotRoot = rootDir
		return nil
	}

	if err := runRootTUI(); err != nil {
		t.Fatalf("run root TUI: %v", err)
	}
	if gotRoot != resolvedRoot {
		t.Fatalf("TUI root = %q, want %q", gotRoot, resolvedRoot)
	}
}

func chdirForRootPathTest(t *testing.T, dir string) func() {
	t.Helper()

	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("change cwd: %v", err)
	}
	return func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}
}
