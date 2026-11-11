---
name: sdlc-feedback-loop
description: Build test-first (red, green, then a separate refactor) and give the AI assistant a way to verify its own work (tests, build, lint, screenshot diff) that it cannot weaken, with a red check that shows new tests fail without the change. Use when starting implementation or a bug fix, when asked to "set up verification", "do TDD", "write the test first", or when the assistant reports work done without proof; also for UI changes that need a visual check.
---

# Give the assistant a feedback loop

Always give the AI assistant (Claude, Codex, Copilot, Gemini) a way to check its own work before a person sees it. A late signal (CI minutes later, a tester days later) turns the engineer into the bottleneck.

## Set up

1. Wrap verification in one command per concern that exits non-zero on failure (`make test`, `npm test`, `make lint`, `make build`). Create the targets if only a command sequence exists today.
2. List them in `AGENTS.md` (see `sdlc-agent-instructions`) with a sample of healthy output, plus the instruction: run the tests before reporting a task complete and show the output.
3. State a quantifiable target for the task so the assistant can check without asking: "all tests in X pass", "screenshot matches the attached mock", "the endpoint returns 200 with the new field". Take targets from the Test plan in `plan.md` when it exists.
4. For UI work, give the assistant a browser or screenshot tool (e.g. via MCP) and the mock: implement, screenshot, compare, adjust. Two or three rounds is normal.

## Test-driven by default

Every change to behavior, feature or bug fix, starts from a failing test. The test is the executable definition of done, so the agent has a finish line it cannot argue with. Background and sources: `references/test-driven-development.md`.

1. **Red.** Write the smallest test for the next piece of behavior: a requirement from `spec.md` (its `Test:` line), the next step of `plan.md`, or the bug's reproduction steps. Put it in the closest existing test file. Run it and show the failing assertion before touching production code. A test that passes already, or fails for an unrelated reason (import error in another module, missing fixture), is not red: fix the test.
2. **Commit the test** (`test: ...`). A test that existed before the code, and that the agent did not rewrite to fit the code, is the evidence.
3. **Green.** Make the smallest production change that passes it. Do not edit the test in this step. Run the test, then the whole suite.
4. **Refactor, separately and only if needed.** Ask whether the code needs it; small and clear is a fine place to stop. If you refactor, keep it in its own commit, change no test, and run the full verification again: green before a refactor says nothing about the refactor.
5. Repeat per requirement or plan step until the Test plan in `plan.md` is met.

While building:

- Fix the root cause, not the symptom. If the cause is somewhere else, say so rather than patching around it.
- Make the small change in the codebase's existing style; extend what exists before adding new code, and leave no dead code, unused options or commented-out blocks behind.
- Cover security-relevant behavior and edge cases with tests, not just the happy path.
- Never delete data, or weaken a migration, fixture or test, to make a check pass.

Changes with no behavior to test (docs, comments, a pure refactor, formatting) skip red; say so in the PR's Testing section, and label a pure refactor `refactor` so the red check holds the tests unchanged instead.

### Asking the assistant for it

State four things: the behavior to prove, the scope (which files may change), the verification commands, and when to stop. For example, "Write a failing test in `tests/test_billing.py` for R3 (a refund over the original charge is rejected), run `make test` and show the failing assertion, then make the smallest change in `billing/refunds.py` that passes it, and run `make test lint`. If something unrelated fails, stop and report it instead of editing other files."

### Separate the test author for higher-risk work

An agent that writes both the test and the code can write a test shaped to its code. For risky changes have one session or subagent write the tests from `spec.md` and commit them, and a second implement against those tests without editing them (`sdlc-subagents`).

### Red check in CI

`assets/red_check.py` shows that the new tests detect the change, even when commits are squashed. It does not show they were written first: a test added after the code passes it too. For evidence of the order, keep the `test:` commit before the code commit (merge without squashing, or have CI record the red run on the test commit). For the PR's diff it runs the changed test files against the merge base (they must fail, which shows they test the change) and against the change (they must pass). A red run that fails to build or load the tests (a compile error, a missing import) fails the check, since no assertion ran: add the new API as a stub first so the test compiles and fails on its assertion. Code changed with no test changed fails; a PR labelled `refactor` must leave tests untouched instead. A tests-only PR skips red but its tests must pass, and in the merge queue the job runs the suite on the combined commit (`--green-only`).

```bash
python3 red_check.py --base origin/main --test-cmd "go test -race ./... && make scripts-test"
```

Copy it to `.github/scripts/` and `assets/red-check.yml` to `.github/workflows/`, set `--test-cmd`, and make the job a required check. The red run uses a fresh checkout of the base, so the test command must install what it needs. It prints the end of the failing output; the reviewer confirms the failure is the right one.

## Protect the loop

An agent fixing code must not be able to weaken the check on that code. Either:

- block edits to test files during the green step with a pre-tool hook (Claude Code: `PreToolUse`; other assistants: `references/ai-assistants.md`; `sdlc-hook-gate` and `sdlc-policy-skill` show the pattern), or
- check the diff in review and reject a change that weakens a test to make it pass (`sdlc-pr-review`), and
- run the red check as a required check, so a test that never failed cannot count as proof.

## Report format

When finishing, report in this order, so the person reading it sees what needs them first:

1. Blockers and judgment calls: anything that stopped you, and every decision you made that the plan did not settle.
2. What changed and why, by file, citing requirement numbers.
3. Evidence: the failing assertion each new test showed before the change, then the literal output of the build, test and lint commands (or the screenshot diff). Evidence comes from the toolchain, not from the agent's summary.
4. What the reviewer should check by hand, and what the checks cannot see.
5. Follow-ups and next steps, most important first.
6. A commit message in the repo's convention, or a note that the work is not ready to commit.

Update docs and `plan.md` in the same change when the code departed from them.

## Review before the PR

Layered review starts before the PR exists. Before opening it:

1. Have the session show output (tests, build, screenshot compare), not a claim of success.
2. Run a bug-hunt pass, a simplify pass and an end-to-end behavior check, as skills if you have them (in Claude Code, `/code-review` and `/simplify`; or your own).
3. Have a subagent review the diff in a fresh context (`sdlc-subagents`), so the verdict is not colored by the assumptions that produced the code. For UI changes, check the result against the design guidelines.
4. When a pass finds the same kind of problem repeatedly, fix it at the source: a rule in `AGENTS.md`, a lint, a skill. Do not rely on repeating the comment on each PR.

Giving the agent something it can run is the largest single factor in result quality, according to the Claude Code team (`references/code-review-at-anthropic.md`).

## Governance

Verification before "done" and the block on editing tests are enforced as hooks where the organization wants them guaranteed. Evidence lives in the session transcript and in the PR's check run; the code owner reviews intent and risk because mechanical evidence is already attached.

## Measure

- Leading: first-pass CI success rate of agent-written changes; share of PRs passing the red check on the first run.
- Lagging: review time per PR (should fall) and change failure rate from the incident tracker.
