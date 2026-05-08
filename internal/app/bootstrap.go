package app

import (
	"fmt"
	"os"
	"path/filepath"

	assetbundle "github.com/rpcarvs/reasond/cmd/assets"
	"github.com/rpcarvs/reasond/internal/install"
	"github.com/rpcarvs/reasond/internal/integrity"
	"github.com/rpcarvs/reasond/internal/judge"
	"github.com/rpcarvs/reasond/internal/processing"
	appRuntime "github.com/rpcarvs/reasond/internal/runtime"
	"github.com/rpcarvs/reasond/internal/storage"
)

// Bootstrap wires shared initialization, integrity, storage, and processing services for one repo.
type Bootstrap struct {
	RootDir string
}

// LayoutResult mirrors the runtime layout result exposed by the runtime package.
type LayoutResult = appRuntime.LayoutResult

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
}

// InitOptions controls optional repository mutations during provider initialization.
type InitOptions struct {
	ManageGitIgnore bool
}

// DefaultInitOptions returns the init defaults used by existing provider bootstrap flows.
func DefaultInitOptions() InitOptions {
	return InitOptions{ManageGitIgnore: true}
}

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
	return b.InitProviderWithOptions(provider, DefaultInitOptions())
}

// InitProviderWithOptions installs provider assets and runtime files using the shared bootstrap path.
func (b Bootstrap) InitProviderWithOptions(provider assetbundle.Provider, options InitOptions) (InitResult, error) {
	result := InitResult{}

	installResult, err := (install.Installer{}).Install(b.RootDir, provider)
	if err != nil {
		result.Install = installResult
		return result, err
	}
	result.Install = installResult

	layoutResult, err := appRuntime.EnsureLayoutWithOptions(b.RootDir, appRuntime.LayoutOptions{
		ManageGitIgnore: options.ManageGitIgnore,
	})
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
		Store:        store,
		CodexRunner:  judge.CodexRunner{},
		ClaudeRunner: judge.ClaudeRunner{},
	}
}
