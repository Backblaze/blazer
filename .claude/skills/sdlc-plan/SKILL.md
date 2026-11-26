---
name: sdlc-plan
description: Produce and commit a plan.md before any code is written, working in plan mode from the approved spec.md and intent.md. Use when starting implementation of a change, when asked to "plan this", "make an implementation plan", or before editing code for a change that has an intent and a spec; also use to keep plan.md in sync when implementation departs from it.
---

# Plan mode as the default starting point

Work starts with a written, reviewable plan. In plan mode, Claude can read the codebase without changing it. Course corrections are edits to a document.

## Steps

1. Work in plan mode (Shift+Tab, or start the session with `--permission-mode plan`). Do not edit files until the engineer accepts the plan.
2. Read `docs/product-specs/<slug>/intent.md`, `docs/product-specs/<slug>/spec.md` (the requirement numbers R1.. and concerns), `CLAUDE.md` and the code the change touches. If a spec exists, a plan without it is a smell. If neither exists, ask whether to run `sdlc-intent` / `sdlc-spec` first or to plan from a plain description.
3. Draft the plan using the template below.
4. Let the engineer interrogate the plan. Be ready to answer: what could this break, which step is riskiest, which alternatives were rejected and why.
5. Iterate until someone who never saw the conversation could implement the change from the plan alone.
6. Commit the approved plan as `docs/exec-plans/active/<slug>/plan.md`. Then implement. With a solid plan, this is often one pass.
7. If implementation departs from the plan, update `plan.md` in the same commit. Consider a hook that flags source changes with no matching plan update.

## Template

```markdown
# Plan: <title> (from intent.md and spec.md)

## Files that change
<paths, marking new files>

## Order of work
1. <step> (R1, R2).
2. <step> (R4).

## Risks
<what could break; cite spec concerns by number>

## Proof
<which tests or checks cover which requirements, and how UI changes are verified>
```

## Rules

- Cite requirement numbers on every step and every proof line. This lets a reviewer check the plan against the spec line by line.
- The Proof section must be executable by Claude (tests, build, screenshot compare); see `sdlc-feedback-loop`.
- Independent steps (different files) can run in parallel sessions. Mark them so `sdlc-subagents` can split the work.
- Plans for changes the organization classes as higher risk go to a tech lead or architect before acceptance.
- When the organization says existing records (Jira, requirements tools) stay authoritative, note the record ID in the plan header. Write outcomes back through the connector. Otherwise link both ways.

## Auto mode

Once guardrails mature (a tuned `CLAUDE.md`, policy skills, blocking hooks, a test suite Claude can run), routine work with a tight spec and a small blast radius can run in auto mode after the plan is approved. The plan is still reviewed first.

## Measure

- Leading: share of changes that merge from the first implementation pass; time from plan approval to merged PR.
- Lagging: rework cycles per change; how often the merged diff still matches `plan.md`.
