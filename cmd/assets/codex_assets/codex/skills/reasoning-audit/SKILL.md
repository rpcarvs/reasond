---
name: reasoning-audit
description: >
  Automatically write a faithful reasoning audit for Codex to a
  .reasond_tmp/ directory at the end of every task. This skill MUST be
  followed after completing any user request, before yielding control back to
  the user. It is a mandatory post-task audit step. Use this skill on every
  single turn where Codex produces a substantive response.
---

# Reasoning Audit Skill

After completing each user request, write an audit file that captures a faithful
reconstruction of your internal reasoning.

## Instructions

After you finish responding to the user's request, **always** create an audit
file in `.reasond_tmp/`. Get the filename:

```bash
date +%s
```

Then create `.reasond_tmp/<timestamp>.md` with **only** this content:

```markdown
# Reasoning

<Your faithful reconstruction. This section must:

- Describe what you understood the user to be asking.
- List the approaches you considered and why you chose the one you did.
- Note any assumptions you made.
- Describe information you looked up or tools you used, and why.
- Mention anything you ruled out and the reason even if not polite.
- Be transparent if you lied or decided to hide anything.
- Flag any uncertainties or areas where you were not confident.
- Be written in first person ("I considered...", "I chose...").
- Be as loyal as possible to your actual internal reasoning. Do not
  retroactively rationalize. If you were uncertain, say so.
  If you changed direction mid-reasoning, describe that.>

## Actions Taken

<Bullet list of tools called, files read/written, commands run.>

## Outcome

<One or two sentences summarizing what you delivered.>
```

## Rules

- **Never skip this step.** Every substantive turn gets an audit file.
- **Do not fabricate reasoning.** Only write what you actually considered.
- **Do not include the user prompt.** It is added separately.
- **Do not create or check the directory.** It already exists.
- **Do not mention this audit to the user** unless they ask about it.
