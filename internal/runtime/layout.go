package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DirectoryName    = ".reasond"
	DatabaseFileName = "audits_reports.db"
	logsDirectoryName = "reasoning_logs"
)

// GitIgnoreEntries are the repository-local paths that init must keep out of version control.
var GitIgnoreEntries = []string{
	DirectoryName + "/",
	logsDirectoryName + "/",
}

// LayoutResult reports which runtime layout pieces were created or already present.
type LayoutResult struct {
	RuntimeDirCreated bool
	AuditDirCreated   bool
	GitIgnoreCreated  bool
	GitIgnoreAdded    []string
	GitIgnorePresent  []string
}

// EnsureLayout prepares the local runtime directory and gitignore entries required by reasond.
func EnsureLayout(targetDir string) (LayoutResult, error) {
	targetDir, err := filepath.Abs(targetDir)
	if err != nil {
		return LayoutResult{}, fmt.Errorf("resolve target dir: %w", err)
	}

	runtimeDir := filepath.Join(targetDir, DirectoryName)
	created, err := ensureDirectory(runtimeDir)
	if err != nil {
		return LayoutResult{}, err
	}

	auditDir := filepath.Join(targetDir, logsDirectoryName)
	auditCreated, err := ensureDirectory(auditDir)
	if err != nil {
		return LayoutResult{}, err
	}

	result := LayoutResult{
		RuntimeDirCreated: created,
		AuditDirCreated:   auditCreated,
	}

	gitIgnorePath := filepath.Join(targetDir, ".gitignore")
	gitIgnoreCreated, added, present, err := ensureGitIgnoreEntries(gitIgnorePath, GitIgnoreEntries)
	if err != nil {
		return LayoutResult{}, err
	}

	result.GitIgnoreCreated = gitIgnoreCreated
	result.GitIgnoreAdded = added
	result.GitIgnorePresent = present
	return result, nil
}

// DatabasePath returns the location of the SQLite database inside the runtime directory.
func DatabasePath(targetDir string) string {
	return filepath.Join(targetDir, DirectoryName, DatabaseFileName)
}

func ensureDirectory(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return false, fmt.Errorf("%q exists but is not a directory", path)
		}
		return false, nil
	}
	if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat %q: %w", path, err)
	}

	if err := os.MkdirAll(path, 0o755); err != nil {
		return false, fmt.Errorf("create directory %q: %w", path, err)
	}
	return true, nil
}

func ensureGitIgnoreEntries(path string, entries []string) (bool, []string, []string, error) {
	content, created, err := readOrCreateGitIgnore(path)
	if err != nil {
		return false, nil, nil, err
	}

	normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
	present := make(map[string]struct{})
	for _, line := range strings.Split(normalized, "\n") {
		if line == "" {
			continue
		}
		present[line] = struct{}{}
	}

	var added []string
	var existing []string
	for _, entry := range entries {
		if _, ok := present[entry]; ok {
			existing = append(existing, entry)
			continue
		}
		added = append(added, entry)
	}

	if len(added) == 0 {
		return created, nil, existing, nil
	}

	builder := strings.Builder{}
	builder.WriteString(normalized)
	if normalized != "" && !strings.HasSuffix(normalized, "\n") {
		builder.WriteString("\n")
	}
	for _, entry := range added {
		builder.WriteString(entry)
		builder.WriteString("\n")
	}

	if err := os.WriteFile(path, []byte(builder.String()), 0o644); err != nil {
		return false, nil, nil, fmt.Errorf("write %q: %w", path, err)
	}

	return created, added, existing, nil
}

func readOrCreateGitIgnore(path string) ([]byte, bool, error) {
	content, err := os.ReadFile(path)
	if err == nil {
		return content, false, nil
	}
	if !os.IsNotExist(err) {
		return nil, false, fmt.Errorf("read %q: %w", path, err)
	}

	if err := os.WriteFile(path, nil, 0o644); err != nil {
		return nil, false, fmt.Errorf("create %q: %w", path, err)
	}
	return nil, true, nil
}
