---
name: sdlc-explain-module
description: Explain a slice of the codebase on demand before changing it (purpose, entry points, data flow, invariants, tests, history, gotchas), with every claim tied to file and line, optionally as a small visual artifact shaped to how the learner learns. Use when asked to "explain this module", "walk me through how X works", "I'm about to change Y, what should I know", when onboarding someone, or after an agent-written change nobody fully read.
---

# Explain a module

When machines do most of the reading, nobody holds the whole codebase in their head. The replacement is an explainer on the slice you are about to change: it takes minutes and reflects the code as it is now, unlike a doc that went stale. Reading trivial diffs teaches little; explaining the module you are about to change teaches much more. The pattern comes from the Claude Code team (this skill's `references/code-review-at-anthropic.md`).

## Steps

1. **Scope the slice.** Ask what the learner will change or needs to understand, and how deep (a map, or enough to modify safely). If the request is "the whole repo", propose a slice instead.
2. **Read before you write.** Read the code, the tests, `AGENTS.md`, and the recent history of the slice (`git log --follow`, recent PRs, linked issues, `intent/` and `plan.md` if present). Note what you did not read.
3. **Write the explainer** with the sections below. Keep it short enough to read in a few minutes.
4. **Tie every claim to evidence.** Cite `path:line` for each behavior you state. If something is inferred, not read, say "inferred". If you could not find the answer, say so; do not fill the gap.
5. **Check it.** Re-read the cited lines for each claim, and run the tests or a small example if that is cheap, so the explainer is not a plausible-sounding guess.
6. **Check understanding.** Offer two or three questions about the slice, or ask the learner to say back what they would change and where. Correct from the code, not from the explainer.

## Explainer sections

- **Purpose:** what this slice is for and who calls it, in two or three sentences.
- **Map:** the files that matter and what each does, in the order to read them.
- **Data flow:** one path through the slice from input to output, step by step.
- **Invariants and contracts:** what must stay true, with where it is enforced or tested.
- **Tests:** what is covered, what is not, and the command that runs them.
- **Gotchas:** surprising behavior, known sharp edges, recent bugs, with the commit or issue.
- **Safe to change vs risky:** which parts have tests and flags, which touch the always-human paths (`sdlc-risk-tiers`).
- **Open questions:** what the code does not settle.

## Visual version

When the learner asks, or the slice is easier to see than to read, build a small visual artifact (an HTML page or diagram) that teaches the module the way that person learns: a request path animation, a state machine, a dependency map, or a worked example they can step through. Ask how they learn best first. Apply the same rule: every box and arrow is traceable to code.

## Rules

- Do not change code. This is a reading task.
- Do not commit the explainer by default. It describes the code as of today and goes stale; regenerate it when needed. Save it only if the owner asks, and put the commit it describes in its header.
- Never present an explainer as the source of truth over the code and tests.
- For an agent-written change nobody fully read, explain the change and the module it touched, and flag what you could not verify.

## Measure

- Leading: time for a new engineer to make a first correct change in an area they did not know.
- Lagging: review comments that say "you did not know this module", and defects from changes to modules the author had not read.
