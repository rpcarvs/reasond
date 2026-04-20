#!/usr/bin/env bash

# reasoning-audit-prompt.sh (Claude Code version)
# UserPromptSubmit hook: append the user prompt to the pending file.
# Prompts accumulate until a Stop hook successfully processes them.

AUDIT_DIR="${CLAUDE_PROJECT_DIR:-}"
if [[ -z "$AUDIT_DIR" ]]; then
  exit 0
fi

AUDIT_DIR="$AUDIT_DIR/reasoning_audits"
if [[ ! -d "$AUDIT_DIR" ]]; then
  exit 0
fi

PROMPT=$(cat | jq -r '.prompt // empty' 2>/dev/null || true)
if [[ -z "$PROMPT" ]]; then
  exit 0
fi

echo "$PROMPT" >>"$AUDIT_DIR/.pending_prompt"
echo "" >>"$AUDIT_DIR/.pending_prompt"

exit 0
