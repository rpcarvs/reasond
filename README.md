# reasond

`reasond` is a local-first reasoning audit viewer for coding-agent sessions.
It installs repository-local audit hooks for Codex and Claude Code, indexes the generated markdown audit files, runs judge models against them, and exposes the results in a TUI.

## Why reasond

- Local repository auditing for coding-agent reasoning traces
- TUI-first workflow for installation, processing, and review
- Dual-provider judging with separate Codex and Claude result boards
- Immutable archived-audit indexing with SQLite-backed findings
- Merge-safe local install for managed hooks, skills, and context blocks

## Quick start

```bash
# Install directly from GitHub:
go install github.com/rpcarvs/reasond@latest

# Check the installed version:
reasond -v

# Run inside the repository you want to audit:
cd /path/to/repo
reasond
```

Inside the TUI:

- Press `i` to install or reinstall Codex or Claude assets for the current repository.
- If `.reasond/reasond_audits/` contains new archived markdown audits, `reasond` prompts to process them.
- Use the board to inspect findings, switch providers, and review source files.

If `reasond` is not found after install, add `$(go env GOPATH)/bin` to your `PATH`.
```bash
echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.bashrc
```

## Install behavior

`reasond` manages repository-local files for the selected provider:

- `.codex/` or `.claude/`
- `AGENTS.md` and `CLAUDE.md` managed blocks
- `.reasond/`
- `.reasond_tmp/`
- `.gitignore` entries for `.reasond/` and `.reasond_tmp/`

Install is merge-safe and idempotent:

- Managed blocks in `AGENTS.md` and `CLAUDE.md` are upserted between `REASONING-AUDIT` markers.
- Managed JSON files such as provider settings and hooks are merged instead of blindly overwritten.
- Provider-managed scripts and skill files are refreshed in place without duplicating entries.
- Codex hook enablement is checked before install proceeds.

Both Codex and Claude assets can coexist in the same repository.

## Runtime layout

`reasond` stores repository-local state in:

- `.reasond/audits_reports.db`
  SQLite database for indexed sources and judge findings.
- `.reasond/reasond_audits/`
  Canonical immutable markdown audit archive used by indexing, judging, and the source viewer.
- `.reasond_tmp/`
  Transient staging area where agents write new markdown before the stop hook archives it.

## Storage model

SQLite stores the audit pipeline in provider-aware tables:

- `audit_sources`
  One row per indexed markdown file under `.reasond/reasond_audits/`.
- `audit_runs_codex`
  One row per Codex judge run for a source file.
- `audit_findings_codex`
  Zero or more Codex findings for a Codex run.
- `audit_runs_claude`
  One row per Claude judge run for a source file.
- `audit_findings_claude`
  Zero or more Claude findings for a Claude run.

Archived source files are immutable after indexing. If a file changes unexpectedly, it is treated as an integrity conflict instead of being overwritten.

The board defaults to the most recently used provider and shows the latest run per source file for that provider. `a` toggles between latest-only and all runs. Raw judge output is not persisted; `reasond` stores only normalized findings in SQLite.

## Judge providers

`reasond` currently supports two headless judge runners:

- Codex
- Claude Code

The TUI lets the user choose the judge provider and model independently of the provider that originally generated the archived audit markdown.

## Core interactions

```text
reasond              Open the full-screen TUI
reasond -v           Print version
reasond --help       Show help
```

Board keybindings:

- `up/down` or `j/k` move through findings
- `enter` open the finding detail modal
- `tab` switch between Codex and Claude boards
- `a` toggle latest-only versus all runs
- `f` filter the board by source file
- `r` re-run all indexed files through a selected judge provider/model
- `i` install or reinstall provider assets for the current repository
- `s` open the state popup
- `q` close the active popup, or quit from the board

Detail and source views:

- `o` open the source markdown in the in-app full-screen viewer
- `up/down` or `j/k` scroll
- `q` close the current view


## Notes

- Processing is issue-driven, not issue-forcing. A judge can return no problems for a source file.
- Re-audits are insert-only. Historical runs remain stored and can be surfaced with the all-runs toggle.
