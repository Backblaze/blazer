---
name: sdlc-evals
description: Build a continuous eval suite that regression-tests the AI assistant's configuration (AGENTS.md, skills, hooks, model, prompts) in CI, and turn production incidents into permanent evals. Use when asked to "add evals", "regression test our AGENTS.md or skills", "turn this incident into an eval", or before merging changes to agent configuration.
---

# Continuous evals in CI

Evals are the stage-gate QA of an AI-native SDLC: a suite that runs whenever the agent's configuration changes and says whether the agent still does the work to the same standard. Treat it as a live suite. As models improve, cases stop discriminating and new ones must arrive from monitoring. Some teams prefer a set offline cadence; this skill sets up the continuous variant.

## Prerequisites

`AGENTS.md` and a feedback loop (`sdlc-agent-instructions`, `sdlc-feedback-loop`). CI that can run the AI assistant (Claude, Codex, Copilot, Gemini) non-interactively and an API key with budget for eval runs.

## Steps

1. Collect 20 to 50 real tasks from recent work (merged PRs, past incidents), each with an accepted outcome.
2. Write each as `evals/<name>.json` (schema below): the prompt plus the checks that define acceptable (tests pass, lint clean, no existing test weakened, policy script passes, behavior unchanged).
3. Pick the commit to start from (`from`): the last commit before the original change, so each run sees the code as it stood then.
4. Copy `assets/check.sh` and `assets/normalize_result.py` to `evals/`, and `assets/agent-evals.yml` to `.github/workflows/agent-evals.yml` (adapt paths, runner and tools). The workflow runs Claude Code. `normalize_result.py` turns the run's raw output into one result (`ok`, `final`, `why`) that `check.sh` grades, decided from the runtime's own signals (exit code, the completion event), never from what the model wrote; it reads Claude Code's JSON and Codex's `codex exec --json` events. For Codex, swap the install and the headless command (`references/ai-assistants.md`), set `ASSISTANT=codex`, and keep the same limits. Copilot and Gemini CLI need a parser added there first. Put helper scripts for checks beside the eval files; `check.sh` exports `$EVALS` so checks can call them.
5. Run one eval locally before turning on CI: confirm it passes against the known-good change and fails against a deliberately bad one.
6. Gate configuration changes on the result: the workflow runs on PRs touching any assistant's configuration (`AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, `REVIEW.md`, `.mcp.json`, `.claude/`, `.codex/`, `.gemini/`, `.agents/`, Copilot's files under `.github/`) or `evals/**`, and on a nightly schedule. Make it a required check. The default threshold is every eval passing; a suite of 20 to 50 may set a lower one.
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
    { "name": "no line removed from existing tests", "run": "! git diff $EVAL_BASE -- tests | grep '^-[^-]'" },
    { "name": "policy script", "run": "scripts/check-policy.sh src/routes/thing.py" }
  ]
}
```

Prefer behavior checks (run the code, inspect logs or output) over text greps, so passing does not depend on how the assistant names a variable.

## Integrity rules

- The checker and the evals are copied out before any run and run from the copy. The agent under test must not be able to rewrite its own grader.
- `check.sh` fails any run that changed files outside `may_change` since the base commit pinned before the run (`EVAL_BASE`), committed or not (Git-ignored files excepted), so the agent cannot pass by editing or committing changes to the Makefile or the checks.
- Tools (Claude Code flags): the run passes `--tools` and `--permission-mode dontAsk`, so the agent has only file tools, Bash and the Skill tool (the skills under test must be able to load), and anything the eval's settings do not allow is refused instead of prompting. Allow rules in the `.claude/settings.json` under test still apply, because they are part of the configuration being tested; review them like code. `--tools` limits built-in tools only, so the run also passes `--strict-mcp-config`, which loads no MCP server: a server from the repo or a user's settings cannot be called. To test an MCP setup, add `--mcp-config` with a test server on purpose.
- Credentials: the workflow checks out with `persist-credentials: false`, runs with `contents: read`, and grades with `ANTHROPIC_API_KEY` (or your assistant's key) removed from the environment. The agent's own `Bash(make test)` still inherits the key, so use a key dedicated to evals with a spend limit; GitHub does not pass secrets to workflows from forks.
- Each eval runs in a fresh clone, not a worktree, so a run cannot plant Git hooks or config in the workflow's checkout that fire during later evals, and `check.sh` turns off hooks and `core.fsmonitor` for its own Git calls. The checks still run the repo's code (`make test`), so this limits the damage rather than sandboxing it.
- The evals themselves are always-human: put `evals/` in `CODEOWNERS` and `always_human_paths` (`sdlc-risk-tiers`), since the suite runs the PR's own copy and a deleted or emptied eval would otherwise just stop running. The workflow also annotates each deleted eval on the PR.
- `check.sh` fails the run when Git cannot list the changed files, rather than treating that as no changes.
- The scope step skips the suite only when the PR's diff succeeds and touches no configuration; a failed diff runs it. A change to the eval workflow itself also runs it.
- The workflow fails a run that modified the graders in `/tmp/evals` (checksum manifest), checked after the agent and before any grader code runs (including `normalize_result.py`), and again after grading, because the checks themselves run repo code (`make test`). Once the graders differ, every later eval fails too. That detects tampering but cannot prevent it: a process the run left in the background could still change a grader between the check and its use. For untrusted prompts run the agent in a sandbox or container with the graders mounted read-only, or grade in a separate job. Grading runs with the assistants' API keys removed.
- Do not pass `--bare` to `claude -p` in the workflow: it skips `CLAUDE.md`, skills and hooks, which are the things under test. With any assistant, the run must load the instruction file, skills and hooks under test.
- Keep the suite's API spend visible and capped; run the full suite on a schedule and a changed-config subset on PRs if cost demands.

## Reading failures

A failure should name the eval, the failing check and the first lines of output. A typical true positive: trimming `AGENTS.md` drops a rule that a skill and a hook only partly backed up, and the behavior eval catches it. Restore the rule before merging.

## Evals for the reviewer itself

The AI reviewer is part of the agent's configuration, so it gets an eval set too. Build it from past PRs that caused incidents, with their fixes and reverts; have engineers who know each area confirm every bug and where it lives. Run the reviewer on that set daily and before any change to its prompts, tools, model or `REVIEW.md`, and drop changes that do not improve the score. Add a set of known-clean PRs to measure false positives. The full method is in `sdlc-review-tuning`.

## Governance and measures

The pass-rate threshold is a merge check; run logs make results comparable over time; the team that owns the configuration approves changes.

- Leading: eval pass rate over time; how long an incident takes to become a permanent eval.
- Lagging: regressions caught in CI vs found in production (incident tracker).
