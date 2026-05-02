@AGENTS.md

# Project Rules

## Upstream

Single upstream — `router-for-me/CLIProxyAPI` (this repo's `upstream` remote). All upstream PRs route here.

## Remotes

- `origin` — `thebtf/CLIProxyAPI` (our fork). Local `main` tracks `origin/main`. Push fork patches and deploy branches here.
- `upstream` — `router-for-me/CLIProxyAPI`. Sync from here, send PRs from feature branches.

## Upstream PR Policy

**NEVER create a PR to upstream until:**
1. The fix has been deployed and manually tested by the user.
2. The user explicitly commands to create the upstream PR.

Non-negotiable. Test first, PR after explicit approval.

## Deploy

GHCR image source — our fork. Trigger build via `gh workflow run ghcr-build -R thebtf/CLIProxyAPI`. Internal deployment details (host, port, watchtower wiring) live in private notes, not in this file.
