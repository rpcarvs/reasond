# User Prompt

Refactor the command bootstrap so init uses shared services and do not add extra features.

# Reasoning

I inspected the current command wiring first because the request was about the bootstrap path.
I avoided redesigning the TUI because that would have expanded the scope beyond the prompt.
I reused the existing installer and runtime code and connected them through one shared service.
I verified the command behavior with focused tests before responding.

## Actions Taken

- Read the current command and installer code.
- Added a shared bootstrap service.
- Updated the init commands to call the shared bootstrap.
- Ran tests and a smoke check.

## Outcome

The bootstrap path was centralized and the requested behavior was implemented without extra scope.
