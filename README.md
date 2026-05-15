# reasond

`reasond` is a local-first reasoning audit archive, judge, and review tool for
coding-agent sessions. It installs repository-local assets for supported agent
tools, archives reasoning audits as markdown, runs configurable judge models
against those audits, and exposes the results through both a TUI and a small
agent-facing CLI.

## Why reasond

- Local repository auditing for coding-agent reasoning traces
- Human setup plus agent-consumable judge feedback in the same repository
- TUI and CLI workflows for installation, processing, and review
- Three judge providers: Ollama, Codex, and Claude Code
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

# Initialize inside the repository you want to audit:
cd /path/to/repo
reasond init

# Open the TUI:
reasond
```

Typical human workflow:

1. Run `reasond init` or press `i` inside the TUI to install Codex and/or
   Claude Code assets and choose the default judge.
2. Run coding-agent sessions in the repository so audits are archived under
   `.reasond/reasond_audits/`.
3. Return to `reasond` to process new audits, inspect findings, switch judge
   providers, and review source markdown.

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
and is usually not needed. `reasond judge --timeout <minutes>` overrides the
Ollama request timeout for one batch run and defaults to 15 minutes.
All CLI and TUI flows resolve the Git repository root before reading or writing
`.reasond` state, so running from a subdirectory still uses the same
repository-local data.

Typical agent workflow:

1. Use `reasond onboard` to print the local repository workflow reminder.
2. Run `reasond judge` when new archived audits need to be judged.
3. Inspect the latest results with `reasond latest`, `reasond list`, or
   `reasond show <provider:id>`.

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

- `ollama` if you want to use local Ollama models as judge providers
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

Install is merge-safe and idempotent. Codex and Claude assets can coexist in
the same repository, and Ollama can be selected as the judge provider without
installing an additional repository asset bundle.

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

The TUI board defaults to the most recently used provider and shows the latest
run per source file for that provider. `a` toggles between latest-only and all
runs. Long finding lists scroll inside the board instead of rendering as one
unbounded page. Raw judge output is not persisted; `reasond` stores only
normalized findings.

## Judge providers

`reasond` currently supports three judge providers:

- Ollama (local)
- Codex
- Claude Code

The TUI lets the user choose the judge provider and model independently of the provider that originally generated the archived audit markdown.

### Ollama

Ollama is a local judge provider. `reasond` does not install Ollama or pull
models for you. Install Ollama separately, pull at least one local model, and
make sure the selected model fits your available RAM or VRAM.

Useful commands before choosing Ollama in `reasond init` or the TUI:

```bash
ollama list
ollama pull glm4:9b
```

Operational notes for Ollama judging:

- `reasond` sends one independent `/api/chat` request per audit file.
- Current Ollama judge requests use `OLLAMA_CONTEXT_LENGTH` when that
  environment variable is set. Otherwise reasond falls back to `num_ctx=8192`.
- `OLLAMA_KEEP_ALIVE` controls how long the loaded model stays resident in
  memory. Ollama defaults to 5 minutes globally, but `reasond` sends
  `keep_alive=10m` on judge requests when no override is set.
- `OLLAMA_NUM_PARALLEL` is an Ollama server setting, not a `reasond` setting.
  If your machine can support it, `OLLAMA_NUM_PARALLEL=2` is a practical tuning
  value for medium local models and allows two same-model requests to run at
  once.
- Larger `num_ctx` values and higher `OLLAMA_NUM_PARALLEL` values increase
  memory usage. If judging becomes unstable, reduce parallelism first or choose
  a smaller model.

Example Ollama server tuning:

```bash
export OLLAMA_NUM_PARALLEL=2
export OLLAMA_KEEP_ALIVE=10m
ollama serve
```

## TUI Keybindings

- `up/down` or `j/k` move through findings
- `enter` open the finding detail modal
- `tab` switch between provider boards
- `a` toggle latest-only versus all runs
- `f` filter the board by source file
- `r` choose a judge provider/model and re-run all indexed files
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
