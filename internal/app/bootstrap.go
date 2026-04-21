package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	assetbundle "github.com/rpcarvs/reasond/cmd/assets"
	"github.com/rpcarvs/reasond/internal/codexconfig"
	"github.com/rpcarvs/reasond/internal/install"
	"github.com/rpcarvs/reasond/internal/integrity"
	"github.com/rpcarvs/reasond/internal/judge"
	"github.com/rpcarvs/reasond/internal/processing"
	appRuntime "github.com/rpcarvs/reasond/internal/runtime"
	"github.com/rpcarvs/reasond/internal/storage"
)

// Bootstrap wires shared initialization, integrity, storage, and processing services for one repo.
type Bootstrap struct {
	RootDir            string
	CodexConfigManager codexconfig.Manager
}

// LayoutResult mirrors the runtime layout result exposed by the runtime package.
type LayoutResult = appRuntime.LayoutResult

// CodexConfigResult mirrors the Codex global config result exposed by the codexconfig package.
type CodexConfigResult = codexconfig.Result

// DatabaseResult reports whether the runtime SQLite database already existed or was created during init.
type DatabaseResult struct {
	Path    string
	Created bool
}

// InitResult summarizes all side effects performed during provider initialization.
type InitResult struct {
	Install  install.Result
	Layout   appRuntime.LayoutResult
	Database DatabaseResult
	Codex    *codexconfig.Result
}

// ErrCodexHooksBlocked reports that local Codex init was skipped because the global feature flag is disabled.
var ErrCodexHooksBlocked = errors.New("codex hooks are disabled in the global config")

// NewBootstrap resolves the shared application root used by CLI and TUI flows.
func NewBootstrap(rootDir string) (Bootstrap, error) {
	absolute, err := filepath.Abs(rootDir)
	if err != nil {
		return Bootstrap{}, fmt.Errorf("resolve root dir: %w", err)
	}
	return Bootstrap{RootDir: absolute}, nil
}

// Inspect returns the current repository integrity state without mutating it.
func (b Bootstrap) Inspect() (integrity.Report, error) {
	return (integrity.Checker{}).Check(b.RootDir)
}

// InitProvider installs provider assets and runtime files using the shared bootstrap path.
func (b Bootstrap) InitProvider(provider assetbundle.Provider) (InitResult, error) {
	result := InitResult{}

	if provider == assetbundle.ProviderCodex {
		configManager := b.CodexConfigManager
		configResult, err := configManager.EnsureHooksEnabled()
		if err != nil {
			return result, err
		}
		result.Codex = &configResult
		if configResult.Status == codexconfig.StatusBlocked {
			return result, ErrCodexHooksBlocked
		}
	}

	installResult, err := (install.Installer{}).Install(b.RootDir, provider)
	if err != nil {
		result.Install = installResult
		return result, err
	}
	result.Install = installResult

	layoutResult, err := appRuntime.EnsureLayout(b.RootDir)
	if err != nil {
		result.Layout = layoutResult
		return result, err
	}
	result.Layout = layoutResult

	databasePath := appRuntime.DatabasePath(b.RootDir)
	_, statErr := os.Stat(databasePath)
	databaseCreated := os.IsNotExist(statErr)
	if statErr != nil && !os.IsNotExist(statErr) {
		return result, fmt.Errorf("stat runtime database: %w", statErr)
	}

	store, err := storage.Open(b.RootDir)
	if err != nil {
		return result, err
	}
	result.Database = DatabaseResult{
		Path:    databasePath,
		Created: databaseCreated,
	}
	if err := store.Close(); err != nil {
		return result, fmt.Errorf("close runtime database: %w", err)
	}

	return result, nil
}

// OpenStore opens the runtime SQLite database.
func (b Bootstrap) OpenStore() (*storage.Store, error) {
	return storage.Open(b.RootDir)
}

// NewProcessor wires the shared judge runners for batch processing.
func (b Bootstrap) NewProcessor(store *storage.Store) *processing.Processor {
	return &processing.Processor{
		Store:       store,
		CodexRunner: judge.CodexRunner{},
		ClaudeRunner: judge.ClaudeRunner{
			FallbackModel: "claude-sonnet-4-6",
		},
	}
}
