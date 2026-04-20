package tui

import (
	"os"
	"path/filepath"
	"testing"

	progressbar "github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"

	assetbundle "rdit/cmd/assets"
	"rdit/internal/app"
	"rdit/internal/install"
	appRuntime "rdit/internal/runtime"
	"rdit/internal/storage"
	"rdit/internal/testutil"
)

func TestReloadStateOnUninitializedRepoStaysInOverview(t *testing.T) {
	t.Parallel()

	root := testutil.CopyFixtureTree(t, "repos/clean")

	bootstrap, err := app.NewBootstrap(root)
	if err != nil {
		t.Fatalf("new bootstrap: %v", err)
	}

	m := model{
		bootstrap: bootstrap,
		progress:  progressbar.New(),
	}
	if err := m.reloadState(); err != nil {
		t.Fatalf("reload state: %v", err)
	}

	if m.report.Healthy() {
		t.Fatalf("expected uninitialized repo to be unhealthy")
	}
	if m.phase != phaseOverview {
		t.Fatalf("expected overview phase, got %s", m.phase)
	}
}

func TestReloadStateOnHealthyRepoWithPendingAuditStartsPrompt(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, err := (install.Installer{}).Install(root, assetbundle.ProviderCodex); err != nil {
		t.Fatalf("install codex assets: %v", err)
	}
	if _, err := appRuntime.EnsureLayout(root); err != nil {
		t.Fatalf("ensure layout: %v", err)
	}
	store, err := storage.Open(root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	auditDir := filepath.Join(root, "reasoning_audits")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatalf("create audit dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(auditDir, "one.md"), []byte("# Reasoning\n\nfixture\n"), 0o644); err != nil {
		t.Fatalf("write audit file: %v", err)
	}

	bootstrap, err := app.NewBootstrap(root)
	if err != nil {
		t.Fatalf("new bootstrap: %v", err)
	}

	m := model{
		bootstrap: bootstrap,
		progress:  progressbar.New(),
	}
	if err := m.reloadState(); err != nil {
		t.Fatalf("reload state: %v", err)
	}

	if !m.report.Healthy() {
		t.Fatalf("expected initialized repo to be healthy")
	}
	if m.pendingCount != 1 {
		t.Fatalf("expected one pending audit, got %d", m.pendingCount)
	}
	if m.phase != phaseAuditPrompt {
		t.Fatalf("expected audit prompt phase, got %s", m.phase)
	}
}

func TestAuditPromptDeclineMovesToBoard(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, err := (install.Installer{}).Install(root, assetbundle.ProviderCodex); err != nil {
		t.Fatalf("install codex assets: %v", err)
	}
	if _, err := appRuntime.EnsureLayout(root); err != nil {
		t.Fatalf("ensure layout: %v", err)
	}
	store, err := storage.Open(root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	auditDir := filepath.Join(root, "reasoning_audits")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatalf("create audit dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(auditDir, "one.md"), []byte("# Reasoning\n\nfixture\n"), 0o644); err != nil {
		t.Fatalf("write audit file: %v", err)
	}

	bootstrap, err := app.NewBootstrap(root)
	if err != nil {
		t.Fatalf("new bootstrap: %v", err)
	}

	m := model{
		bootstrap: bootstrap,
		progress:  progressbar.New(),
	}
	if err := m.reloadState(); err != nil {
		t.Fatalf("reload state: %v", err)
	}

	nextModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	next := nextModel.(model)

	if next.phase != phaseBoard {
		t.Fatalf("expected board phase after declining audit, got %s", next.phase)
	}
	if !next.auditPromptDismissed {
		t.Fatalf("expected audit prompt to be dismissed")
	}
}
