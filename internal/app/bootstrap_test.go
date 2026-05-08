package app

import (
	"os"
	"path/filepath"
	"testing"

	assetbundle "github.com/rpcarvs/reasond/cmd/assets"
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

func TestInitProviderCodexInstallsWithoutGlobalConfigMutation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bootstrap := Bootstrap{RootDir: root}

	result, err := bootstrap.InitProvider(assetbundle.ProviderCodex)
	if err != nil {
		t.Fatalf("init codex provider: %v", err)
	}

	for _, path := range []string{
		filepath.Join(root, ".codex"),
		filepath.Join(root, ".reasond"),
		filepath.Join(root, ".reasond_tmp"),
	} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("expected %s to exist, stat err=%v", path, statErr)
		}
	}
	if result.Database.Path == "" {
		t.Fatalf("expected database path to be reported")
	}
}
