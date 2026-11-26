---
type: reference
title: "Change intents"
description: "Each non-trivial change gets a folder docs/product-specs/<change-slug>/ holding its intent and spec."
status: active
owner: ["@goanpeca", "@sophiecarreras", "@ffumero2003", "@david-santacruz-backblaze"]
stale_after: 2027-05-26T00:00:00+00:00
---
# Change intents

Each non-trivial change gets a folder `docs/product-specs/<change-slug>/` holding its intent and spec. The three files are written in order; `plan.md` lives in `docs/exec-plans/active/<change-slug>/plan.md` while in progress and moves to `docs/exec-plans/completed/<change-slug>/plan.md` after merge. A
human approves each one before the next is written; the approval is a commit or a PR review, not an
agent's edit to the status line.

| File | Written by | Approved by | Holds |
| --- | --- | --- | --- |
| `intent.md` | Originator (with Claude) | Maintainer | Problem, outcome, constraints, open questions. No design. |
| `spec.md` | Claude, from the intent | Maintainer; area owner for flagged concerns | Numbered requirements R1.., design, constraint table, concerns. |
| `docs/exec-plans/active/<change-slug>/plan.md` (moves to `docs/exec-plans/completed/<change-slug>/plan.md` after merge) | Claude in plan mode | Engineer doing the work | Files, ordered steps citing R numbers, risks, proof. |

Plans live in `docs/exec-plans/active/<change-slug>/` while in progress and `docs/exec-plans/completed/<change-slug>/` after merge.

Small fixes that already have a GitHub issue with a clear reproduction can skip these artifacts and start from a
failing test. Link the GitHub issue in the `intent.md` header when one exists, and the intent folder in the
issue.

The `ai-native-sdlc` skill in `.claude/skills/` looks at what exists and routes to the next step.

## Templates

### intent.md

```markdown
# Intent: <short title>
Author: <name>. Issue: #<n>. Status: draft.

## Problem
## Proposed outcome
## Affected users and APIs
## Constraints
## Open questions
```

### spec.md

```markdown
# Spec: <title>
From: intent.md (<author>). Status: ready for review.

## Requirements
- R1. <testable statement: request shape, returned value, error>

## Design
<which b2/ and base/ functions change, new options, wire fields>

## How each constraint is met
| Constraint | Met by |
| --- | --- |

## Areas of concern
1. <concern>. Owner: <@handle>.
```

### plan.md

```markdown
# Plan: <title>

## Files that change
## Order of work
1. <step> (R1).
## Risks
## Proof
- R1: <test name, and whether it fails when the change is reverted>
```
