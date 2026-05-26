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

## Upstream Sync Hygiene

On every "check for updates" cycle (`git fetch upstream`, rebase, patch audit), **also audit upstream issues + recent PRs/commits for our active symptoms**. Skipping this once let us ship the wrong-hypothesis `c6a325ad` h2 PING fix while issue #3267 (correct diagnosis with goroutine dump) was open upstream for 18 days.

**Mandatory checks during sync:**

1. **New upstream commits since last sync** — `git log --oneline LAST..upstream/main`. Look for `fix:` / `feat:` touching code areas we're actively investigating or have fork patches in.
2. **Open + recently-closed issues with matching symptom signatures.** For each active investigation, search:
   - `gh issue list --repo router-for-me/CLIProxyAPI --search "<symptom keyword> in:title,body"`
   - `gh issue list --repo router-for-me/CLIProxyAPI --state closed --search "<symptom> closed:>YYYY-MM-DD"`
   - Include error-log strings, function names from stack traces, observed user-visible behavior (e.g. `hang`, `idle`, `accept`, `5min`, name of the affected endpoint).
3. **Recently-merged PRs touching our problem files.** `gh pr list --repo router-for-me/CLIProxyAPI --state merged --search "in:files <our path>"`.

**Trigger to halt local fix work and re-audit upstream:**
- Investigation enters Phase 2 (first hypothesis refuted) — don't build the second-round fix without re-checking upstream first.
- Active investigation exceeds 30 minutes of code-reading without finding root cause — upstream may have already identified it.
- Symptom description is generic enough that other users could have hit it (most multi-user-facing bugs qualify).

Record the upstream-search step in any debug report (`.agent/debug/*/investigation.md`): which queries were run, with what result. Empty result is acceptable evidence; skipping the step is not.

## Deploy

GHCR image source — our fork. Trigger build via `gh workflow run ghcr-build -R thebtf/CLIProxyAPI`. Internal deployment details (host, port, watchtower wiring) live in private notes, not in this file.
