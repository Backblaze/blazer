---
name: sdlc-pr-review
description: Set up REVIEW.md and run layered AI review on a diff or PR (bugs, security, compliance against spec.md/plan.md) with verified, severity-tagged findings (Important, Nit, Pre-existing), a separate blocking security pass, round caps, and a PR template; or address review comments on a PR. Use when asked to "review this PR/diff", "set up PR review", "write REVIEW.md", "address the review comments", "set up AI review in CI", or before a PR is marked ready.
---

# AI in the PR review loop

The AI assistant (Claude, Codex, Copilot, Gemini) both gives and receives reviews. Every PR gets the same review passes with findings ranked by severity, so human attention moves up to whether the change does what the plan intended and whether the risk is acceptable. The question to ask is not "did a human read this?" but "what could go wrong, and which check, automated or human, should catch it?" Review is a stack of layers (pre-PR verification, AI review, security review, blocking checks, risk-scaled human approval, merge queue and rollback), each catching what others miss.

Separation of duties holds: the agent that wrote the code has no way to approve it, and an agent's approval never fills a required human slot. How many humans must approve, and when none is needed, is set by `sdlc-risk-tiers`. The lessons here come from this skill's `references/code-review-at-anthropic.md`, alongside the playbook's PR review lesson.

## Layers around this skill

1. Before the PR: the author's session verifies its own work (`sdlc-feedback-loop`) and runs a bug-hunt, a simplify pass and a fresh-context subagent review.
2. This skill: AI review on every PR.
3. A separate security pass that blocks merge when it has open findings.
4. Required automated checks (lint, types, tests, builds) and the PR template (below).
5. Human approval scaled to risk (`sdlc-risk-tiers`).
6. Merge queue, flags, staged release, rollback (`sdlc-cicd`).

## Mode A: set up `REVIEW.md` (code owners)

`REVIEW.md` says what matters in the part of the codebase it covers. Code owners write it, per area where areas differ; short files work best.

1. Ask which passes the organization cares about. Default: Bugs, Security, Compliance (change matches `spec.md`, `plan.md` and design principles).
2. Define what failure looks like in that area, in a word or two (for docs, "it lies"; for config, "it misroutes"), and what Important means (breaks behavior, leaks data, breaches policy). Style and naming are nits.
3. Cap nits (e.g. five per review, summarize the rest as a count) and list what not to report (generated paths, anything CI already enforces).
4. Require evidence: every finding cites a file and line, never a guess.
5. Require the summary to start with a count, and to say plainly when nothing needs fixing.
6. Commit `REVIEW.md` at the repo root (or the area's folder). Then check that the reviewer actually follows it (`sdlc-review-tuning`).

```markdown
# Review instructions
## Passes
Run three passes and tag each finding with its pass:
- Bugs: logic errors, broken edge cases, subtle regressions
- Security: injection risks, authentication gaps, PII in logs
- Compliance: the change matches spec.md, plan.md and our design principles
## What Important means here
Reserve Important for findings that would break behavior, leak data
or breach a policy. Style and naming are nits.
## Evidence and summary
Cite file and line for every finding. Start the summary with the number of
findings; say "nothing needs fixing" when that is true.
## Cap the nits
Report at most five nits per review; summarize the rest as a count.
## Do not report
Generated files and anything CI already enforces.
```

Hosting options: Anthropic's managed Code Review service (admin enables it per repository, with a depth setting per repo: Quick, Standard or Deep, where Deep is for areas where one missed bug costs more than the extra review time), Copilot code review (it reads `AGENTS.md`, `.github/copilot-instructions.md` and `REVIEW.md`, so this file applies without a workflow), or a CI job running `claude-code-action`, `codex-action` or `run-gemini-cli` when you need control of the pipeline or model access through your own cloud agreement (`sdlc-cicd`, `references/ai-assistants.md`). Roll out on one repo, tune it for a few weeks, then expand.

## Mode B: review a diff or PR

1. Get the diff (`git diff <base>...HEAD`, or `gh pr diff`). Read `REVIEW.md`, `AGENTS.md` (flag convention violations and stale lines), the PR description (the stated intent), and the change's `intent/<slug>/spec.md` and `plan.md` if present. The compliance pass depends on the committed plan, which a reviewer looking at the diff alone would not have open.
2. Judge the whole change, not only lines: does it solve the stated problem, fit the system, and avoid breaking something elsewhere? For a bug fix, check it addresses the root cause and not just the symptom.
3. Run each pass in `REVIEW.md` over the full diff, citing requirement numbers (R1..) and plan steps where relevant. Prefer several independent hunting passes over one long one.
4. **Verify before posting.** Have a separate verifier check each candidate finding (a fresh-context subagent per finding, or several voting) and keep only those that hold up. Drop style-only, diagnostic and cleanup findings unless `REVIEW.md` asks for them.
5. Check that the change was test-first: new tests fail without the change (the red check output), tests were not weakened to pass, refactors are separate commits with tests unchanged, and `plan.md` was updated if the implementation departed from it.
6. Tag each finding **Important**, **Nit** or **Pre-existing**. Pre-existing is for a bug the PR did not introduce, so it is not blamed on the author; still report it.
7. For a recurring bug class, say so, and list every instance, so the author fixes all of them at once.
8. Report with a count first, then a table sorted by severity:

   | Severity | File:Line | Issue |
   |---|---|---|
   | Important | `path:22` | Compliance: <finding, cite plan/spec> |
   | Nit | `path:8` | Bugs: <finding> |
   | Pre-existing | `path:140` | Bugs: <finding the PR did not introduce> |

9. Findings do not approve or block on their own, except the security pass below. State plainly that approval belongs to the humans the risk tier requires.

### Security pass

Run security as a separate pass on every PR (Claude Code's `/security-review` or an equivalent job) with its own check run. Open findings from the bug review or the security pass block merge. Closing a thread with a short reason clears a false positive; a later commit that fixes the issue should clear the finding automatically.

### Resolve every finding

Each finding is resolved either by a fix or by closing the thread with a reason. Do not let a PR merge with open threads.

## Mode C: address review comments

When tagged on a comment, make the fix, push, and reply on the thread so it records both the request and the change. To shepherd your own PR: sweep unresolved comments and failing checks, fix, push, repeat until green and waiting only on the required approvals. Never approve or merge your own PR.

Avoid endless loops between an agent author and an agent reviewer:

- After three rounds, stop line-fixing and ask the author to reconsider the approach.
- After the first few passes, re-review only the new diff.
- Fix the whole bug class in one commit.
- Never repost a finding already raised or resolved.

Details and numbers in `sdlc-review-tuning`.

## PR template

Ask for Testing (what was run and what it showed), Risk (what could go wrong, blast radius) and How to revert. The assistant can draft all three; the person who requested the change signs off on them. A ready-made template is in `sdlc-issue-templates/assets/pull_request_template.md`; make sure the repo has it.

## Feedback into the system

- A finding that recurs is fixed at the source: a rule in `AGENTS.md`, a line in `REVIEW.md`, a lint, or a skill (`sdlc-agent-instructions`, `sdlc-policy-skill`). A mistake flagged a second time goes into `AGENTS.md` in that PR.
- Flag when a change makes `AGENTS.md` outdated.
- Monthly: rate findings and cap noisy categories in `REVIEW.md` (`sdlc-review-tuning` for how to measure noise).
- Incidents and reverts become review evals, so the reviewer is scored against them (`sdlc-evals`).

## What humans still own

Whether to build it at all, risky code (billing, updaters, CI workflows, auth), architecture others will copy, and reading the evidence: test output, commands run, screenshots, reviewer findings. If you cannot say what the checks would miss, ask for a real review. A person is accountable for every shipped change, whoever wrote it.

## Measure

- Leading: time to first review (minutes); human approvals per PR (median and p95); human comments; time a PR waits on a person; share of review comments resolved without a human touching the branch.
- Guardrail: revert and incident rates. If they rise, human time was moved, not saved.
- Lagging: defects and vulnerabilities caught before merge vs escaping to production; mean reviews to pass.
