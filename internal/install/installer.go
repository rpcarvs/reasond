package install

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	assetbundle "github.com/rpcarvs/reasond/cmd/assets"
)

type Status string

const (
	StatusCreate   Status = "create"
	StatusUpdate   Status = "update"
	StatusReuse    Status = "reuse"
	StatusConflict Status = "conflict"
)

type ItemResult struct {
	Path   string
	Status Status
}

type Result struct {
	Created   []string
	Updated   []string
	Reused    []string
	Conflicts []string
}

// HasConflicts reports whether the install result includes conflicting paths.
func (r Result) HasConflicts() bool {
	return len(r.Conflicts) > 0
}

// ConflictError indicates that init cannot proceed because an existing managed path
// cannot be safely interpreted or merged.
type ConflictError struct {
	Paths []string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("install conflicts detected: %s", strings.Join(e.Paths, ", "))
}

type Installer struct{}

// Install copies or merges provider assets into targetDir using the managed-file rules.
func (Installer) Install(targetDir string, provider assetbundle.Provider) (Result, error) {
	files, err := assetbundle.FilesForProvider(provider)
	if err != nil {
		return Result{}, err
	}

	targetDir, err = filepath.Abs(targetDir)
	if err != nil {
		return Result{}, fmt.Errorf("resolve target dir: %w", err)
	}

	planned := make([]plannedWrite, 0, len(files))
	var result Result

	for _, file := range files {
		content, err := assetbundle.FS.ReadFile(file.EmbeddedPath)
		if err != nil {
			return Result{}, fmt.Errorf("read bundled asset %q: %w", file.EmbeddedPath, err)
		}

		targetPath := filepath.Join(targetDir, filepath.FromSlash(file.TargetPath))
		status, mergedContent, err := compareTarget(targetPath, file.TargetPath, content)

		switch status {
		case StatusCreate:
			if err != nil {
				return Result{}, err
			}
			result.Created = append(result.Created, file.TargetPath)
			planned = append(planned, plannedWrite{
				path:    targetPath,
				content: mergedContent,
				mode:    file.Mode,
			})
		case StatusUpdate:
			if err != nil {
				return Result{}, err
			}
			result.Updated = append(result.Updated, file.TargetPath)
			planned = append(planned, plannedWrite{
				path:    targetPath,
				content: mergedContent,
				mode:    file.Mode,
			})
		case StatusReuse:
			if err != nil {
				return Result{}, err
			}
			result.Reused = append(result.Reused, file.TargetPath)
		case StatusConflict:
			result.Conflicts = append(result.Conflicts, file.TargetPath)
		default:
			if err != nil {
				return Result{}, err
			}
			return Result{}, fmt.Errorf("unsupported install status %q", status)
		}
	}

	sortResult(&result)
	if result.HasConflicts() {
		return result, &ConflictError{Paths: slices.Clone(result.Conflicts)}
	}

	for _, write := range planned {
		if err := os.MkdirAll(filepath.Dir(write.path), 0o755); err != nil {
			return Result{}, fmt.Errorf("create parent directory for %q: %w", write.path, err)
		}

		if err := os.WriteFile(write.path, write.content, write.mode); err != nil {
			return Result{}, fmt.Errorf("write %q: %w", write.path, err)
		}
	}

	return result, nil
}

type plannedWrite struct {
	path    string
	content []byte
	mode    fs.FileMode
}

func compareTarget(absolutePath string, relativePath string, expected []byte) (Status, []byte, error) {
	info, err := os.Stat(absolutePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			normalized, normalizeErr := NormalizeManagedContent(relativePath, nil, expected)
			if normalizeErr != nil {
				return StatusConflict, nil, fmt.Errorf("normalize %q: %w", relativePath, normalizeErr)
			}
			return StatusCreate, normalized, nil
		}
		return "", nil, fmt.Errorf("stat %q: %w", absolutePath, err)
	}

	if info.IsDir() {
		return StatusConflict, nil, nil
	}

	actual, err := os.ReadFile(absolutePath)
	if err != nil {
		return "", nil, fmt.Errorf("read existing file %q: %w", absolutePath, err)
	}

	normalized, err := NormalizeManagedContent(relativePath, actual, expected)
	if err != nil {
		return StatusConflict, nil, fmt.Errorf("normalize %q: %w", relativePath, err)
	}

	if bytes.Equal(actual, normalized) {
		return StatusReuse, normalized, nil
	}

	return StatusUpdate, normalized, nil
}

func sortResult(result *Result) {
	slices.Sort(result.Created)
	slices.Sort(result.Updated)
	slices.Sort(result.Reused)
	slices.Sort(result.Conflicts)
}
