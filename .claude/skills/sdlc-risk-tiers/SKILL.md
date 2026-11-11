---
name: sdlc-risk-tiers
description: Design risk-scaled human approval for PRs (none, one or two approvers by the change's risk, not the author), with CODEOWNERS, branch protection, a risk classifier, trusted-author paths, per-commit approvals and circuit breakers on revert and incident rates. Use when asked to "decide who has to approve what", "reduce human review time safely", "set up CODEOWNERS and branch protection for agent PRs", "define risk tiers", or when review is the bottleneck.
---

# Risk-scaled human approval

Goal: the minimum human review time that does not raise reverts or incidents. The change decides how many people look at it, not who wrote it. Start strict and loosen only with evidence. The pattern comes from how the Claude Code team reviews code (see `references/code-review-at-anthropic.md` in the playbook folder).

## Steps

1. **Baseline.** Measure today: human approvals per PR (median and p95), human comments, time a PR waits on a person, and the revert and incident rates. Revert and incident rates are the guardrail; without them you cannot tell saved time from moved risk.
2. **Define tiers by the change.**

   | Risk | Typical contents | Human approvals |
   |---|---|---|
   | Low | Docs, copy, internal-only features, additive changes behind a flag | None, once automated review and security checks are green |
   | Medium | Ordinary product changes; sensitive areas the owners name (caching, updaters, system prompts) | One person |
   | High | Highest-risk code, plus a list of paths that always need two | Two people |

   Adapt the examples to your repo with the owners of each area. Fewer tiers is better.
3. **Name the always-human paths.** Auth, billing, CI and release workflows, installers, migrations, infra, agent configuration (instruction files such as `AGENTS.md`, `AGENTS.override.md`, `CLAUDE.md`, `GEMINI.md`, `.github/copilot-instructions.md` and `.github/instructions/`, `REVIEW.md`, assistant config folders such as `.claude/`, `.codex/`, `.gemini/`, `.agents/`, `.github/agents/`, `.github/hooks/` and `.github/skills/`, and `.mcp.json`), the tier rules and the classifier themselves (`risk-tiers.json`, `classify.py`; a check that runs the PR's own classifier must not be able to approve an edit to it) and evals. Put them in `CODEOWNERS` and make branch protection require code-owner review for them whatever the tier says. List only those paths: a `*` catch-all with code-owner review required puts a person on every PR and cancels the low tier. If you keep a no-human tier, the branch's required approval count must be 0 too, and a required check that fails when a PR has fewer approvals than the classifier's `humans_required` enforces the tiers instead.
4. **Classify mechanically.** One classifier assigns the tier from the diff (paths touched, size, flags, dependencies). A second can force a human even when the tier would not (sensitive path, workflow file, new permission). Both are scripts or review-agent outputs published as check runs, so the decision is visible and logged. Do not let the author set or edit the tier.
5. **Lighter paths only for trusted authors**, typically the owning team. Anyone else needs a second approver.
6. **Keep agent and human approvals separate.**
   - An agent's approval never counts as a human approval. Automated review can clear a low-risk change but never fills a required human slot.
   - On an agent-written PR, the person who prompted the agent is a required reviewer.
   - Approvals are per commit: dismiss stale approvals on new commits, and require re-approval for commits the system cannot vouch for (an agent push, a hand-resolved merge).
7. **PR template.** Ask for Testing (what was run), Risk (what could go wrong, blast radius) and How to revert. The AI assistant (Claude, Codex, Copilot, Gemini) may draft them; the person who requested the change signs off.
8. **Require resolved threads and green checks** for every tier: AI review findings, the security pass and CI all clear before merge (`sdlc-pr-review`).
9. **Pair with reversibility.** Merge queue, flags, internal-first rollout, staged releases and one-step rollback (`sdlc-cicd`) are what make lighter tiers acceptable.

## Assets

Copy what you need from `assets/` into the repo:

- `risk-tiers.json`: the tiers as data (`low_paths`, `medium_paths`, `high_paths`, `always_human_paths`, `trusted_authors`, `large_diff_lines`). Edit it with the owners.
- `classify.py`: the classifier. Standard library only.
  ```bash
  git diff -z --name-only --no-renames origin/main...HEAD | python3 classify.py --config risk-tiers.json --author "$PR_AUTHOR" --lines "$CHANGED_LINES"
  ```
  `--no-renames` lists both sides of a move, so a sensitive file moved into `docs/` keeps its tier, and `-z` passes unusual filenames unquoted (newline input with git's C-quoting also works). It prints `tier`, `humans_required`, `force_human` and the reasons as JSON. The highest tier of any changed file wins, a diff over `large_diff_lines` goes up one step, `always_human_paths` force at least one person, and authors outside `trusted_authors` need one more approver (up to two), and so does a run with no `--author`: an unknown author is never trusted. Pass `--agent-authored` for agent PRs; it raises `humans_required` to at least one. A bot or agent author (`[bot]`, `-bot`, or a login starting `claude`, `copilot`, `codex`, `openai-codex`, `gemini` or `github-actions`) is treated as agent-authored automatically, and bots in `trusted_authors` are ignored. An empty file list (usually a failed diff) also requires a person rather than clearing the PR. `always_human_keywords` force a person when any path segment contains one of them (`src/auth.py`, `oauth/`, `Payments/`), as a backstop to the globs. Publish the JSON as a check run and have a required check fail when the approvals on the PR are fewer than `humans_required`.
- `CODEOWNERS.sample`: always-human paths and the owning teams. Turn on "Require review from Code Owners" in branch protection.
- `review_metrics.py`: the baseline. `python3 review_metrics.py --repo owner/name --limit 200` reports human approvals per PR (median and p95), hours to first human review, hours open, reviews with a written body and revert share, read-only through `gh`. Run it before changing tiers and monthly after.

Test the classifier on a dozen real past PRs, including one that caused an incident, before it gates anything. If a PR lands in a tier you would not have chosen, change the config, not the PR.

## Circuit breakers

Putting more changes on the no-human path needs a brake. Put a control band on the revert rate and the incident rate (`sdlc-close-the-loop`). On a breach, the response is deterministic and pre-approved: raise every tier by one, or send the affected area to the next tier up, until the metric is back at baseline. Record the change in version control and tell the owners. The Claude Code team lists this as a known gap; treat it as required before widening zero-human merges.

## When review has missed something

Run an incident review that asks which layer should have caught it, then encode the fix: a rule in `AGENTS.md` or `REVIEW.md`, a skill, a new check, a tier change, or an eval (`sdlc-evals`, `sdlc-review-tuning`). Review the system, not the person.

## Governance

Tier rules, path lists and classifier code live in the repo and change through PRs approved by the area owners. Branch protection and `CODEOWNERS` enforce them. If the organization is regulated, keep the human-approval default for everything and use tiers only to route the second approver. This is a posture decision for change management, not for the assistant (see the gate dial in `references/playbook-harness-translation.md`).

## Measure

- Leading: human approvals per PR (median, p95); wait time on people; share of PRs merged on the no-human path.
- Lagging (guardrail): revert and incident rates by tier. If they rise, tighten the tier before anything else.
