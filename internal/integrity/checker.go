package integrity

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	assetbundle "rdit/cmd/assets"
	appRuntime "rdit/internal/runtime"
)

// Status describes whether an expected runtime or provider-managed file is present, missing, or drifted.
type Status string

const (
	StatusMissing  Status = "missing"
	StatusPresent  Status = "present"
	StatusModified Status = "modified"
)

// Report summarizes the runtime and provider installation state for one repository.
type Report struct {
	RootDir   string
	Runtime   RuntimeStatus
	Providers map[assetbundle.Provider]ProviderStatus
}

// RuntimeStatus captures the integrity of the local rdit runtime files.
type RuntimeStatus struct {
	RuntimeDir        ItemStatus
	Database          ItemStatus
	GitIgnore         ItemStatus
	MissingGitIgnores []string
}

// ProviderStatus captures the integrity of one provider-specific managed payload.
type ProviderStatus struct {
	Provider assetbundle.Provider
	Files    []FileStatus
}

// ItemStatus describes the state of one runtime-level path.
type ItemStatus struct {
	Path   string
	Status Status
}

// FileStatus describes the state of one provider-managed file.
type FileStatus struct {
	Path   string
	Status Status
}

// Healthy reports whether runtime requirements and at least one provider are fully present.
func (r Report) Healthy() bool {
	if r.Runtime.RuntimeDir.Status != StatusPresent ||
		r.Runtime.Database.Status != StatusPresent ||
		r.Runtime.GitIgnore.Status != StatusPresent {
		return false
	}

	for _, provider := range r.Providers {
		if provider.Healthy() {
			return true
		}
	}

	return false
}

// ProviderNames returns the providers seen in the report in stable order.
func (r Report) ProviderNames() []assetbundle.Provider {
	providers := []assetbundle.Provider{assetbundle.ProviderCodex, assetbundle.ProviderClaude}
	var present []assetbundle.Provider
	for _, provider := range providers {
		if _, ok := r.Providers[provider]; ok {
			present = append(present, provider)
		}
	}
	return present
}

// Healthy reports whether all expected files for the provider are present and unmodified.
func (p ProviderStatus) Healthy() bool {
	if len(p.Files) == 0 {
		return false
	}

	for _, file := range p.Files {
		if file.Status != StatusPresent {
			return false
		}
	}

	return true
}

// MissingPaths returns all expected files that do not exist.
func (p ProviderStatus) MissingPaths() []string {
	return collectPathsByStatus(p.Files, StatusMissing)
}

// ModifiedPaths returns all expected files that exist but do not match the bundled payload.
func (p ProviderStatus) ModifiedPaths() []string {
	return collectPathsByStatus(p.Files, StatusModified)
}

// Checker inspects a repository without mutating it.
type Checker struct{}

// Check inspects whether rdit has been installed correctly in targetDir.
func (Checker) Check(targetDir string) (Report, error) {
	rootDir, err := filepath.Abs(targetDir)
	if err != nil {
		return Report{}, fmt.Errorf("resolve target dir: %w", err)
	}

	report := Report{
		RootDir:   rootDir,
		Runtime:   checkRuntime(rootDir),
		Providers: make(map[assetbundle.Provider]ProviderStatus),
	}

	for _, provider := range []assetbundle.Provider{assetbundle.ProviderCodex, assetbundle.ProviderClaude} {
		status, err := checkProvider(rootDir, provider)
		if err != nil {
			return Report{}, err
		}
		report.Providers[provider] = status
	}

	return report, nil
}

func checkRuntime(rootDir string) RuntimeStatus {
	runtimeDir := filepath.Join(rootDir, appRuntime.DirectoryName)
	databasePath := appRuntime.DatabasePath(rootDir)
	gitIgnorePath := filepath.Join(rootDir, ".gitignore")

	missingEntries := missingGitIgnoreEntries(gitIgnorePath, appRuntime.GitIgnoreEntries)

	return RuntimeStatus{
		RuntimeDir: ItemStatus{
			Path:   runtimeDir,
			Status: pathStatus(runtimeDir, true),
		},
		Database: ItemStatus{
			Path:   databasePath,
			Status: pathStatus(databasePath, false),
		},
		GitIgnore: ItemStatus{
			Path:   gitIgnorePath,
			Status: gitIgnoreStatus(gitIgnorePath, missingEntries),
		},
		MissingGitIgnores: missingEntries,
	}
}

func checkProvider(rootDir string, provider assetbundle.Provider) (ProviderStatus, error) {
	files, err := assetbundle.FilesForProvider(provider)
	if err != nil {
		return ProviderStatus{}, err
	}

	status := ProviderStatus{Provider: provider}
	for _, file := range files {
		embedded, err := assetbundle.FS.ReadFile(file.EmbeddedPath)
		if err != nil {
			return ProviderStatus{}, fmt.Errorf("read bundled asset %q: %w", file.EmbeddedPath, err)
		}

		targetPath := filepath.Join(rootDir, filepath.FromSlash(file.TargetPath))
		fileStatus, err := compareFile(targetPath, embedded)
		if err != nil {
			return ProviderStatus{}, err
		}

		status.Files = append(status.Files, FileStatus{
			Path:   file.TargetPath,
			Status: fileStatus,
		})
	}

	return status, nil
}

func compareFile(path string, expected []byte) (Status, error) {
	actual, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return StatusMissing, nil
		}
		return "", fmt.Errorf("read %q: %w", path, err)
	}

	if bytes.Equal(actual, expected) {
		return StatusPresent, nil
	}

	return StatusModified, nil
}

func pathStatus(path string, requireDir bool) Status {
	info, err := os.Stat(path)
	if err != nil {
		return StatusMissing
	}

	if requireDir && !info.IsDir() {
		return StatusModified
	}
	if !requireDir && info.IsDir() {
		return StatusModified
	}

	return StatusPresent
}

func gitIgnoreStatus(path string, missingEntries []string) Status {
	info, err := os.Stat(path)
	if err != nil {
		return StatusMissing
	}
	if info.IsDir() {
		return StatusModified
	}
	if len(missingEntries) > 0 {
		return StatusModified
	}
	return StatusPresent
}

func missingGitIgnoreEntries(path string, entries []string) []string {
	content, err := os.ReadFile(path)
	if err != nil {
		return append([]string{}, entries...)
	}

	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		seen[line] = struct{}{}
	}

	var missing []string
	for _, entry := range entries {
		if _, ok := seen[entry]; !ok {
			missing = append(missing, entry)
		}
	}

	return missing
}

func collectPathsByStatus(files []FileStatus, status Status) []string {
	var paths []string
	for _, file := range files {
		if file.Status == status {
			paths = append(paths, file.Path)
		}
	}
	return paths
}
