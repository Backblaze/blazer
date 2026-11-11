# Translating between the AI-native SDLC playbook and harness engineering

Two bodies of guidance for working with coding agents:

- **The playbook** ([README](../README.md)): a process for the whole SDLC. Which artifact each stage produces, who approves it, how it is governed and measured.
- **Harness engineering** ([notes](harness-engineering-notes.md), summarizing OpenAI's article): the design of the repository and environment so an agent can work with little supervision. Knowledge layout, enforced architecture, legibility, garbage collection.

They agree on the thesis (humans steer, agents execute; the repo is the system of record; enforce mechanically) and are organized differently. This guide translates in both directions; the [comparison](playbook-vs-harness-comparison.md) covers how they differ and when to lean on each. The [`sdlc-harness-translate`](../.claude/skills/sdlc-harness-translate/SKILL.md) skill applies it to a real repo.

## 1. Vocabulary

| Playbook | Harness engineering | Fidelity |
|---|---|---|
| `AGENTS.md` (under a page; Claude Code reads it through a `CLAUDE.md` that imports it) | `AGENTS.md` (~100-line map) | Same file, both short. The playbook keeps commands, conventions, architecture, recurring mistakes and verification in it and moves procedures into skills; the harness makes it a table of contents that links out to `docs/`. |
| Skill (policy) | A rule in `docs/` (design doc, `SECURITY.md`) plus the lint that enforces it | Partial. A skill is advisory and loads on demand; a harness rule is read via the map. |
| Hook (guardrail, `PostToolUse`) | Custom linter or structural test | Close. Both feed a fix message back to the agent. |
| Hook (approval gate, `PreToolUse` ask/deny) | none | The harness favors minimal blocking gates. |
| Required CI check | Full gate (`make ci`) mirrored in CI | Same. |
| `intent/<slug>/intent.md` | `docs/product-specs/<slug>.md` | Close. |
| `intent/<slug>/spec.md` | `docs/design-docs/<slug>.md` | Close, minus the harness's verification status. |
| `intent/<slug>/plan.md` | `docs/exec-plans/active/<slug>.md` (then `completed/`) | Close. Harness adds a progress and decision log. |
| `spec.md` areas of concern / open questions | `docs/exec-plans/tech-debt-tracker.md` entries | Partial. Concerns have owners and block progress; tracker items are deferred work. |
| Feedback loop (tests, build, screenshots) | Application legibility (worktree-bootable app, browser tooling, logs and metrics queries) | The harness goes deeper (per-worktree observability). |
| Parallel sessions, worktrees, subagents | One app instance per worktree; agent-to-agent review | Close. |
| `REVIEW.md` passes | Agent review instructions and review agents | Close. |
| PR review with human code-owner approval | Agent-to-agent review, optional human review | **Different posture.** See section 4. |
| Evals on `AGENTS.md`, skills, hooks | Evaluation harnesses (agent-written) | Different aim. The playbook regression-tests the agent's configuration. |
| Control bands, incident to `intent.md` | Observability queries plus doc updates and tooling fixes | Partial. The playbook adds triggers and tiers. |
| Leading and lagging indicators per play | `QUALITY_SCORE.md` grades per domain | Different object. One measures the process, the other the code. |
| Managed settings, sandbox | none | Playbook only. |
| none | `ARCHITECTURE.md`, layer/dependency linter | Harness only. |
| none | Garbage-collection agents, golden principles | Harness only. |

## 2. Playbook to harness

Use this when a team following the playbook wants the harness's repo substrate.

| Playbook artifact | Becomes | Notes |
|---|---|---|
| `AGENTS.md` Commands, Conventions, Architecture | The same `AGENTS.md`, trimmed to a map that links to `ARCHITECTURE.md`, `docs/` | The file stays; its sections move out. |
| `AGENTS.md` "Things the assistant gets wrong" | Taste lints or `docs/design-docs/core-beliefs.md` entries | Promote repeated mistakes into lints. |
| `AGENTS.md` Verifying your work | The single full-gate command (`make ci`), mirrored in CI | |
| `intent.md` | `docs/product-specs/<slug>.md` | Keep the header (author, status). |
| `spec.md` | `docs/design-docs/<slug>.md`, plus a row in `docs/design-docs/index.md` | Add a verification status (current, stale, superseded). |
| `plan.md` | `docs/exec-plans/active/<slug>.md`, moved to `completed/` at merge | Add a progress log and a decision log. |
| Unresolved concerns | `docs/exec-plans/tech-debt-tracker.md` | Keep owner and date. Concerns blocking the spec stay in the spec. |
| Policy skill | Policy in `docs/` (or `SECURITY.md` etc.) plus a lint | Keep the skill too if you want on-demand loading. |
| Build hook script | Linter wired into `make ci` and pre-commit | Keep the remediation message. |
| `REVIEW.md` | Review-agent instructions in `docs/` or a repo skill | |
| Control bands | A scheduled drift scan, and `docs/RELIABILITY.md` | |
| Missing | `ARCHITECTURE.md`, layer linter, `QUALITY_SCORE.md`, doc-gardening job | Create. See [section 5](#5-gaps-neither-side-covers). |

## 3. Harness to playbook

Use this when a team with a harness-style repo wants the playbook's flow and governance.

| Harness artifact | Becomes | Notes |
|---|---|---|
| `AGENTS.md` map | The same `AGENTS.md` (keep the map; add the Commands and Verifying your work sections) | If the repo has no `CLAUDE.md` import (section 1), add it. |
| `docs/product-specs/<slug>.md` | `intent/<slug>/intent.md` | Rewrite into the intent template (problem, outcome, constraints, open questions). |
| `docs/design-docs/<slug>.md` | `intent/<slug>/spec.md` | Number the requirements (R1...) and add the constraint table. |
| `docs/exec-plans/active/<slug>.md` | `intent/<slug>/plan.md` | Add Test plan lines citing requirement numbers. Completed plans keep their folder. |
| `core-beliefs.md`, golden principles | Policy skills, or `AGENTS.md` Conventions | A skill when a named owner must sign off on changes. |
| `ARCHITECTURE.md` plus layer linter | `AGENTS.md` Architecture section, a post-edit hook (`PostToolUse` in Claude Code) running the linter, a required CI check | Use the skill-plus-hook-plus-check pattern from `sdlc-policy-skill`. |
| Taste lints | Hooks and required checks | Exit 2 returns the lint message to the AI assistant (Claude, Codex, Copilot, Gemini). |
| `QUALITY_SCORE.md` | Leading and lagging indicators per play | Different instrument. Keep both. |
| Doc-gardening and GC agents | Scheduled headless job (`claude -p` or an [equivalent](ai-assistants.md)), tier "propose a PR" | Fits Stage 6 and `sdlc-cicd`. |
| Tech-debt tracker | Concerns and open questions with owners, or new `intent.md` files | A tracker entry that matters becomes an intent. |
| Minimal merge gates | Autonomy tiers and `sdlc-hook-gate` | A decision, not a rename. See section 4. |

## 4. The one thing that doesn't translate mechanically: the gates

The harness runs minimal blocking gates and lets agents merge their own PRs. The playbook requires a human code-owner approval and keeps approval gates, so the agent that wrote the code cannot approve it. Neither side is wrong. They sit at different points on a dial:

| Setting | Posture | Fits |
|---|---|---|
| Harness end | Short-lived PRs, agent self-merge, follow-up runs for flaky tests | Small team, internal product, cheap corrections |
| Middle | Self-merge for routine changes under a tight spec, human approval for anything classed higher risk. The Claude Code team's [risk tiers](code-review-at-anthropic.md#5-risk-scaled-human-approval) are a worked example (see `sdlc-risk-tiers`). | Most teams |
| Playbook end | Human code-owner approval on every PR, managed settings, approval hooks on release | Regulated, audited, high blast radius |

Translating in either direction means someone picks the setting. Ask the owner of change management, and record the answer in `AGENTS.md`. The playbook's auto mode and per-environment tiers are the route toward the harness end as guardrails mature.

## 5. Gaps neither side covers

Translating exposes what each convention lacks:

- **In the playbook, not in the harness:** artifact approval chain, policy owners, intent from non-engineers, managed settings and sandboxing, config evals, measures per play.
- **In the harness, not in the playbook:** enforced architecture layers, taste lints, golden principles, code-drift garbage collection, a quality score, catalogued design docs with verification status.

A repo that adopts both should add the missing half instead of translating it away.

## 6. Same change, both ways

A change "rate-limit the export endpoint":

```text
Playbook                                  Harness
AGENTS.md   (commands, conventions)       AGENTS.md (map)
intent/export-rate-limit/                 docs/
  intent.md   (problem, constraints)        product-specs/export-rate-limit.md
  spec.md     (R1..R3, concerns)            design-docs/export-rate-limit.md  (+ index row)
  plan.md     (steps cite R1..R3)           exec-plans/active/export-rate-limit.md
REVIEW.md                                   ARCHITECTURE.md  (layers the change must respect)
.claude/skills/<policy>/                  lint: layer + logging rules
.claude/hooks/<check>.sh                  make ci  (runs the lints, mirrored in CI)
```

Moving between the two is a set of renames and rewrites, as in sections 2 and 3, plus the gate decision.
