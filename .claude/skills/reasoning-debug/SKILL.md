---
name: reasoning-debug
description: >
  Use reasond during code reviews, debugging, bug fixing, and review-like
  investigations. This skill is mandatory whenever the current work is about
  finding, validating, or explaining defects.
---

# Reasoning Debug Skill

Use reasond to inspect judge feedback from archived reasoning audits before you
finalize debugging or review conclusions.

## Required Flow

1. Read the agent-facing reasond workflow:

```bash
reasond onboard
```

2. Run the judge command:

```bash
reasond judge
```

This judges only archived audits that have not been judged yet using the local
default judge provider/model from `.reasond/settings.json`. If the repository is
not initialized, ask the user to run `reasond init`.

3. Read the newest judge results:

```bash
reasond latest
```

4. For each finding that may affect the current review or debugging task, read
the detail:

```bash
reasond show <provider:id>
```

5. If the detail points to a source markdown file and the full reasoning log is
needed, read that file directly.

6. Compare the findings against the actual code, tests, and user request. Use
the findings as evidence, not as unquestioned truth.

7. Report conclusions to the user with clear separation between verified facts,
reasond judge feedback, and your own analysis.

## Additional Commands

Use this when a broader repository-level review needs all stored findings:

```bash
reasond list
```

## Rules

- This skill does not replace normal debugging, code reading, or tests.
- Do not invent findings if reasond returns none.
- Do not run `reasond judge --all` by default because it can spend extra model
  time and create additional historical runs.
- If a reasond finding is irrelevant to the current task, ignore it and state
  nothing about it unless the user asks.
