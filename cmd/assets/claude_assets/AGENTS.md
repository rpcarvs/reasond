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
