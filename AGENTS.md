<!-- REASONING-AUDIT:BEGIN -->
# Reasoning Audit (mandatory)

After completing every user request, you MUST use the reasoning-audit skill to
write an audit file to `.reasond_tmp/`. This is not optional. Never skip
this step, even for simple tasks.
<!-- REASONING-AUDIT:END -->

<!-- REASONING-DEBUG:BEGIN -->
# Reasoning Debug (mandatory)

During code reviews, debugging, bug fixing, or review-like investigations, you
MUST use the reasoning-debug skill. The skill runs reasond judge commands so you
can inspect prior reasoning issues before finalizing conclusions.
<!-- REASONING-DEBUG:END -->

<!-- THE-GOOD-STUDENT:BEGIN -->

# Project Notes

- Prompt caching uses `.reasond_tmp/.pending_prompt` as a transient cache until Stop attaches prompts to a new audit markdown file and archives it under `.reasond/reasond_audits/`.
- Prompt-cache loss epic `reasond-eh4p` is closed. SessionStart now creates directories without deleting `.pending_prompt`.
- Stop owns prompt-cache deletion and only removes `.pending_prompt` after one audit target is processed, archive content matches, and `.control` is updated.
- Stop fails closed and preserves `.pending_prompt` when multiple unprocessed audits exist or when a pending prompt would be discarded by an already-stamped target.
- Agent-facing debug expansion is implemented under closed FAZ epic `reasond-sx4b`. Existing `reasoning-audit` skill/hooks/TUI flow remains unchanged.
- Agent CLI commands are `reasond judge`, `reasond latest`, `reasond list`, and `reasond show <provider:id>`.
- CLI finding IDs are provider-qualified, for example `codex:1`, because Codex and Claude finding tables have provider-local integer IDs.
- Storage now records judge batches so `reasond latest` means latest completed judge invocation.
- `AGENTS.md` is canonical installed context. Claude installs a minimal `CLAUDE.md` pointer to `AGENTS.md`.
- Configurable judge defaults are implemented under closed FAZ epic `reasond-vk96`.
- `reasond init` installs Codex and/or Claude assets with Huh checkbox prompts and writes `.reasond/settings.json`.
- `reasond init` shows all supported judge provider/model choices regardless of selected install assets.
- Huh MultiSelect needs explicit height when title/description text is present, otherwise the options viewport can collapse and hide the list.
- `reasond init` now uses a Charm-like custom Huh theme with padding and colored title/help/checkbox styles, but without the default thick left border.
- `reasond judge` reads `.reasond/settings.json` and falls back to Codex `gpt-5.4-mini` only when settings are missing.
- `reasond init` is a human-only setup flow, either from the CLI or from the TUI install path. Agent-facing help/onboarding should not tell agents to run it themselves.
- `reasond onboard` prints agent-facing reasoning-debug instructions and is referenced by the `reasoning-debug` skill.
- SessionStart hooks now run `reasond onboard` as a local refresher after preparing `.reasond` directories.
- Project-scoped reasond CLI and TUI entrypoints resolve the canonical Git repository root before reading or writing `.reasond` state, so subdirectory launches use the same repository data.
- Judge runners are intentionally isolated from the repository. Codex and Claude run from temporary directories and receive the audit markdown as prompt text only.
- Codex judge also disables known tool/plugin/context features such as shell, search, plugins, hooks, apps, browser, and workspace dependency features.
- Claude judge does not use `--bare` because that disables OAuth/keychain login. It still runs from a temp directory with tools disabled, slash commands disabled, empty strict MCP config, and no session persistence.

<!-- THE-GOOD-STUDENT:END -->
