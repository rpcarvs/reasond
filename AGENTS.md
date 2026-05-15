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
- Codex install/init no longer edits `~/.codex/config.toml` to enable hooks; it only installs repository-managed Codex assets because hooks are enabled by default upstream.
- `reasond init` still installs only Codex and/or Claude assets, but CLI judge selection is now a separate two-step flow: pick the default judge harness first, then pick a model for that harness. The CLI defaults the harness choice to `ollama`.
- Huh MultiSelect needs explicit height when title/description text is present, otherwise the options viewport can collapse and hide the list.
- `reasond init` now uses a Charm-like custom Huh theme with padding and colored title/help/checkbox styles, but without the default thick left border.
- `reasond judge` reads `.reasond/settings.json` and falls back to Codex `gpt-5.4-mini` only when settings are missing.
- `reasond init` is a human-only setup flow, either from the CLI or from the TUI install path. Agent-facing help/onboarding should not tell agents to run it themselves.
- `reasond onboard` prints agent-facing reasoning-debug instructions and is referenced by the `reasoning-debug` skill.
- SessionStart hooks now run `reasond onboard` as a local refresher after preparing `.reasond` directories.
- Project-scoped reasond CLI and TUI entrypoints resolve the canonical Git repository root before reading or writing `.reasond` state, so subdirectory launches use the same repository data.
- Init now asks whether reasond files should be git ignored. The setting is persisted as `git_ignore_reasond` in `.reasond/settings.json`, defaults to `true`, and both CLI and TUI init paths honor it.
- Runtime bootstrap no longer mutates `.gitignore` unconditionally. Agent CLI runtime setup and integrity checks read the saved gitignore preference before deciding whether `.reasond/` and `.reasond_tmp/` are required in `.gitignore`.
- Agent-facing CLI commands call a shared `openAgentStore` path that ensures runtime layout, opens the SQLite store, and syncs archived markdown audits before reading or judging findings.
- The root CLI keeps the bare `reasond` TUI entrypoint and hides `help [command]`, but `completion` is enabled so shells can generate autocompletion scripts.
- Investigation under closed FAZ epic `reasond-9fvz` found no successful `reasond` CLI command routing normal output to stderr. The only non-test stderr messages in shipped commands are fail-closed Stop hook warnings in the Codex and Claude hook scripts, both paired with `exit 1`.
- Judge execution is already parallelized in `internal/processing.Processor`: it uses a bounded worker pool with default concurrency `4`, and `Bootstrap.NewProcessor` relies on that default for both CLI and TUI judge flows.
- Current judge slowness is therefore not caused by a serial per-file loop in reasond. The remaining likely bottlenecks are provider/API latency, subprocess startup cost, and provider-side rate limiting. If tuning is needed, prefer configurable bounded concurrency over one-goroutine-per-file fanout.
- Empirical follow-up under epic `reasond-5xjf` confirmed real headless overlap outside the fake-runner tests: isolated `codex exec` runs completed in about `15s` sequential versus `8s` parallel for two requests, and isolated `claude --print` runs completed in about `8s` sequential versus `3s` parallel for two requests.
- The CLI/TUI progress display only emits updates on completion, not on work start, so judge output inherently looks one-file-at-a-time even when multiple worker goroutines are already running provider subprocesses.
- Judge runners are intentionally isolated from the repository. Codex and Claude run from temporary directories and receive the audit markdown as prompt text only.
- Codex judge also disables known tool/plugin/context features such as shell, search, plugins, hooks, apps, browser, and workspace dependency features.
- Claude judge does not use `--bare` because that disables OAuth/keychain login. It still runs from a temp directory with tools disabled, slash commands disabled, empty strict MCP config, and no session persistence.
- Ollama is now a first-class judge provider. It is not an installable asset bundle; it uses a small local HTTP client against the Ollama API, honors `OLLAMA_HOST`, discovers installed models from `/api/tags`, and judges audits with structured output through `/api/chat`.
- The Ollama judge runner currently applies a hardcoded `2m` HTTP client timeout to both `/api/tags` and `/api/chat`. Large local models can therefore fail before first response bytes arrive, even though the processor and CLI judge flow do not impose their own deadlines.
- Ollama judge requests now send `keep_alive` explicitly. The value comes from `OLLAMA_KEEP_ALIVE` when set, otherwise reasond uses `10m` as the batch-friendly default.
- The default Ollama HTTP request timeout is now `15m`. The CLI `reasond judge` command also accepts `--timeout <minutes>` to override that timeout for one batch run.
- During one `reasond judge` batch, Ollama judging still sends one independent `/api/chat` request per audit file rather than maintaining a shared session. Model residency is therefore improved by `keep_alive`, but execution still depends on Ollama server queueing and concurrency settings.
- Ollama settings validation is intentionally looser than Codex/Claude validation: saved Ollama models only need to be non-empty because the local installed-model set can change after init.
- The TUI runtime judge chooser now loads provider models lazily when a provider is selected and caches them for that interaction, so dynamic providers like Ollama do not trigger model discovery on every render.
- The judged-audits board now renders a height-aware viewport around the selected finding instead of dumping the entire provider list at once, so long finding lists remain navigable in the TUI.
- When a judge run ends with an error, the TUI now keeps the processing modal visible with an explicit failure title and dismissal hint instead of dropping straight back to the board. This makes early runner failures, including slow Ollama startup timeouts, visible rather than feeling like a silent exit.
- Board-tab switching should skip judge providers that have no stored results, so adding Ollama does not force users through empty provider partitions when only Codex or Claude findings exist.
- Measured current audit samples in this repository produce roughly 4481 to 9920 total request characters when combining the judge prompt wrapper, schema payload, and archived audit markdown. That is roughly 1120 to 3306 tokens under simple 4-char and 3-char approximation bounds, so Ollama judge requests now use `OLLAMA_CONTEXT_LENGTH` when set and otherwise fall back to `num_ctx: 8192` for safer headroom over the current audit set.
- The README now documents Ollama as a local judge provider, including local model installation expectations, RAM-fit guidance, `OLLAMA_KEEP_ALIVE`, `OLLAMA_NUM_PARALLEL`, the `OLLAMA_CONTEXT_LENGTH` override plus `8192` fallback, and the `reasond judge --timeout` override.
- The README narrative has been refreshed to match the current product shape: human setup plus agent consumption, TUI and CLI workflows, Git-root-aware state, three judge providers, and scrollable long-list review in the TUI.
- Exported Go comments were refreshed selectively rather than rewritten wholesale. The main corrections cover repository-local settings semantics, dynamic versus static judge model catalogs, and the Ollama runner request defaults and overrides.
- Judge-provider hardcoding is currently spread across three architectural concerns: duplicated provider catalog data in `settings`, `cmd/init`, and `internal/tui`; necessary runner dispatch in `processing`/`bootstrap`; and provider-partitioned persistence logic in `storage`. Only the existence of a dispatch point is inherently necessary. The duplication and fixed two-provider loops are implementation artifacts.
- The recommended refactor scope under epic `reasond-sx58` is targeted, not a full storage redesign: centralize provider metadata, make runner dispatch registry-based, and drive provider-partitioned storage loops from shared provider definitions. Keep the current per-provider table model for now unless a narrower change proves impossible.
- Planning for Ollama work is now split into three epics: `reasond-9smx` for Ollama judge support, `reasond-esp1` for judge-selection UX, and `reasond-sx58` for the prerequisite judge-architecture refactor. Combined planning epic `reasond-cseq` was closed as superseded.
- Blast-radius investigation under `reasond-sx58.4` found the targeted judge-provider refactor touches five main files directly: `internal/settings/settings.go`, `internal/processing/processor.go`, `internal/app/bootstrap.go`, `internal/storage/store.go`, and `internal/tui/model.go`, plus CLI helpers in `cmd/init.go` and tests across `cmd/root_test.go`, `internal/settings/settings_test.go`, `internal/processing/processor_test.go`, `internal/storage/store_test.go`, and `internal/tui/model_test.go`.
- The highest regression risk is not runner dispatch. It is preserving exact behavior in provider ordering, default model selection, provider-qualified finding IDs, latest/list/show query semantics, board-provider recency, and storage migration/bootstrap behavior. Those behaviors are heavily asserted by existing tests and should remain unchanged while the source of truth is centralized.
- Integrity and install flows should remain out of scope for the judge-provider refactor. They are intentionally about managed Codex/Claude asset bundles, not judge providers. The only judge-related coupling there is `settings.Load`, which must continue to work with existing defaults and saved repositories.
- FAZ planning now encodes the refactor compatibility contract directly in epic and task descriptions across `reasond-sx58`, `reasond-9smx`, and `reasond-esp1`, so implementation work is explicitly constrained to preserve existing Codex/Claude semantics while adding Ollama and improving judge-selection UX.
- Closed epic `reasond-sx58` implemented the targeted judge-provider refactor without changing current Codex/Claude behavior. Judge-provider metadata now lives in `internal/judge/providers.go`, processor dispatch uses a provider-to-runner registry, and storage provider loops/table resolution derive from the shared judge catalog while keeping the current per-provider table schema intact.
- The refactor preserved the compatibility-sensitive behaviors identified during investigation: canonical provider strings, current default fallback behavior, provider-qualified IDs, latest/list/show semantics, board-provider recency, preferred provider with visible findings, and existing SQLite bootstrap/migration behavior. Full `go test ./...` passed after the change.

<!-- THE-GOOD-STUDENT:END -->
