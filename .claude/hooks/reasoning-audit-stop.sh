#!/usr/bin/env bash

# reasoning-audit-stop.sh (Claude Code version)
# Stop hook: detect new audit files using the .control ledger.
# If a new file exists, prepend all accumulated prompts into it.
# If no new file, do nothing (prompts keep accumulating).

set -euo pipefail

AUDIT_DIR="${CLAUDE_PROJECT_DIR:-}"
if [[ -z "$AUDIT_DIR" ]]; then
  exit 0
fi

AUDIT_DIR="$AUDIT_DIR/.reasond_tmp"
ARCHIVE_DIR="${CLAUDE_PROJECT_DIR}/.reasond/reasond_audits"
if [[ ! -d "$AUDIT_DIR" ]]; then
  exit 0
fi

mkdir -p "$ARCHIVE_DIR"

PENDING="$AUDIT_DIR/.pending_prompt"
CONTROL="$AUDIT_DIR/.control"

# Ensure .control exists
touch "$CONTROL"

# Get all .md files, subtract already-processed ones from .control.
shopt -s nullglob
CANDIDATES=()
for F in "$AUDIT_DIR"/*.md; do
  BASENAME=$(basename "$F")
  if ! grep -qxF "$BASENAME" "$CONTROL" 2>/dev/null; then
    CANDIDATES+=("$F")
  fi
done
shopt -u nullglob

# No new audit file — skill hasn't run yet, do nothing
if [[ ${#CANDIDATES[@]} -eq 0 ]]; then
  exit 0
fi

if [[ ${#CANDIDATES[@]} -gt 1 ]]; then
  echo "reasond: multiple unprocessed audit files; preserving pending prompt" >&2
  exit 1
fi

TARGET="${CANDIDATES[0]}"
TARGET_BASENAME=$(basename "$TARGET")
ARCHIVE_TARGET="$ARCHIVE_DIR/$TARGET_BASENAME"

# Prepend prompts if we have any. If the target is already stamped while prompts
# are pending, fail closed so a newer prompt is not silently discarded.
if [[ -s "$PENDING" ]]; then
  if grep -q '^# User Prompt' "$TARGET" 2>/dev/null; then
    echo "reasond: audit already has a user prompt; preserving pending prompt" >&2
    exit 1
  fi

  TMPFILE=$(mktemp "$AUDIT_DIR/.prompt.XXXXXX")
  {
    echo "# User Prompt"
    echo ""
    cat "$PENDING"
    cat "$TARGET"
  } >"$TMPFILE"
  mv "$TMPFILE" "$TARGET"
fi

if [[ -e "$ARCHIVE_TARGET" ]]; then
  if ! cmp -s "$TARGET" "$ARCHIVE_TARGET"; then
    exit 1
  fi
else
  TMP_ARCHIVE=$(mktemp "$ARCHIVE_DIR/.archive.XXXXXX")
  cp "$TARGET" "$TMP_ARCHIVE"
  mv "$TMP_ARCHIVE" "$ARCHIVE_TARGET"
fi

cmp -s "$TARGET" "$ARCHIVE_TARGET"

# Mark this file as processed
printf '%s\n' "$TARGET_BASENAME" >>"$CONTROL"
rm -f "$PENDING"

exit 0
