#!/usr/bin/env bash

# reasoning-audit-stop.sh (Codex version)
# Stop hook: detect new audit files using the .control ledger.
# If a new file exists, prepend all accumulated prompts into it.
# If no new file, do nothing (prompts keep accumulating).

REPO_ROOT=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
AUDIT_DIR="$REPO_ROOT/.reasond_tmp"
ARCHIVE_DIR="$REPO_ROOT/.reasond/reasond_audits"

if [[ ! -d "$AUDIT_DIR" ]]; then
  exit 0
fi

mkdir -p "$ARCHIVE_DIR"

PENDING="$AUDIT_DIR/.pending_prompt"
CONTROL="$AUDIT_DIR/.control"

# Ensure .control exists
touch "$CONTROL"

# Get all .md files, subtract already-processed ones from .control
TARGET=""
for F in $(ls -t "$AUDIT_DIR"/*.md 2>/dev/null || true); do
  BASENAME=$(basename "$F")
  if ! grep -qxF "$BASENAME" "$CONTROL" 2>/dev/null; then
    TARGET="$F"
    break
  fi
done

# No new audit file — skill hasn't run yet, do nothing
if [[ -z "$TARGET" ]]; then
  exit 0
fi

TARGET_BASENAME=$(basename "$TARGET")
ARCHIVE_TARGET="$ARCHIVE_DIR/$TARGET_BASENAME"

# Prepend prompts if we have any and the file hasn't been stamped yet
if [[ -f "$PENDING" ]] && ! grep -q '^# User Prompt' "$TARGET" 2>/dev/null; then
  TMPFILE=$(mktemp)
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

# Mark this file as processed
echo "$TARGET_BASENAME" >>"$CONTROL"
rm -f "$PENDING"

exit 0
