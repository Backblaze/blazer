---
name: sdlc-subagents
description: Split a plan into independent streams for parallel AI assistant sessions in separate git worktrees, and define reusable subagents (verifier, simplifier, researcher, plan critic) in the assistant's agents folder (.claude/agents/ for Claude Code). Use when asked to "work on these in parallel", "set up worktrees", "create a verifier agent", or when one engineer has several independent tasks in flight.
---

# Parallel sessions and subagents

A **parallel session** is a full instance of the AI assistant (Claude, Codex, Copilot, Gemini) on its own task in its own git worktree; sessions share nothing except the engineer steering them. A **subagent** is a scoped helper inside one session, with its own context window and tool limits, for jobs that recur across tasks. Sessions raise how many tasks are in flight; subagents keep each session focused. The engineer's job becomes steering and reviewing.

## Prerequisites

`AGENTS.md` exists (every session reads it; Claude Code through a `CLAUDE.md` that imports it). A feedback loop helps, since sessions that can verify themselves need less supervision (`sdlc-feedback-loop`). Permission settings should be tuned so safe commands do not stall sessions on prompts.

## Split the work

1. Read `plan.md` (or the task list). Group steps by the files they touch.
2. Independent groups become parallel sessions. Tasks that share files run in one session, sequentially.
3. Start with two or three sessions. The ceiling is how many streams the engineer can review properly; add more only while review keeps up.
4. Give each its own worktree and branch. In Claude Code:
   ```bash
   claude --worktree feature-auth
   claude --worktree fix-rate-limit
   ```
   With any assistant, create the worktree with git (`git worktree add -b feature-auth ../feature-auth`) and start the session inside it.
5. Give each session its slice of the plan and its own proof (tests or checks) so it can finish without the others.

## Define subagents

One file per agent in the assistant's agents folder: `.claude/agents/*.md` (Claude Code), `.codex/agents/*.toml` (Codex), `.github/agents/*.agent.md` (Copilot), `.gemini/agents/*.md` (Gemini CLI). Check them into Git so the team shares them. Each needs a name, a description of when to use it, and the minimal tools it may touch. The example uses Claude Code's format; other assistants: `references/ai-assistants.md`.

```markdown
---
name: verifier
description: Runs the app and checks the change works before the session reports done
tools: Bash, Read
---
Start the app with <run command>. Exercise the changed behavior and the two
nearest neighboring flows. Report what you ran, what you saw, and any
behavior that does not match plan.md. Do not fix anything; report only.
```

Useful starting set:

- **verifier**: fresh-context final check, so the verdict is not colored by the assumptions that produced the code. This complements, and does not replace, the feedback loop running throughout the task.
- **code-simplifier**: strips needless complexity after the main agent finishes.
- **researcher**: explores the codebase and reports back without flooding the main context.
- **plan-critic**: a skeptical engineering manager that attacks a draft `plan.md` and its test plan before a person approves it (over-engineering, missed reuse, hidden complexity, missing edge cases, weak or duplicate tests). It always names the riskiest assumption and the weakest test, and is told not to invent findings. Copy `assets/plan-critic.md` to `.claude/agents/` for Claude Code, or `assets/plan-critic.toml` to `.codex/agents/` for Codex (same instructions; Codex agents inherit the parent's sandbox, so run it from a read-only session). For Copilot or Gemini, port it to their agents folder and format; `sdlc-plan` runs it.
- **test author**: for risky changes, writes the failing tests from `spec.md` in its own context, so the implementer cannot shape the tests to its code (`sdlc-feedback-loop`).

## Governance

More sessions means more output, so controls must live in repo configuration (hooks, permission settings) that all sessions inherit. Activity is attributed to the engineer who ran it and exported via OpenTelemetry if configured.

## Measure

- Leading: concurrent sessions per engineer while review quality holds; share of the day spent steering vs waiting.
- Lagging: changes merged per engineer per week, read alongside the rework rate from PR history.
