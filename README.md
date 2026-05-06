# reasond

`reasond` is a local-first reasoning audit viewer for coding-agent sessions.
It installs repository-local audit hooks for Codex and Claude Code, indexes the generated markdown audit files, runs judge models against them, and exposes the results in a TUI.
It also exposes a small CLI for coding agents to inspect judge feedback during debugging and code review work.

## Why reasond

- Local repository auditing for coding-agent reasoning traces
- TUI-first workflow for installation, processing, and review
- Dual-provider judging with separate Codex and Claude result boards
- No extra APIs usage or access tokens, just use your already installed Codex or Claude Code.
- Defaults to the cheapest models (Haiku, GPT-Mini, etc.). They work great for this task.
- Immutable archived-audit indexing with SQLite-backed findings
- Merge-safe local install for managed hooks, skills, and context blocks

## Quick start

```bash
# Install with Homebrew:
brew tap rpcarvs/reasond
brew install reasond

# Or install directly from GitHub:
go install github.com/rpcarvs/reasond@latest

# Check the installed version:
reasond -v

# Run inside the repository you want to audit:
cd /path/to/repo
reasond init
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

## Agent CLI

Coding agents can use reasond directly during code reviews, debugging, and bug fixing.

```bash
# Print agent-facing workflow instructions.
reasond onboard

# Judge archived audits that have not been judged yet.
reasond judge

# Print findings from the latest judge run.
reasond latest

# Print all stored findings.
reasond list

# Print one finding detail.
reasond show codex:12
```

`reasond init` installs Codex and/or Claude Code assets and writes the local
default judge provider/model to `.reasond/settings.json`. `reasond judge` uses
that local default. `reasond judge --all` re-judges every indexed audit source
and is usually not needed.

The compact result commands print provider-qualified IDs such as `codex:12` or
`claude:7`. Detail output includes the full archived markdown path under
`.reasond/reasond_audits/` so an agent can read the full reasoning log when
needed.

## System dependencies

`reasond` relies on common UNIX command-line programs.

Required for normal repository use:

- `bash`
- `git`
- `jq`
- `tr`
- `uuidgen`

Provider-specific CLIs:

- `codex` if you install or run the Codex integration
- `claude` if you install or run the Claude Code integration

## Install behavior

`reasond` manages repository-local files for the selected provider:

- `.codex/` or `.claude/`
- `AGENTS.md` managed context blocks
- `CLAUDE.md` pointer to `AGENTS.md` when installing Claude Code assets
- `.reasond/`
- `.reasond_tmp/`
- `.gitignore` entries for `.reasond/` and `.reasond_tmp/`

Install is merge-safe and idempotent and both Codex and Claude assets can coexist in the same repository.

## Runtime layout

`reasond` stores repository-local state in:

- `.reasond/audits_reports.db`
  SQLite database for indexed sources and judge findings.
- `.reasond/settings.json`
  Local default judge provider/model used by agent-facing CLI commands.
- `.reasond/reasond_audits/`
  Canonical immutable markdown audit archive used by indexing, judging, and the source viewer.
- `.reasond_tmp/`
  Transient staging area where agents write new markdown before the stop hook archives it.

The TUI board defaults to the most recently used provider and shows the latest run per source file for that provider. `a` toggles between latest-only and all runs. Raw judge output is not persisted; `reasond` stores only normalized findings.

## Judge providers

`reasond` currently supports two headless judge runners:

- Codex
- Claude Code

The TUI lets the user choose the judge provider and model independently of the provider that originally generated the archived audit markdown.

## Keybindings

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
