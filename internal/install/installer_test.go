package install

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	assetbundle "rdit/cmd/assets"
	"rdit/internal/testutil"
)

func TestInstallCodexIntoCleanFixture(t *testing.T) {
	t.Parallel()

	root := testutil.CopyFixtureTree(t, "repos/clean")

	result, err := (Installer{}).Install(root, assetbundle.ProviderCodex)
	if err != nil {
		t.Fatalf("install codex assets: %v", err)
	}

	expectedCreated := []string{
		".codex/hooks.json",
		".codex/hooks/reasoning-audit-prompt.sh",
		".codex/hooks/reasoning-audit-stop.sh",
		".codex/skills/reasoning-audit/SKILL.md",
		"AGENTS.md",
	}
	if !slices.Equal(result.Created, expectedCreated) {
		t.Fatalf("unexpected created paths: %v", result.Created)
	}

	for _, relativePath := range expectedCreated {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relativePath))); err != nil {
			t.Fatalf("stat installed file %q: %v", relativePath, err)
		}
	}
}

func TestInstallDetectsConflictingManagedFile(t *testing.T) {
	t.Parallel()

	root := testutil.CopyFixtureTree(t, "repos/conflict-agents")

	_, err := (Installer{}).Install(root, assetbundle.ProviderCodex)
	if err == nil {
		t.Fatalf("expected conflict error")
	}

	var conflictErr *ConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected ConflictError, got %T", err)
	}
	if len(conflictErr.Paths) != 1 || conflictErr.Paths[0] != "AGENTS.md" {
		t.Fatalf("unexpected conflict paths: %v", conflictErr.Paths)
	}
}

func TestInstallAllowsCodexAndClaudeToCoexist(t *testing.T) {
	t.Parallel()

	root := testutil.CopyFixtureTree(t, "repos/clean")

	if _, err := (Installer{}).Install(root, assetbundle.ProviderCodex); err != nil {
		t.Fatalf("install codex assets: %v", err)
	}
	if _, err := (Installer{}).Install(root, assetbundle.ProviderClaude); err != nil {
		t.Fatalf("install claude assets: %v", err)
	}

	for _, relativePath := range []string{
		".codex/hooks.json",
		"AGENTS.md",
		".claude/settings.json",
		"CLAUDE.md",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relativePath))); err != nil {
			t.Fatalf("stat coexistence path %q: %v", relativePath, err)
		}
	}
}
