package judge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ClaudeRunner executes Claude Code in print mode for structured judging.
type ClaudeRunner struct {
	BinaryPath string
}

type claudePrintEnvelope struct {
	StructuredOutput Response `json:"structured_output"`
}

// Run executes Claude Code in print mode and parses the structured findings response.
func (r ClaudeRunner) Run(ctx context.Context, rootDir, model, auditMarkdown string) (Response, error) {
	if strings.TrimSpace(model) == "" {
		return Response{}, fmt.Errorf("claude model is required")
	}
	_ = rootDir

	judgeDir, err := os.MkdirTemp("", "reasond-claude-judge-*")
	if err != nil {
		return Response{}, fmt.Errorf("create isolated claude judge directory: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(judgeDir)
	}()

	binaryPath := r.BinaryPath
	if binaryPath == "" {
		binaryPath = "claude"
	}

	args := []string{
		"--print",
		"--model", model,
		"--json-schema", Schema(),
		"--no-session-persistence",
		"--output-format", "json",
		"--disable-slash-commands",
		"--tools", "",
		"--mcp-config", `{"mcpServers":{}}`,
		"--strict-mcp-config",
	}
	args = append(args, BuildPrompt(auditMarkdown))

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Dir = judgeDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return Response{}, fmt.Errorf("run claude judge: %w: %s", err, strings.TrimSpace(string(output)))
	}

	var envelope claudePrintEnvelope
	if err := json.Unmarshal(output, &envelope); err != nil {
		return Response{}, fmt.Errorf("decode claude output: %w", err)
	}

	return envelope.StructuredOutput, nil
}
