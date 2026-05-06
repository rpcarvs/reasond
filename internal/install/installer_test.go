package install

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	assetbundle "github.com/rpcarvs/reasond/cmd/assets"
	"github.com/rpcarvs/reasond/internal/testutil"
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
		".codex/skills/reasoning-debug/SKILL.md",
		"AGENTS.md",
	}
	if !slices.Equal(result.Created, expectedCreated) {
		t.Fatalf("unexpected created paths: %v", result.Created)
	}
	if len(result.Updated) != 0 {
		t.Fatalf("expected no updated paths, got %v", result.Updated)
	}

	for _, relativePath := range expectedCreated {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relativePath))); err != nil {
			t.Fatalf("stat installed file %q: %v", relativePath, err)
		}
	}
}

func TestInstallMergesManagedBlockIntoExistingAgentsFile(t *testing.T) {
	t.Parallel()

	root := testutil.CopyFixtureTree(t, "repos/conflict-agents")

	result, err := (Installer{}).Install(root, assetbundle.ProviderCodex)
	if err != nil {
		t.Fatalf("install codex assets: %v", err)
	}
	if !slices.Equal(result.Updated, []string{"AGENTS.md"}) {
		t.Fatalf("unexpected updated paths: %v", result.Updated)
	}

	content, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "# Custom Agent Rules") {
		t.Fatalf("expected original AGENTS content to be preserved")
	}
	if strings.Count(text, reasoningAuditBlockBegin) != 1 {
		t.Fatalf("expected one managed reasoning audit block, got %d", strings.Count(text, reasoningAuditBlockBegin))
	}
	if strings.Count(text, reasoningDebugBlockBegin) != 1 {
		t.Fatalf("expected one managed reasoning debug block, got %d", strings.Count(text, reasoningDebugBlockBegin))
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

	claudeContent, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	if strings.TrimSpace(string(claudeContent)) != "See [AGENTS.md](./AGENTS.md)" {
		t.Fatalf("expected CLAUDE.md pointer, got %q", string(claudeContent))
	}

	agentsContent, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if strings.Count(string(agentsContent), reasoningAuditBlockBegin) != 1 {
		t.Fatalf("expected one reasoning audit block in AGENTS.md")
	}
	if strings.Count(string(agentsContent), reasoningDebugBlockBegin) != 1 {
		t.Fatalf("expected one reasoning debug block in AGENTS.md")
	}
}

func TestInstallMergesExistingHookConfigWithoutDuplication(t *testing.T) {
	t.Parallel()

	root := testutil.CopyFixtureTree(t, "repos/partial-codex")

	first, err := (Installer{}).Install(root, assetbundle.ProviderCodex)
	if err != nil {
		t.Fatalf("first install codex assets: %v", err)
	}
	if !slices.Contains(first.Updated, ".codex/hooks.json") {
		t.Fatalf("expected hooks.json to be updated, got %v", first.Updated)
	}

	second, err := (Installer{}).Install(root, assetbundle.ProviderCodex)
	if err != nil {
		t.Fatalf("second install codex assets: %v", err)
	}
	if len(second.Updated) != 0 {
		t.Fatalf("expected second install to be idempotent, got updated %v", second.Updated)
	}

	content, err := os.ReadFile(filepath.Join(root, ".codex", "hooks.json"))
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}
	text := string(content)
	if strings.Count(text, "reasoning-audit-prompt.sh") != 1 {
		t.Fatalf("expected one prompt hook entry, got %d", strings.Count(text, "reasoning-audit-prompt.sh"))
	}
	if strings.Count(text, "reasoning-audit-stop.sh") != 1 {
		t.Fatalf("expected one stop hook entry, got %d", strings.Count(text, "reasoning-audit-stop.sh"))
	}
}

func TestInstallDetectsInvalidManagedJSON(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".codex"), 0o755); err != nil {
		t.Fatalf("create .codex dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".codex", "hooks.json"), []byte("{invalid"), 0o644); err != nil {
		t.Fatalf("write invalid hooks.json: %v", err)
	}

	_, err := (Installer{}).Install(root, assetbundle.ProviderCodex)
	if err == nil {
		t.Fatalf("expected conflict error")
	}

	var conflictErr *ConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected ConflictError, got %T", err)
	}
	if len(conflictErr.Paths) != 1 || conflictErr.Paths[0] != ".codex/hooks.json" {
		t.Fatalf("unexpected conflict paths: %v", conflictErr.Paths)
	}
}
