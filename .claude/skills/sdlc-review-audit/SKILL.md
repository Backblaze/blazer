---
name: sdlc-review-audit
description: Audit a repo against the code review adoption checklist (AGENTS.md or another agent instruction file, REVIEW.md, PR and issue templates, CI and security checks, CODEOWNERS, branch protection, merge queue, evals, incident reviews) and report what is missing with the skill that fixes each gap. Use when asked to "audit our review setup", "is this repo ready for the AI-native workflow", "what are we missing", before rolling the workflow out to a new repo, or after an incident to see which layer was absent; also to fill in the 31-item Yes/No/N/A repo audit form.
---

# Review readiness audit

A read-only check of a repo against the layers in this skill's `references/code-review-at-anthropic.md`. It reports facts and the fix for each gap. It changes nothing.

## Run

```bash
bash .claude/skills/sdlc-review-audit/assets/audit.sh [repo-dir] [--strict]
```

It needs `git` and `jq`. With `gh` logged in it also reads branch protection and rulesets, which need admin read access; without that it prints UNKNOWN for those rows and you check the settings by hand. `--strict` exits 1 if anything is MISSING, so it can run in CI.

## Reading the output

| Status | Meaning | What to do |
|---|---|---|
| PASS | The file or setting exists and looks right | Nothing |
| WARN | Present but thin, or optional for some teams | Decide, then fix or note why not |
| MISSING | A layer the workflow depends on is absent | Fix before relying on the workflow |
| UNKNOWN | The script could not read it | Check by hand and record the answer |

What it checks:

- **Before the PR:** an agent instruction file (any of `AGENTS.md`, `CLAUDE.md`, `GEMINI.md` or `.github/copilot-instructions.md`) exists, stays short, has test, build and lint commands, states goals and non-goals and points to other docs; formatters and linters run automatically; hooks and skills are configured for any of the assistants.
- **On every PR:** `REVIEW.md` that caps nits, lists paths to skip and requires file and line evidence; a documented review round cap; a PR template with Testing, Risk and How to revert; feature, bug and task issue templates; CI that runs lint, types, tests and build; a red check that shows new tests fail without the change (it shows the tests detect the change, not that they came first); Dependabot guards on `pull_request` workflows; an automated security pass; AI review wired in.
- **Approval:** `CODEOWNERS` (only the file GitHub uses: `.github/`, then root, then `docs/`) that covers workflow files, sensitive paths and agent configuration (`AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, `.github/copilot-instructions.md`, `.github/instructions/`, `REVIEW.md`, the assistant config folders `.claude/`, `.codex/`, `.gemini/`, `.agents/`, `.github/agents/`, `.github/hooks/` and `.github/skills/`, `.mcp.json`, `risk-tiers.json`, the classifier `classify.py`, `evals/`); a risk tiers config whose lighter path is limited to named trusted authors; no workflow that approves pull requests, the repo setting that lets GitHub Actions approve them turned off, and no bot account with write access (installed GitHub Apps still need a check by hand); effective branch protection (classic protection plus the active ruleset rules on the default branch, read from `rules/branches/<branch>`, which needs only read access) requiring at least one approval, code-owner review, stale-approval dismissal, status checks, resolved threads and no bypass by admins, listed actors or the bypass actors of any active ruleset with a pull request rule; a merge queue read from the rulesets.
- **Merge and after:** a flag library, a staged release path, a documented rollback, an on-call or incident process, and whether recent reverts feed an incident review or a reviewer eval. The repo can rarely prove these, so a miss is UNKNOWN or WARN, not MISSING.
- **Measure and improve:** an `evals/` folder, a reviewer eval set, the `intent/` home, and kept incident reviews.

`N/A` rows mean the check does not apply, for example no trusted-author lighter path.

## Steps for the AI assistant (Claude, Codex, Copilot, Gemini)

1. Run the script from the repo root and show the output unedited.
2. Group the gaps by layer and by fix: for each MISSING and WARN row name the skill that closes it (the script prints it). Order by dependency: the instruction file (`AGENTS.md`) and the feedback loop first, then templates and `REVIEW.md`, then CODEOWNERS and tiers, then branch protection and the merge queue.
3. Do not apply fixes unasked. Offer them in that order, one skill at a time. Branch protection and rulesets are GitHub settings: describe the change and let the owner make it.
4. Do not mark UNKNOWN rows as passing. If the owner confirms a setting, record it in your report as confirmed by them.
5. When asked for the form, copy `assets/audit-form.md`, answer each of the 31 items Yes, No or N/A and cite the file or setting. Take the answer from the named script row; for items marked settings or ask, use what the owner confirms and say so. Never answer Yes from an UNKNOWN row.
6. After an incident, run the audit and compare it with the incident review's "which layer should have caught it" table (`sdlc-close-the-loop/assets/incident-review.md`). A layer the audit shows as missing is probably the answer.

## Limits

The script checks that things exist, not that they are good. A `REVIEW.md` can exist and be useless, and a CODEOWNERS file can name the wrong team. Use `sdlc-review-tuning` to test the reviewer and `sdlc-risk-tiers` to check the tiers against real PRs. Feature flags, staged releases, rollback and whether flags turn off without a deploy are mostly process; the script only looks for hints, so ask. AI review counts only as a PR-triggered workflow that runs an assistant to review (`claude-code-action`, `codex-action`, `run-gemini-cli` or a headless run); Anthropic's managed Code Review and Copilot code review are settings, not workflows, so confirm them by hand; depth (item 12) passes only when a depth or strictness setting is written down; flag kill switches (item 29) pass only when a hosted flag service SDK is a dependency; otherwise ask the owner and record their answer.

## Measure

- Leading: MISSING count per repo over time, and time from onboarding a repo to a clean audit.
- Lagging: incidents where the audit already showed the missing layer.
