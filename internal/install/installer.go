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

	assetbundle "rdit/cmd/assets"
)

type Status string

const (
	StatusCreate   Status = "create"
	StatusReuse    Status = "reuse"
	StatusConflict Status = "conflict"
)

type ItemResult struct {
	Path   string
	Status Status
}

type Result struct {
	Created   []string
	Reused    []string
	Conflicts []string
}

// HasConflicts reports whether the install result includes conflicting paths.
func (r Result) HasConflicts() bool {
	return len(r.Conflicts) > 0
}

// ConflictError indicates that init cannot proceed without overwriting user files.
type ConflictError struct {
	Paths []string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("install conflicts detected: %s", strings.Join(e.Paths, ", "))
}

type Installer struct{}

// Install copies the provider assets into targetDir without overwriting differing files.
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
		status, err := compareTarget(targetPath, content)
		if err != nil {
			return Result{}, err
		}

		switch status {
		case StatusCreate:
			result.Created = append(result.Created, file.TargetPath)
			planned = append(planned, plannedWrite{
				path:    targetPath,
				content: content,
				mode:    file.Mode,
			})
		case StatusReuse:
			result.Reused = append(result.Reused, file.TargetPath)
		case StatusConflict:
			result.Conflicts = append(result.Conflicts, file.TargetPath)
		default:
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

func compareTarget(targetPath string, expected []byte) (Status, error) {
	info, err := os.Stat(targetPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return StatusCreate, nil
		}
		return "", fmt.Errorf("stat %q: %w", targetPath, err)
	}

	if info.IsDir() {
		return StatusConflict, nil
	}

	actual, err := os.ReadFile(targetPath)
	if err != nil {
		return "", fmt.Errorf("read existing file %q: %w", targetPath, err)
	}

	if bytes.Equal(actual, expected) {
		return StatusReuse, nil
	}

	return StatusConflict, nil
}

func sortResult(result *Result) {
	slices.Sort(result.Created)
	slices.Sort(result.Reused)
	slices.Sort(result.Conflicts)
}
