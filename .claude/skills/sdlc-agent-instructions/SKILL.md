---
name: sdlc-agent-instructions
description: Create, trim or audit a repo's agent instruction file (AGENTS.md, with a CLAUDE.md that imports it for Claude Code) so it holds exactly what a new joiner needs on day one (commands, conventions, architecture, recurring mistakes, verification). Use when a repo has no AGENTS.md or CLAUDE.md, when it has grown stale or long, when the AI assistant repeats a mistake, or when asked to "set up AGENTS.md", "set up CLAUDE.md" or "update our CLAUDE.md".
---

# The agent instruction file

`AGENTS.md` is the context a new joiner would need, read by the AI assistant (Claude, Codex, Copilot, Gemini) at the start of every session. It is version controlled and reviewed like code. Keep it under a page: the assistant reads all of it every session, and stale lines waste context.

## Which file each assistant reads

`AGENTS.md` at the repo root is the one file you edit. Codex and Copilot read it as is. Claude Code reads it through a `CLAUDE.md` whose only line is `@AGENTS.md`. Gemini CLI reads it once `context.fileName` in `.gemini/settings.json` includes `AGENTS.md`. Details and limits (nesting, size caps): `references/ai-assistants.md`.

## Create

1. Survey the repo: build files, `Makefile`/package scripts, CI config, README, directory layout, test setup.
2. Draft with the assistant's generator command if it has one (Claude Code: `/init`), or by hand, then cut it down to day-one essentials.
3. Verify every command by running it (or asking the engineer). Never list a command you have not seen work, and note a healthy output for each.
4. Ask the engineer who knows the codebase best for the "things the assistant gets wrong" list; pull more from recent PR review comments and reverted changes.
5. Place `AGENTS.md` at the repo root, add the `CLAUDE.md` and Gemini pointers above for the assistants the team uses, and commit, with code owners set so changes get reviewed.
6. While you are setting the repo up, check it has issue templates for features, bugs and tasks and a PR template; if not, add them (`sdlc-issue-templates`).

## Template

```markdown
# <Project>

## Commands
- Build: <cmd>
- Test: <cmd> (unit), <cmd> (integration, needs ...)
- Lint: <cmd> (runs in CI; fix before pushing)
- Run: <cmd> (<what comes up, on which ports>)

## Conventions
- <language/framework versions>
- <rules that matter: every endpoint needs a test, dates are ISO 8601, ...>

## Architecture
- <directory map, which component talks to which, hard limits (rate limits, ownership)>

## Things the assistant gets wrong
- <each mistake the team has corrected twice>

## Reviewing
- Review against REVIEW.md: what counts as Important, what to skip, how to report.

## Verifying your work
- Build: <cmd> (must finish with "<healthy output>")
- Test: <cmd> (all green; never skip or delete a failing test)
- Lint: <cmd> (zero warnings)
Run all three before reporting any task complete, and paste the output.
Test first: for a behavior change, write the failing test, run it and show the failing assertion, then change the code.
If a test fails, fix the code, not the test.
```

Add the project's goals and non-goals in a few lines so the reviewer can judge whether a change belongs. Keep the file short and point to other docs instead of copying them; the AI reviewer loads `AGENTS.md` and flags convention violations and stale lines.

If artifacts live in `intent/`, add a line saying `intent/` holds `intent.md`, `spec.md` and `plan.md` per change.

## Audit an existing file

- If the content lives in `CLAUDE.md`, `GEMINI.md` or `.github/copilot-instructions.md`, move it into `AGENTS.md` and leave `CLAUDE.md` as the `@AGENTS.md` import, so there is one copy to maintain. Show the move in the diff.
- Remove anything stale, obvious from the code, or already enforced by CI or a hook.
- Move multi-step procedures into a skill (`sdlc-policy-skill`); move must-always-hold rules behind a hook (`sdlc-hook-gate`).
- Flag missing sections, unverifiable commands, and contradictions with `README` or CI.
- Report the diff and why; do not silently drop lines someone may rely on.

## Maintenance rule

When the assistant makes the same mistake twice, the correction goes into `AGENTS.md` in the same PR. PR review should also flag changes that make `AGENTS.md` outdated (`sdlc-pr-review`). Trimming changes agent behavior, so run the evals when you have them (`sdlc-evals`).

## Measure

- Leading: how often the assistant repeats a mistake `AGENTS.md` should have caught (track corrections in Git history).
- Lagging: time to first merged PR for a new team member.
