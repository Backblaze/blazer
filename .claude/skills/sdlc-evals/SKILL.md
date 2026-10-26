---
name: sdlc-evals
description: Build a continuous eval suite that regression-tests Claude's configuration (CLAUDE.md, skills, hooks, model, prompts) in CI, and turn production incidents into permanent evals. Use when asked to "add evals", "regression test our CLAUDE.md or skills", "turn this incident into an eval", or before merging changes to agent configuration.
---

# Continuous evals in CI

Evals are the stage-gate QA of an AI-native SDLC: a suite that runs whenever the agent's configuration changes and says whether the agent still does the work to the same standard. Treat it as a live suite. As models improve, cases stop discriminating and new ones must arrive from monitoring. Some teams prefer a set offline cadence; this skill sets up the continuous variant.

## Prerequisites

`CLAUDE.md` and a feedback loop (`sdlc-claude-md`, `sdlc-feedback-loop`). CI that can run Claude Code non-interactively and an API key with budget for eval runs.

## Steps

1. Collect 20 to 50 real tasks from recent work (merged PRs, past incidents), each with an accepted outcome.
2. Write each as `evals/<name>.json` (schema below): the prompt plus the checks that define acceptable (tests pass, lint clean, no existing test weakened, policy script passes, behavior unchanged).
3. Pick the commit to start from (`from`): the last commit before the original change, so each run sees the code as it stood then.
4. Copy `assets/check.sh` to `evals/check.sh` and `assets/agent-evals.yml` to `.github/workflows/agent-evals.yml` (adapt paths, runner and tools). Put helper scripts for checks beside the eval files; `check.sh` exports `$EVALS` so checks can call them.
5. Run one eval locally before turning on CI: confirm it passes against the known-good change and fails against a deliberately bad one.
6. Gate configuration changes on the result: the workflow runs on PRs touching `CLAUDE.md`, `.claude/**`, `evals/**` and on a nightly schedule. Make it a required check. The default threshold is every eval passing; a suite of 20 to 50 may set a lower one.
7. Every production incident gets an eval, written by the team that owned it. Use `sdlc-close-the-loop` to turn the incident into the prompt.

## Eval schema

```json
{
  "name": "short-kebab-name",
  "source": "Merged PR or incident this came from",
  "from": "<commit-sha>",
  "prompt": "The request as a user would phrase it. Run the tests before you finish.",
  "may_change": ["src/routes", "tests"],
  "checks": [
    { "name": "tests pass", "run": "make test" },
    { "name": "lint clean", "run": "make lint" },
    { "name": "no line removed from existing tests", "run": "! git diff HEAD -- tests | grep '^-[^-]'" },
    { "name": "policy script", "run": "scripts/check-policy.sh src/routes/thing.py" }
  ]
}
```

Prefer behavior checks (run the code, inspect logs or output) over text greps, so passing does not depend on how Claude names a variable.

## Integrity rules

- The checker and the evals are copied out before any run and run from the copy. The agent under test must not be able to rewrite its own grader.
- `check.sh` fails any run that changed files outside `may_change` (Git-ignored files excepted), so the agent cannot pass by editing the Makefile or the checks.
- Do not pass `--bare` to `claude -p` in the workflow: it skips `CLAUDE.md`, skills and hooks, which are the things under test.
- Keep the suite's API spend visible and capped; run the full suite on a schedule and a changed-config subset on PRs if cost demands.

## Reading failures

A failure should name the eval, the failing check and the first lines of output. A typical true positive: trimming `CLAUDE.md` drops a rule that a skill and a hook only partly backed up, and the behavior eval catches it. Restore the rule before merging.

## Governance and measures

The pass-rate threshold is a merge check; run logs make results comparable over time; the team that owns the configuration approves changes.

- Leading: eval pass rate over time; how long an incident takes to become a permanent eval.
- Lagging: regressions caught in CI vs found in production (incident tracker).
