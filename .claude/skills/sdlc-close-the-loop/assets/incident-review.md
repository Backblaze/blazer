# Incident review: <short title>

Date: <yyyy-mm-dd>. Owner: <name>. Severity: <critical | high | medium | low>. Status: <draft | reviewed>.

A review of the system, not of a person. The question is which layer should have caught this, and what we change so it does next time.

## What happened

<timeline in a few lines: change merged, deployed, first symptom, detection, mitigation, recovery. Link the PR, the release and the dashboards.>

## Impact

<who or what was affected, for how long, how many>

## How it was detected

<author, on-call, a control band, a customer, a dashboard. How long after the change?>

## Which layer should have caught it

Mark each layer: **caught** (it did), **should have** (it was in scope and missed), or **n/a** (out of scope for this change).

| Layer | Status | Why it missed, or what it did |
|---|---|---|
| 1. Verification before the PR (tests, build, screenshot, subagent review) | | |
| 2. AI review on the PR (`REVIEW.md` passes) | | |
| 3. Security review | | |
| 4. Automated checks (lint, types, tests, builds) | | |
| 5. Human approval (tier was right? approver saw the evidence?) | | |
| 6. Merge queue, flags, gradual release | | |
| 7. Detection and rollback (time to notice, time to roll back) | | |

## Was the risk tier right

<the tier the change received, the tier it deserved, and why. If the classifier or path list was wrong, say how.>

## What we encode

Every review ends with at least one change that makes the same failure harder. Pick from this list, link each one, and name an owner and a date.

- [ ] A rule in `AGENTS.md` or `REVIEW.md` (`sdlc-agent-instructions`, `sdlc-pr-review`)
- [ ] A lint, hook or required check that blocks the pattern (`sdlc-hook-gate`)
- [ ] A skill update, signed off by the policy owner (`sdlc-policy-skill`)
- [ ] A tier or path change in `risk-tiers.json` or `CODEOWNERS` (`sdlc-risk-tiers`)
- [ ] An eval from this incident, for the agent configuration and for the reviewer (`sdlc-evals`, `sdlc-review-tuning`)
- [ ] A faster rollback or a flag that should have existed (`sdlc-cicd`)
- [ ] A control band or alert on the symptom (`sdlc-close-the-loop`)

| Action | Link | Owner | Due |
|---|---|---|---|
| | | | |

## Intent follow-up

<if the incident points to work beyond a fix, link the new `intent.md` or issue; if it is only a fix, say so>

## What went well

<what limited the damage: a flag, a fast rollback, a clear dashboard>
