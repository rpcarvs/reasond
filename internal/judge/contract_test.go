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
	if !strings.Contains(prompt, audit) {
		t.Fatalf("expected prompt to include audit log contents")
	}
}
