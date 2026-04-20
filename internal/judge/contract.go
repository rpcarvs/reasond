package judge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	appRuntime "rdit/internal/runtime"
)

// SchemaFileName is the runtime filename used for the shared structured-output schema.
const SchemaFileName = "judge_output_schema.json"

// Finding is one normalized judge finding ready to be persisted or shown in the board.
type Finding struct {
	Title string  `json:"title"`
	Issue string  `json:"issue"`
	Why   string  `json:"why"`
	How   string  `json:"how"`
	Score float64 `json:"score"`
}

// Response is the zero-or-many finding payload returned by judge providers.
type Response struct {
	Findings []Finding `json:"findings"`
}

// Runner evaluates one audit markdown file and returns normalized findings.
type Runner interface {
	Run(ctx context.Context, rootDir, model, auditMarkdown string) (Response, error)
}

const schemaJSON = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "findings": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "title": {
            "type": "string",
            "description": "Short listing title that captures the essence of the issue."
          },
          "issue": {
            "type": "string",
            "description": "What problem was identified in the reasoning."
          },
          "why": {
            "type": "string",
            "description": "Why this is a problem and what makes it problematic."
          },
          "how": {
            "type": "string",
            "description": "How the issue connects back to the user prompt or task."
          },
          "score": {
            "type": "number",
            "minimum": 0.0,
            "maximum": 1.0,
            "description": "Criticality score relative to the user prompt."
          }
        },
        "required": ["title", "issue", "why", "how", "score"]
      }
    }
  },
  "required": ["findings"]
}
`

// Schema returns the provider-neutral JSON schema used for structured judge output.
func Schema() string {
	return schemaJSON
}

// SchemaPath returns where the shared judge schema file should live inside .rdit.
func SchemaPath(rootDir string) string {
	return filepath.Join(rootDir, appRuntime.DirectoryName, SchemaFileName)
}

// WriteSchema materializes the schema into .rdit so CLI runners can pass it by path.
func WriteSchema(rootDir string) (string, error) {
	rootDir, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("resolve root dir: %w", err)
	}

	schemaPath := SchemaPath(rootDir)
	if err := os.MkdirAll(filepath.Dir(schemaPath), 0o755); err != nil {
		return "", fmt.Errorf("create runtime directory for schema: %w", err)
	}
	if err := os.WriteFile(schemaPath, []byte(schemaJSON), 0o644); err != nil {
		return "", fmt.Errorf("write schema file: %w", err)
	}

	return schemaPath, nil
}

// BuildPrompt produces the provider-neutral judging prompt for one reasoning audit file.
func BuildPrompt(auditMarkdown string) string {
	return fmt.Sprintf(`You are auditing a coding agent reasoning log against the user prompt captured in the same file.

Your job is to identify only substantive reasoning or prompt-following problems.

Rules:
- You may return zero findings if the reasoning is acceptable.
- Do not invent issues just to fill the schema.
- Focus on failures in prompt following, missing validation, weak reasoning, unsupported assumptions, or harmful execution choices.
- Ignore minor style preferences unless they materially affect task success.
- Base every finding on the contents of the audit log.
- Keep each title short and list-friendly.
- The "how" field must explain the connection to the user prompt or requested task.
- The score must be between 0.0 and 1.0, where higher means more critical relative to the user prompt.

If there are no meaningful problems, return:
{"findings":[]}

Audit log to judge:

%s`, auditMarkdown)
}
