These fixtures support installer, storage, processing, and TUI tests.

Layout:
- `audits/`
  Sample reasoning audit markdown files.
- `repos/clean/`
  Minimal repository baseline with no rdit assets installed.
- `repos/conflict-agents/`
  Repository fixture with a conflicting `AGENTS.md`.
- `repos/partial-codex/`
  Repository fixture with only part of the Codex installation present.

Notes:
- Audit fixtures are designed to cover no-issue, multiple-issue, and simple-pending scenarios.
- Repo fixtures are intentionally incomplete so tests can assert safe merge and integrity behavior.
