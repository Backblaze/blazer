---
name: sdlc-harness-translate
description: Translate a repo or artifact between the AI-native SDLC playbook convention (CLAUDE.md, intent/<slug>/intent.md+spec.md+plan.md, skills, hooks, REVIEW.md) and the harness-engineering convention (AGENTS.md map, docs/ system of record with product-specs, design-docs, exec-plans, ARCHITECTURE.md, layer linters, garbage-collection jobs), in either direction. Use when asked to "convert this repo to the harness layout", "adopt the playbook in our harness repo", "map AGENTS.md to CLAUDE.md", or to compare the two approaches.
---

# Translate between the playbook and harness engineering

The playbook organizes a process (artifacts, approvals, governance). Harness engineering organizes a repo (knowledge layout, enforced architecture, garbage collection). The mapping tables live in `reference/playbook-harness-translation.md` in the playbook folder; if that file is not available, use the summary below.

## Quick map

| Playbook | Harness |
|---|---|
| `CLAUDE.md` | `AGENTS.md` (~100-line map) |
| `intent/<slug>/intent.md` | `docs/product-specs/<slug>.md` |
| `intent/<slug>/spec.md` | `docs/design-docs/<slug>.md` + row in `docs/design-docs/index.md` |
| `intent/<slug>/plan.md` | `docs/exec-plans/active/<slug>.md` (-> `completed/` at merge) |
| open concerns | `docs/exec-plans/tech-debt-tracker.md` |
| policy skill | doc in `docs/` + lint |
| guardrail hook / required check | custom linter / `make ci` |
| approval-gate hook, human code-owner approval | no counterpart (a posture decision) |
| control-band job | scheduled drift scan |
| none | `ARCHITECTURE.md`, layer linter, `QUALITY_SCORE.md` |

## Procedure

1. **Detect the source convention.** Playbook signs: `CLAUDE.md`, `intent/`, `.claude/skills`, `REVIEW.md`. Harness signs: `AGENTS.md`, `docs/exec-plans/`, `docs/design-docs/`, `ARCHITECTURE.md`. A repo may have both; ask which direction the user wants.
2. **Inventory and propose before touching files.** List every source artifact, its target path, the transformation (move, rewrite, split, merge), and the gaps. Show it as a table and wait for confirmation.
3. **Ask the one decision that cannot be mapped:** the merge-gate posture (human code-owner approval on every PR, agent self-merge for routine changes, or in between). Record the answer in `CLAUDE.md` / `AGENTS.md`. Never loosen or tighten gates without an explicit answer.
4. **Execute with history preserved.** Use `git mv` for moves; rewrite only where the template differs. Update every inbound link. Do not delete content: when a source section has no target, leave it in place and log it as a gap.
5. **Keep Claude Code working.** Claude Code loads `CLAUDE.md`. When making `AGENTS.md` canonical, leave a one-line `CLAUDE.md` that points at it.
6. **Fill gaps as stubs, flagged.** Missing counterparts get a small stub marked TODO for the owner (see below), not invented content.
7. **Verify.** Check that no relative link broke, every moved artifact appears once, and the repo's build and gate commands still pass. Report what moved, what was rewritten, what was left, and what needs a human.

## Playbook -> harness: field rules

- `intent.md` -> product spec. Keep the author and status header.
- `spec.md` -> design doc. Keep requirement numbers (R1..) so plans still cite them. Add a verification-status row (`current`, `stale`, `superseded`) in `docs/design-docs/index.md`.
- `plan.md` -> exec plan. Add `## Progress` and `## Decision log` sections. Move to `completed/` once merged.
- Unresolved spec concerns that are being deferred -> tracker rows with id, severity, date and owner. Concerns that block the spec stay in the spec.
- `CLAUDE.md` -> trim to a map. Commands, conventions and "things Claude gets wrong" move to the docs they belong in, with links.
- Repeated "things Claude gets wrong" are candidates for lints with a remediation message.

## Harness -> playbook: field rules

- Product spec -> `intent.md`: rewrite into problem, proposed outcome, affected users and systems, constraints, open questions. Do not invent answers; unknowns go under open questions.
- Design doc -> `spec.md`: number the requirements and build the constraint-coverage table. Surface policy conflicts as concerns with owners instead of resolving them silently.
- Exec plan -> `plan.md`: add steps citing R-numbers and a Proof section. Keep the decision log as a section.
- `ARCHITECTURE.md` + layer linter -> `CLAUDE.md` Architecture section, plus a `PostToolUse` hook running the linter on the changed file and the same script as a required CI check (see `sdlc-policy-skill`, `sdlc-hook-gate`).
- Taste lints -> hooks whose stderr message tells Claude how to fix the violation.
- GC / doc-gardening agents -> a scheduled `claude -p` job whose permitted route is opening a PR (see `sdlc-cicd`, `sdlc-close-the-loop`). Use a drift count as the metric if you want bands.
- `QUALITY_SCORE.md` stays. Add leading and lagging indicators per play alongside it.

## Gaps to flag as stubs

- Toward the harness: `ARCHITECTURE.md` (domains, layers, allowed edges), a layer/dependency linter with remediation messages, `QUALITY_SCORE.md`, a scheduled doc-gardening job, a tech-debt tracker, `docs/design-docs/index.md`.
- Toward the playbook: artifact approvals (who accepts each intent and spec), policy owners, managed settings and sandbox, config evals (`sdlc-evals`), indicators per play.

## Rules

- This is a restructuring task. Do not change application code, behavior or CI logic except to repoint paths.
- Say plainly when a mapping is lossy (for example, a skill that loads on demand becomes a doc read through the map).
- Do not claim a counterpart exists when it does not.
