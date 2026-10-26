---
name: ai-native-sdlc
description: Entry point for the AI-native SDLC workflow (intent.md -> spec.md -> plan.md -> build -> verify -> review -> deploy -> monitor). Use when starting any new feature, change or incident, when asked "where do we start", "what's the next step", or "which play should we run", or when deciding which sdlc-* skill applies to the current state of a change.
---

# AI-native SDLC

Humans stay accountable for every judgment call. Claude does the drafting, checking and plumbing, and every stage leaves a committed Markdown artifact that the next stage consumes.

## The artifact chain

Each change gets a folder in the repo: `intent/<change-slug>/`.

| Artifact | Written by | Approved by | Skill |
|---|---|---|---|
| `intent.md` | Originator + Claude | Product owner | `sdlc-intent` |
| `spec.md` | Claude, from the intent | Product owner (policy owners for flagged concerns) | `sdlc-spec` |
| `plan.md` | Claude in plan mode | Engineer (tech lead if higher risk) | `sdlc-plan` |
| code + tests | Claude, verified by its own feedback loop | Code owner at PR | `sdlc-feedback-loop`, `sdlc-pr-review` |
| release | Pipeline + hooks | Release manager at the gate | `sdlc-hook-gate`, `sdlc-cicd` |
| new `intent.md` | Monitoring agent | Service owner | `sdlc-close-the-loop` |

## Pick the next step from what exists

Look in `intent/<slug>/` and the repo, then route:

1. No folder, only an idea or a ticket: `sdlc-intent`.
2. `intent.md` approved, no `spec.md`: `sdlc-spec`.
3. `spec.md` approved, no `plan.md`: `sdlc-plan`.
4. `plan.md` approved: build. Use `sdlc-feedback-loop` so the session verifies its own work, and `sdlc-subagents` to parallelise.
5. Diff ready: `sdlc-pr-review`.
6. Touching releases, migrations, protected paths: `sdlc-hook-gate`. Wiring any of it into pipelines: `sdlc-cicd`.
7. Something in production breached a band or an incident arrived: `sdlc-close-the-loop`, which writes a new `intent.md` and restarts at step 1.

If the repo already follows harness-engineering conventions (`AGENTS.md`, `docs/exec-plans/`, `ARCHITECTURE.md`) or the team wants them, use `sdlc-harness-translate` to map between the two layouts.

Foundations that any stage leans on: `sdlc-claude-md` (shared context), `sdlc-policy-skill` (organization policy as skills), `sdlc-evals` (regression tests for the agent's configuration).

## Where to start in a new repo

Six plays have no prerequisites, so any of them is a valid first move: `sdlc-intent`, `sdlc-plan`, `sdlc-claude-md`, `sdlc-policy-skill`, `sdlc-feedback-loop`, `sdlc-hook-gate`. Dependencies for the rest:

- `sdlc-subagents` needs `CLAUDE.md`; the feedback loop helps.
- `sdlc-evals` needs `CLAUDE.md` and the feedback loop.
- `sdlc-spec` needs `intent.md` and policy skills.
- `sdlc-pr-review` needs evals and subagents; skills help.
- `sdlc-cicd` needs PR review and hooks.
- `sdlc-close-the-loop` needs CI/CD and `intent.md`.

If a prerequisite is missing, say so and offer to run that skill first instead of working around it.

## Ground rules for every stage

- Put the artifact in the repo and name its author. Git history is the audit record.
- One source of truth per artifact. If Jira, ServiceNow or a requirements tool already holds the record, either declare it authoritative and write outcomes back through MCP, or link both ways (record ID in the file, commit SHA in the record).
- Advisory controls (skills, `CLAUDE.md`) get a deterministic backstop where the rule must always hold: a hook at the edit, a required check at the PR.
- A human makes each accept or reject decision that moves an artifact to the next stage. Never mark one approved on their behalf.
- Every play has a leading and a lagging indicator. Name them when you set the play up so the team can tell whether it worked.
