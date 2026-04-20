package judge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CodexRunner executes Codex in non-interactive mode for structured judging.
type CodexRunner struct {
	BinaryPath string
}

// Run executes Codex in headless mode and parses the structured findings response.
func (r CodexRunner) Run(ctx context.Context, rootDir, model, auditMarkdown string) (Response, error) {
	if strings.TrimSpace(model) == "" {
		return Response{}, fmt.Errorf("codex model is required")
	}

	rootDir, err := filepath.Abs(rootDir)
	if err != nil {
		return Response{}, fmt.Errorf("resolve root dir: %w", err)
	}

	schemaPath, err := WriteSchema(rootDir)
	if err != nil {
		return Response{}, err
	}

	outputFile, err := os.CreateTemp("", "rdit-codex-last-*.json")
	if err != nil {
		return Response{}, fmt.Errorf("create codex output temp file: %w", err)
	}
	outputPath := outputFile.Name()
	if err := outputFile.Close(); err != nil {
		return Response{}, fmt.Errorf("close codex output temp file: %w", err)
	}
	defer func() {
		_ = os.Remove(outputPath)
	}()

	binaryPath := r.BinaryPath
	if binaryPath == "" {
		binaryPath = "codex"
	}

	cmd := exec.CommandContext(
		ctx,
		binaryPath,
		"exec",
		"--ephemeral",
		"--model", model,
		"--output-schema", schemaPath,
		"--output-last-message", outputPath,
		BuildPrompt(auditMarkdown),
	)
	cmd.Dir = rootDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return Response{}, fmt.Errorf("run codex judge: %w: %s", err, strings.TrimSpace(string(output)))
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		return Response{}, fmt.Errorf("read codex output file: %w", err)
	}

	var response Response
	if err := json.Unmarshal(content, &response); err != nil {
		return Response{}, fmt.Errorf("decode codex output: %w", err)
	}

	return response, nil
}
