package testutil

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// CopyFixtureTree copies one directory from testdata/fixtures into a temp dir and returns the temp path.
func CopyFixtureTree(t *testing.T, relativePath string) string {
	t.Helper()

	sourceRoot := fixturePath(t, relativePath)
	destinationRoot := t.TempDir()

	err := filepath.Walk(sourceRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relative, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}

		target := filepath.Join(destinationRoot, relative)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		sourceFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() {
			_ = sourceFile.Close()
		}()

		targetFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer func() {
			_ = targetFile.Close()
		}()

		_, err = io.Copy(targetFile, sourceFile)
		return err
	})
	if err != nil {
		t.Fatalf("copy fixture tree %q: %v", relativePath, err)
	}

	return destinationRoot
}

func fixturePath(t *testing.T, relativePath string) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("resolve fixture helper path")
	}

	return filepath.Join(filepath.Dir(currentFile), "..", "..", "testdata", "fixtures", filepath.FromSlash(relativePath))
}
