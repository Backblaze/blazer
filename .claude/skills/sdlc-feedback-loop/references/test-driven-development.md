# Test-driven development with an AI assistant

How this playbook builds code: a failing test first, the smallest change that passes it, then a refactor only if one is needed. It applies to any AI assistant (Claude, Codex, Copilot, Gemini). This note collects why, and where each stage of the workflow enforces it. The skill that runs it is [`sdlc-feedback-loop`](../../sdlc-feedback-loop/SKILL.md).

Sources: Steve Kinney's course chapter [Test-Driven Development with Claude](https://stevekinney.com/courses/ai-development/test-driven-development-with-claude), the [Claude Code documentation](https://code.claude.com/docs/en/overview), and the Claude Code team's remarks in the [code review write-up](code-review-at-anthropic.md). The points below are our summary, not quotes; check the originals before citing them.

## Why it suits an agent

An agent works best when "done" is something it can run. A failing test is that: a finish line the agent can reach and check without asking, and that a reviewer can read in a few lines. Without one, "done" is the agent's own description of its work.

TDD also closes the agent's favorite shortcut. A test written after the code tends to describe what the code does, bugs included, and an agent asked to "add tests" will happily write tests that pass on the first run. Those prove nothing. A test only counts as proof if it failed first, for the right reason, and passed after the change without being edited.

## The loop

1. **Red.** Write one small test for the next behavior: a requirement in `spec.md`, a step in `plan.md`, or a bug's reproduction. Run it and look at the failing assertion before any production code changes. Failing for a different reason (a typo, a missing import elsewhere, a broken fixture) is not red.
2. **Green.** Make the smallest production change that passes it. Leave the test alone.
3. **Refactor, if needed.** Often it is not: a small, clear fix is a fine place to stop. A refactor is its own step with the tests unchanged, and it gets the full verification run again, because a green run before the refactor says nothing about the code after it.

## What to put in the prompt

Four things, each short:

- **Behavior:** what should be true, ideally the requirement number and its given, when, then.
- **Scope:** the test file to add to and the production files that may change.
- **Verification:** the exact commands, and "show me the failing assertion before you change the code".
- **Stop rule:** if something unrelated fails, stop and report it rather than editing other files to make it pass.

## Where the workflow enforces it

| Stage | What changes | Where |
|---|---|---|
| Spec | Each requirement carries a `Test:` line (given, when, then). A requirement nobody can test is not ready. | [`sdlc-spec`](../../sdlc-spec/SKILL.md) |
| Plan | A Test plan lists existing tests to extend, one new failing test per behavior and edge case, and where test-first does not fit and why. Each step names the failing test that starts it; refactors are separate steps. A plan critic attacks weak, missing and duplicate tests before approval. | [`sdlc-plan`](../../sdlc-plan/SKILL.md) |
| Build | Red, commit the test, green, separate refactor. For risky work, one session writes the tests and another implements against them. | [`sdlc-feedback-loop`](../../sdlc-feedback-loop/SKILL.md), [`sdlc-subagents`](../../sdlc-subagents/SKILL.md) |
| CI | `red_check.py` runs the PR's new tests against the base (must fail) and the change (must pass); code without a test fails unless the PR is a labelled refactor. | [`sdlc-feedback-loop`](../../sdlc-feedback-loop/SKILL.md#red-check-in-ci) |
| Review | The reviewer checks the red evidence, that no test was weakened, and that refactors are separate. | [`sdlc-pr-review`](../../sdlc-pr-review/SKILL.md) |
| PR | The Testing section shows the test written first and its failure before the change. | [PR template](../../sdlc-issue-templates/assets/pull_request_template.md) |
| Audit | A row reports whether CI runs the red check. | [`sdlc-review-audit`](../../sdlc-review-audit/SKILL.md) |

## Limits

- The red check shows a test fails without the change, not that it failed for the right reason. It prints the failing output; a person reads it.
- It also cannot show the order: a test written after the code passes it too. The order shows in the history, so keep the `test:` commit ahead of the code commit (do not squash it away), or have CI record the red run on the test commit.
- Some changes have nothing to test first: docs, formatting, a pure refactor. Say so in the PR instead of inventing a test.
- UI work often starts from a screenshot comparison instead of a unit test. The same rule applies: the comparison must fail before the change and pass after it.
