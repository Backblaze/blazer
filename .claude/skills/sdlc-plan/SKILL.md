---
name: sdlc-plan
description: Produce and commit a plan.md before any code is written, working in plan mode from the approved spec.md and intent.md, with a reuse survey, a test plan and a fresh-context critique. Use when starting implementation of a change, when asked to "plan this", "make an implementation plan", "red-team this plan", or before editing code for a change that has an intent/spec; also use to keep plan.md in sync when implementation departs from it.
---

# Plan mode as the default starting point

Work starts with a written, reviewable plan. Plan mode lets the AI assistant (Claude, Codex, Copilot, Gemini) read the codebase without changing anything, so course corrections are edits to a document.

## Steps

1. Work in plan mode (Claude Code: Shift+Tab, or start the session with `--permission-mode plan`; other assistants: `references/ai-assistants.md`). Do not edit files until the engineer accepts the plan.
2. Read `intent/<slug>/intent.md`, `spec.md` (the requirement numbers R1.. and concerns), `AGENTS.md` and the code the change touches. If a spec exists, a plan without it is a smell; if neither exists, ask whether to run `sdlc-intent` / `sdlc-spec` first or to plan from a plain description. In a repo you do not own (open source, another team's), read `CONTRIBUTING.md` and follow it.
3. **Survey before designing.** Search for existing functions, types, utilities and config that already do part of the job, and check each module's public surface. Something close exists: extend it. Something truly new: say why nothing existing fits. Find the tests that already cover the area and what they assert. If the repo has a code index (a code graph, a language server, Sourcegraph), query it before grepping. `sdlc-explain-module` helps for unfamiliar code.
4. Draft the plan using the template below: the simplest version that meets the requirements, named for the work it does.
5. **Red-team it** before a person sees it. Run the plan critic (`sdlc-subagents/assets/plan-critic.md`, installed as a custom agent: `.claude/agents/plan-critic.md` in Claude Code) in a fresh context: it attacks over-engineering, reuse, performance, hidden complexity and consistency, and the test plan's missing edge cases, weak tests and duplicates. Revise, and record the critique and what changed in the Critique section, so the approver sees both.
6. Let the engineer interrogate it. Be ready to answer: what could this break, which step is riskiest, which alternatives were rejected and why.
7. Iterate until someone who never saw the conversation could implement the change from the plan alone.
8. Commit the approved plan as `intent/<slug>/plan.md`. Then implement test-first (`sdlc-feedback-loop`); with a solid plan this is often one pass.
9. If implementation departs from the plan, update `plan.md` in the same commit. Consider a hook that flags source changes with no matching plan update (`sdlc-hook-gate`).

## Template

```markdown
# Plan: <title> (from intent.md and spec.md)

## Files that change
<paths, marking new files>

## Reuse
<existing code this extends: file and symbol. Anything new, and why nothing existing fits>

## Order of work
1. Test first: <test name, file> fails because <reason>. Then: <smallest change> (R1, R2).
2. Test first: <test> fails. Then: <change> (R4).
3. Refactor, if any: <what>, tests unchanged.

## Risks
<what could break; cite spec concerns by number>

## Test plan
- Extend: <existing test file and case> covers <R-number> once it also asserts <what>. No duplicates.
- New, failing first: <test name> for <behavior or edge case> (R-number), one per behavior.
- Edge cases: <empty, limits, errors, permissions, concurrency> and the test for each.
- Not test-first: <step> because <reason (docs, pure refactor, visual check by screenshot compare)>.
- Other checks: <build, lint, types, screenshot compare for UI>.

## Critique
<the plan critic's riskiest assumption, weakest test and findings, and what changed in response>
```

## Rules

- Cite requirement numbers on every step and every proof line so a reviewer can check the plan against the spec line by line.
- Every step that changes behavior names the failing test that starts it (red, green, then any refactor as its own step); see `sdlc-feedback-loop`. Steps with no behavior change (docs, pure refactor) say so.
- The Test plan must be executable by the assistant (tests, build, screenshot compare); see `sdlc-feedback-loop`. One failing test per behavior and edge case; extend an existing test instead of adding a near-copy.
- Independent steps (different files) can run in parallel sessions; mark them so `sdlc-subagents` can split the work.
- Plans for changes the organization classes as higher risk go to a tech lead or architect before acceptance.
- Existing records (Jira, requirements tools) stay authoritative when the organization says so: note the record ID in the plan header, and write outcomes back through the connector. Otherwise link both ways.

## Auto mode

Once guardrails mature (a tuned `AGENTS.md`, policy skills, blocking hooks, a test suite the assistant can run), routine work with a tight spec and a small blast radius can run with fewer approval prompts (auto mode in Claude Code) after the plan is approved. The plan is still reviewed first.

## Measure

- Leading: share of changes that merge from the first implementation pass; time from plan approval to merged PR; critic findings accepted per plan.
- Lagging: rework cycles per change; how often the merged diff still matches `plan.md`.
