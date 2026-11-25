# Intent: review and merge process scaled by risk
Author: Gonzalo Peña-Castellanos (with Claude). Status: draft, for @goanpeca and @sophiecarreras.
Source: [How the Claude Code team does code review](https://claude.dev/blog/solving-code-review-at-anthropic/)
(claude.dev, 2026-10-15), applied to Blazer.

## Problem

Most Blazer PRs are now written by agents, and the review load has grown with them. In the last cycle
reviewers repeatedly asked for the same things (changelog entries, tests that pin the fix, wire-shape
tests), several PRs went through many review-and-fix rounds, and 15 PRs are open at once, many touching
`base/base.go` and conflicting with each other.

Today:

- Every PR gets the same human attention whatever its risk; a docs fix and a `base/` auth change are
  treated alike.
- On `main`, the active rulesets block deletion and force-push, require linear history and request a
  Copilot review on every push. `.github/branch-protection-main.json` describes a required `ci-summary`
  check and no required reviews; whether it is applied could not be confirmed without admin access.
- `area-owner-approvals` is a soft, non-blocking status, and it keeps an approval valid after new
  commits, including commits pushed by an agent.
- Review findings can be left unanswered at merge.
- Nothing measures whether a reviewer change (REVIEW.md, AGENTS.md, model) catches more or fewer real
  bugs.

PR #151 already covers the parts that need no settings change: REVIEW.md severities, evidence and
convergence rules, Risk and How-to-revert in the PR template, and a fresh-context self-review before a
PR opens.

## Proposed outcome

Human review time goes where the risk is, with no rise in reverts or post-release fixes:

1. **Risk tiers set by the change, not the author.** A first proposal to accept or change:

   | Tier | Paths | Human approvals |
   | --- | --- | --- |
   | Low | `*.md` docs, `*_test.go` only, `.changes/`, `intent/`, `examples/` | None once CI and automated review are green, for authors on the trusted list |
   | Medium | `b2/`, `x/`, `internal/` (except below), `bin/b2keys/` | One code owner |
   | High | `base/` wire and auth code, credential handling, `.github/workflows/`, `.changie.yaml`, release commits | One code owner always; two for auth and workflow changes |

   Authors outside the trusted list always need a person. A bot's approval never fills a human slot.
2. **Approvals cover one commit.** New commits pushed after an approval (agent pushes, hand-resolved
   merges) need re-approval for Medium and High tiers.
3. **Every review thread resolved before merge** (GitHub "require conversation resolution").
4. **Automated AI review and security review on every PR**, with open Important findings blocking merge.
   Decide whether this replaces or runs beside the current Copilot ruleset review. Start in this repo,
   tune REVIEW.md for a few weeks, then decide on blocking.
5. **Merge queue**, so checks rerun on `main` plus the change and two PRs that pass alone cannot combine
   into a broken `main`, without rebasing each of the 15 open PRs by hand.
6. **Past bugs become reviewer evals.** Build a set from bugs reviewers caught or that shipped, confirmed
   by the code owner, for example #78 (lifecycle rules sent on every update), #74 (empty SSE mode re-sent),
   #30 (v4 key scope parsed from the wrong place), #131 (progress only per chunk), #147 (part SHA1 on
   `ReadFrom`). Score the reviewer against the set whenever REVIEW.md, AGENTS.md or the model changes.
7. **Measure it.** Human approvals per PR (median, p95), review rounds to pass, time a PR waits on a
   person; reverts and post-release fix PRs as the guardrail.

## Affected users and systems

- Contributors and agents opening PRs; the code owners in `.github/CODEOWNERS`.
- Repo settings: branch protection or rulesets on `main`, merge queue, conversation resolution.
- Workflows: `require-area-approvals.yml`, a new review workflow, possibly a risk-tier check.
- Library users only indirectly: fewer regressions and faster fixes.

## Constraints

- Settings changes need a repo admin; this intent proposes them, it does not apply them.
- Separation of duties: the agent that wrote a change never approves it, and the person directing it
  counts as one reviewer.
- Workflow changes keep the Dependabot guard (`github.actor` and `pull_request.user.login`).
- Start strict and loosen with evidence; any tier can be tightened if reverts rise.
- Cost of running an AI reviewer per push in CI.

## Open questions

- Who is on the trusted-author list for the Low tier? (@goanpeca, @sophiecarreras)
- Do the tier paths above match where the risk is? Which paths always need two people?
- Keep Copilot review, replace it, or run both during a trial? (@goanpeca)
- Is a merge queue available on this org's plan, and who enables it? (repo admin)
- Should the `changelog` check from #151 be required? (@goanpeca, @sophiecarreras)
- Where should the eval set live and run: this repo, or the central SDK harness? (@sophiecarreras)
