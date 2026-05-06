package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// currentProjectDir resolves the Git repository root for project-scoped commands.
func currentProjectDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	return gitRootDir(cwd)
}

// gitRootDir returns the absolute Git top-level directory for a path.
func gitRootDir(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("reasond requires a Git repository. Run `git init` first")
	}

	root := strings.TrimSpace(string(output))
	if root == "" {
		return "", fmt.Errorf("git repository root was empty")
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve git repository root: %w", err)
	}
	return resolvedRoot, nil
}
