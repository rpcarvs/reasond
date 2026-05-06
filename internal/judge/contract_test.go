package judge

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSchemaIsValidAndWritable(t *testing.T) {
	t.Parallel()

	if !json.Valid([]byte(Schema())) {
		t.Fatalf("schema is not valid JSON")
	}

	root := t.TempDir()
	schemaPath, err := WriteSchema(root)
	if err != nil {
		t.Fatalf("write schema: %v", err)
	}

	content, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read schema file: %v", err)
	}
	if string(content) != Schema() {
		t.Fatalf("schema file content mismatch")
	}
}

func TestBuildPromptAllowsZeroFindingsAndIncludesAudit(t *testing.T) {
	t.Parallel()

	audit := "# User Prompt\n\nDo the task.\n\n# Reasoning by Codex\n\nThe agent did X."
	prompt := BuildPrompt(audit)

	if !strings.Contains(prompt, `{"findings":[]}`) {
		t.Fatalf("expected prompt to include zero-findings example")
	}
	if !strings.Contains(prompt, "Do not invent issues") {
		t.Fatalf("expected prompt to forbid fabricated issues")
	}
	for _, expected := range []string{
		"You are NOT ALLOWED to investigate the codebase",
		"Do not read any code or use any skill or tool",
		"Do not use MCP servers",
		"the complete and only source of evidence",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected prompt to contain no-codebase rule %q", expected)
		}
	}
	if !strings.Contains(prompt, audit) {
		t.Fatalf("expected prompt to include audit log contents")
	}
}

func TestBuildPromptIncludesScoringRubricAndExamples(t *testing.T) {
	t.Parallel()

	prompt := BuildPrompt("# Reasoning by Codex\n\nTest.")
	for _, expected := range []string{
		"Scoring rubric:",
		"0.00 to 0.19",
		"0.40 to 0.59",
		"0.80 to 1.00",
		"Scoring examples:",
		"Use about 0.50 for missing a likely bug",
		"reporting success after a failed validation",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected prompt to contain %q", expected)
		}
	}
}
