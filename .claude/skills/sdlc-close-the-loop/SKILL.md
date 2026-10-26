---
name: sdlc-close-the-loop
description: Monitor production or pipeline metrics with deterministic control bands and, on a breach, have Claude diagnose it within tiered permissions and write the finding as a new intent.md that restarts the SDLC. Also use to turn an incident, alert, ticket or Slack report into intent.md and an eval. Use when asked to "set up control bands", "alert and auto-diagnose", "turn this incident into an intent", or "close the loop".
---

# Closing the loop on metrics

Every earlier stage needs a person to start it. Maintenance runs headless: a trigger (control-band breach, ticket, channel message, schedule) invokes Claude, which diagnoses, acts only through gated routes, and writes what it finds as `intent.md`. People triage and review that work; they no longer have to start it. Detection stays entirely deterministic, with no model involved.

## Prerequisites

`intent.md` (`sdlc-intent`), CI/CD with a rehearsed rollback (`sdlc-cicd`), approval hooks as the action boundary (`sdlc-hook-gate`), PR review (`sdlc-pr-review`).

## Control bands

A band is a range around a metric's normal level: the mean of the last 30 days of hourly readings, with sigma (σ) the standard deviation. Noise alone puts roughly one reading in six above 1σ, one in 44 above 2σ, one in 740 above 3σ (bell-curve assumption), which is why 1σ only logs and why even 3σ stays on gated routes. The Western Electric rules catch slow drift that never crosses 3σ: four of the last five readings above 1σ, or eight in a row above the mean (both count as 2σ).

## Set up

1. Pick one metric with a stable rolling baseline (CI test failure rate, post-deploy 5xx rate, PR cycle time) and a store the script can query (Prometheus, the CI system's API).
2. Copy `assets/detect.py` into the repo (e.g. `ops/detect.py`) and `assets/bands.yaml` beside it. The script reads a JSON list of readings on stdin, oldest first, and prints the worst tier the newest reading lands in. Version control it and unit test it. Tune `MIN_SIGMA` to the metric's units so a flat baseline does not turn every blip into a breach.
3. Define tiers in `bands.yaml`: 1σ logs; 2σ invokes Claude read-only to diagnose; 3σ lets Claude act only by opening a PR into the review gate or triggering a pre-approved runbook (e.g. `rollback-deploy`).
4. Run it from a trigger: scheduled workflow, webhook from the monitoring stack, or cron inside the network. Claude runs stateless and non-interactive (CI step, or an Agent SDK service in a sandboxed container), with the tier's tools only:
   ```bash
   tier=$(python3 ops/detect.py < readings.json | jq -r .tier)
   # look up $tier in bands.yaml, then e.g. for diagnose:
   claude -p "Diagnose the breach in breach.json. Write intent.md." \
     --allowedTools "Read,Grep,Bash(gh run view *)" --permission-mode dontAsk
   ```
5. The agent writes `intent/<slug>/intent.md` in the Stage 1 format (below) and commits or opens a PR. From there the finding flows through the normal stages.
6. The service owner or on-call engineer triages the queue: fix now, schedule, or dismiss. Dismissals tune the bands.
7. When a fix ships, add an eval for the incident (`sdlc-evals`) so the class of issue is guarded against going forward.

## Incident intent template

```markdown
# Intent: <metric> breach after <release/trigger>
Author: monitoring agent (<metric>, <tier>). Status: draft.

## Problem
<reading vs 30-day mean, when it started, what changed in that window (release, config), evidence>

## Proposed outcome
<what good looks like, framed for the product owner>

## Affected users and systems
<who and what>

## Constraints
<limits that make the obvious fix wrong: rate limits, spec requirements by number>

## Action already taken
<runbook or rollback executed, time, result>

## Open questions
<who asked for the change that caused this; is the old behavior acceptable>
```

## Rules

- Never let the agent take an action beyond its tier. Anything past a runbook goes through a PR and the review gate.
- Log every invocation, finding and triage decision with timestamps.
- Verify recovery before closing: confirm the metric is back at baseline and say so in the thread or intent.
- Claims must cite evidence (readings, release SHA, log lines); do not speculate about root cause as fact.

## Other triggers: chat and tickets

Incidents also arrive in Slack or Teams, or as tickets over MCP. A Claude app in the channel (for example Claude Tag in Slack, public beta at time of writing) acts as first responder under its own identity, keeps the conversation and history in the channel for audit, verifies the metric against baseline through MCP, and writes the post-mortem to a version-controlled lessons file future investigations can read. Small, well-bounded fixes come back as PRs through the review gate; larger work is written up as `intent.md` for `sdlc-intent`'s next stage, so the loop feeds itself.

## Measure

- Leading: time from band breach to an `intent.md` in the triage queue vs the old incident-to-post-mortem time (detection log has the timestamp and tier).
- Lagging: share of findings that become merged fixes; repeat incidents of the same class (should fall as evals accumulate).
