package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	assetbundle "github.com/rpcarvs/reasond/cmd/assets"
	"github.com/rpcarvs/reasond/internal/codexconfig"
	"github.com/rpcarvs/reasond/internal/integrity"
	appRuntime "github.com/rpcarvs/reasond/internal/runtime"
)

func TestInitProviderCreatesDatabaseEagerlyAndIsIdempotent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	bootstrap, err := NewBootstrap(root)
	if err != nil {
		t.Fatalf("new bootstrap: %v", err)
	}

	first, err := bootstrap.InitProvider(assetbundle.ProviderClaude)
	if err != nil {
		t.Fatalf("first init: %v", err)
	}
	if !first.Database.Created {
		t.Fatalf("expected first init to create database")
	}

	databasePath := filepath.Join(root, appRuntime.DirectoryName, appRuntime.DatabaseFileName)
	if first.Database.Path != databasePath {
		t.Fatalf("expected database path %q, got %q", databasePath, first.Database.Path)
	}
	if _, err := os.Stat(databasePath); err != nil {
		t.Fatalf("stat initialized database: %v", err)
	}
	if info, err := os.Stat(filepath.Join(root, appRuntime.StagingDirectoryName)); err != nil || !info.IsDir() {
		t.Fatalf("expected .reasond_tmp directory to exist, stat err=%v", err)
	}
	if info, err := os.Stat(appRuntime.ArchivePath(root)); err != nil || !info.IsDir() {
		t.Fatalf("expected reasond_audits directory to exist, stat err=%v", err)
	}

	report, err := (integrity.Checker{}).Check(root)
	if err != nil {
		t.Fatalf("check integrity after first init: %v", err)
	}
	if report.Runtime.Database.Status != integrity.StatusPresent {
		t.Fatalf("expected runtime database present, got %s", report.Runtime.Database.Status)
	}
	if !report.Providers[assetbundle.ProviderClaude].Healthy() {
		t.Fatalf("expected claude provider healthy after init")
	}
	if report.Providers[assetbundle.ProviderCodex].Healthy() {
		t.Fatalf("expected codex provider unhealthy when not installed")
	}
	if !report.Healthy() {
		t.Fatalf("expected report healthy with one initialized provider and database")
	}

	second, err := bootstrap.InitProvider(assetbundle.ProviderClaude)
	if err != nil {
		t.Fatalf("second init: %v", err)
	}
	if second.Database.Created {
		t.Fatalf("expected second init to reuse existing database")
	}
	if _, err := os.Stat(databasePath); err != nil {
		t.Fatalf("stat database after second init: %v", err)
	}
}

func TestInitProviderCodexBlockedConfigDoesNotMutateRepository(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	homeDir := t.TempDir()

	configDir := filepath.Join(homeDir, ".codex")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[features]\ncodex_hooks = false\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	bootstrap := Bootstrap{
		RootDir:            root,
		CodexConfigManager: codexconfig.Manager{HomeDir: homeDir},
	}

	result, err := bootstrap.InitProvider(assetbundle.ProviderCodex)
	if !errors.Is(err, ErrCodexHooksBlocked) {
		t.Fatalf("expected blocked codex hooks error, got %v", err)
	}
	if result.Codex == nil || result.Codex.Status != codexconfig.StatusBlocked {
		t.Fatalf("expected blocked codex config result, got %+v", result.Codex)
	}

	for _, path := range []string{
		filepath.Join(root, ".codex"),
		filepath.Join(root, "AGENTS.md"),
		filepath.Join(root, ".reasond"),
		filepath.Join(root, ".reasond_tmp"),
		filepath.Join(root, ".gitignore"),
	} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("expected %s to remain absent, stat err=%v", path, statErr)
		}
	}
}
