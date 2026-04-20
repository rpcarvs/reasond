package integrity

import (
	"os"
	"path/filepath"
	"testing"

	assetbundle "rdit/cmd/assets"
	"rdit/internal/install"
	appRuntime "rdit/internal/runtime"
	"rdit/internal/storage"
)

func TestCheckReportsHealthyCodexInstall(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	_, err := (install.Installer{}).Install(root, assetbundle.ProviderCodex)
	if err != nil {
		t.Fatalf("install codex assets: %v", err)
	}

	_, err = appRuntime.EnsureLayout(root)
	if err != nil {
		t.Fatalf("ensure runtime layout: %v", err)
	}
	store, err := storage.Open(root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	report, err := (Checker{}).Check(root)
	if err != nil {
		t.Fatalf("check repo integrity: %v", err)
	}

	if report.Runtime.RuntimeDir.Status != StatusPresent {
		t.Fatalf("expected runtime dir present, got %s", report.Runtime.RuntimeDir.Status)
	}
	if report.Runtime.GitIgnore.Status != StatusPresent {
		t.Fatalf("expected .gitignore present, got %s", report.Runtime.GitIgnore.Status)
	}
	if report.Providers[assetbundle.ProviderCodex].Healthy() != true {
		t.Fatalf("expected codex provider healthy")
	}
	if report.Providers[assetbundle.ProviderClaude].Healthy() != false {
		t.Fatalf("expected claude provider unhealthy when not installed")
	}
	if report.Healthy() != true {
		t.Fatalf("expected report healthy")
	}
}

func TestCheckReportsModifiedManagedFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	_, err := (install.Installer{}).Install(root, assetbundle.ProviderCodex)
	if err != nil {
		t.Fatalf("install codex assets: %v", err)
	}

	_, err = appRuntime.EnsureLayout(root)
	if err != nil {
		t.Fatalf("ensure runtime layout: %v", err)
	}
	store, err := storage.Open(root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	agentsPath := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("custom\n"), 0o644); err != nil {
		t.Fatalf("modify managed file: %v", err)
	}

	report, err := (Checker{}).Check(root)
	if err != nil {
		t.Fatalf("check repo integrity: %v", err)
	}

	modified := report.Providers[assetbundle.ProviderCodex].ModifiedPaths()
	if len(modified) != 1 || modified[0] != "AGENTS.md" {
		t.Fatalf("expected AGENTS.md modified, got %v", modified)
	}
	if report.Providers[assetbundle.ProviderCodex].Healthy() {
		t.Fatalf("expected codex provider unhealthy after file drift")
	}
	if report.Healthy() {
		t.Fatalf("expected overall report unhealthy after file drift")
	}
}

func TestCheckReportsUnhealthyWhenDatabaseMissing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	_, err := (install.Installer{}).Install(root, assetbundle.ProviderClaude)
	if err != nil {
		t.Fatalf("install claude assets: %v", err)
	}

	_, err = appRuntime.EnsureLayout(root)
	if err != nil {
		t.Fatalf("ensure runtime layout: %v", err)
	}

	report, err := (Checker{}).Check(root)
	if err != nil {
		t.Fatalf("check repo integrity: %v", err)
	}

	if report.Runtime.Database.Status != StatusMissing {
		t.Fatalf("expected database missing status, got %s", report.Runtime.Database.Status)
	}
	if report.Providers[assetbundle.ProviderClaude].Healthy() != true {
		t.Fatalf("expected claude provider healthy")
	}
	if report.Healthy() {
		t.Fatalf("expected overall report unhealthy without runtime database")
	}
}
