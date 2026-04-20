#!/usr/bin/env bash

# reasoning-audit-prompt.sh (Codex version)
# UserPromptSubmit hook: append the user prompt to the pending file.
# Prompts accumulate until a Stop hook successfully processes them.

REPO_ROOT=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
AUDIT_DIR="$REPO_ROOT/reasoning_audits"

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
