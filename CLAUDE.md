@AGENTS.md

# Project Rules

## Upstream

Single upstream — `router-for-me/CLIProxyAPI` (this repo's `upstream` remote). All upstream PRs route here.

**Fork lifecycle principle.** This fork exists *only* while our patches are still needed. Each patch is a liability that must be re-justified every sync: if upstream lands an equivalent fix, drop our patch (see `c57203ea` → upstream `e7f4dd47`, 2026-06-09). Keep the patch set minimal — never add a fork-only change to an upstream-tracked file when a fork-only file (e.g. `CLAUDE.md`) can hold it. If all functional patches become superseded upstream, the fork has served its purpose and should be retired, not kept alive for its own sake.

**Current functional patches (4)** — re-audit relevance every sync:
1. `fix: strip -customtools variant suffix` — `internal/registry/model_variants.go` (fork-only file), `model_registry.go`, `sdk/api/handlers/handlers.go`. PR #1958.
2. `feat: bypass proxy for private/LAN IPs` — `internal/api/handlers/management/api_tools.go`, `internal/config/sdk_config.go`. PR #1960.
3. `fix(auth): load and persist prefix for OAuth providers` — `auth_files.go`, `sdk/auth/filestore.go`. PR #2669.
4. `fix(auth): hydrate Prefix from metadata in all store backends` — `gitstore.go`, `objectstore.go`, `postgresstore.go`, `filestore.go`, `sdk/cliproxy/auth/metadata_hydrate.go` (fork-only file). PR #2669.

Plus 2 non-functional fork commits: deploy workflow (`.github/workflows/ghcr-build.yml`) + this `CLAUDE.md`.

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

## Routine Sync Procedure

The full "check for updates → update fork" cycle the user invokes traditionally. Every step is evidence-producing; record results in CONTINUITY. Read-only until step 7.

1. **Fetch + measure delta.** `git fetch upstream --tags`. `git log --oneline <our-base>..upstream/main` and `git rev-list --count`. Identify the new version tag: `git describe --tags upstream/main`. Note big themes (breaking changes, removed modules, new features).

2. **Issue/PR audit** (see Upstream Sync Hygiene above). For every active symptom/watch-item, search open + recently-closed issues. Confirm whether any are fixed in the delta — inspect the actual commits (`git show <sha>`), don't trust the title. Watch-items currently tracked: **#3624/#3663** (Opus 4.8 "thinking blocks cannot be modified" — signature sanitizer; check every sync, still open as of v7.2.20).

3. **Patch-file overlap map.** For each fork-patch file, `git log --oneline <our-base>..upstream/main -- <file>`. Files with 0 upstream commits = safe; files upstream touched = conflict + regression surface to inspect.

4. **Behavioral-regression audit.** For each high-impact upstream commit touching (or adjacent to) our serving path (Claude/Codex executors, auth conductor, config parsing, thinking pipeline), inspect the diff. Verify: new config fields default to legacy behavior (zero-value safe); breaking changes (`feat!`) are in modules we don't use; comment-only changes carry no logic delta. We serve `claude-*` + `codex` only — antigravity/gemini-cli/home/plugin code is config-gated and dead for us (the user does not run antigravity accounts; Google bans third-party antigravity use — maintainer position #3340 "ban is certain").

5. **Patch-relevance / supersede check.** Re-justify each functional patch (see Fork lifecycle principle). A patch upstream solved differently surfaces as an *empty commit* during rebase — drop it. SocratiCode/Serena index the working tree, NOT arbitrary git refs, so the trial rebase itself (step 6) is the authoritative supersede detector, not grep against `upstream/main`.

6. **Trial rebase + verify.** Backup first: `git branch backup/pre-rebase-<tag>-<date> main`. Rebase main onto `upstream/main`; resolve conflicts (recurring one: upstream's Gemini-CLI removal leaves collateral in our patch files — drop the deleted upstream symbols, keep our logic). Then:
   - `go build ./...` (exit 0).
   - `go test ./...` on patch + overlap packages. **A failing test may be pre-existing in clean upstream** — verify with a throwaway worktree (`git worktree add /tmp/chk upstream/main`) before blaming our rebase. (Known pre-existing upstream failure as of v7.2.20: `TestWriteOAuthCallbackFileForPendingSessionCreatesMissingAuthDirForCallbackProviders/gemini`.)
   - `go vet` on touched packages.
   - `git range-diff <base>..backup upstream/main..main` — every patch should be `=` (byte-identical) or have an explainable `!` delta.
   - **Do NOT `gofmt -w .`** — the tree is CRLF; `gofmt -d` shows only line-ending noise. `go vet` exit 0 is the real format gate.
   - Rebase the 3 PR branches too (this is part of every sync, not optional): `fix/customtools-variant-suffix` (#1958, base main), `fix/no-proxy-private-ips` (#1960, base main), `fix/oauth-prefix-load-persist` (#2669, base dev; dev==main historically). Backup, rebase, build, range-diff each.

7. **Deploy** (only after steps 1-6 pass + user go-ahead). `git push origin main --force-with-lease` auto-triggers the GHCR build → Watchtower swap. Force-push the 3 PR branches too. Confirm the build (`gh run list --workflow ghcr-build`) and PR mergeability (`MERGEABLE` + `BLOCKED` on old review is expected). Smoke per Deploy section; the Codex `/v1/responses` path needs live multi-turn tool-use to prove (single-turn 200 is insufficient).

## Deploy

GHCR image source — our fork. Trigger build via `gh workflow run ghcr-build -R thebtf/CLIProxyAPI`. Internal deployment details (host, port, watchtower wiring) live in private notes, not in this file.
