---
name: sdlc-issue-templates
description: Make sure every repo has GitHub issue templates for features/enhancements, bugs and tasks (plus a pull request template), adapted to the repo, and route incoming issues into intent.md. Use whenever setting up or onboarding a repo, starting a change in a repo without .github/ISSUE_TEMPLATE, filing or triaging an issue, or when asked to "add issue templates", "standardize our issues", or "set up the repo for this workflow".
---

# Issue templates: feature, bug, task

Work enters the chain through issues, so the issue form is the first artifact. Every repo that follows this workflow always has three issue forms, and a pull request template to match. Structured intake is what lets the AI assistant (Claude, Codex, Copilot, Gemini) turn an issue into `intent.md` or a failing test without a round of questions.

## Rule

When you start work in a repo, or set one up, check for `.github/ISSUE_TEMPLATE/` (and `.github/pull_request_template.md`). If any of the three forms is missing, say so and add them with this skill before continuing, unless the owner says no. Do not skip this because the repo is small or the task is quick.

## What each form is for

| Form | File | Label | Feeds |
|---|---|---|---|
| Feature or enhancement | `feature.yml` | `enhancement` | `intent.md` (`sdlc-intent`): problem, outcome, affected users and systems, constraints, open questions map one to one |
| Bug report | `bug.yml` | `bug` | A failing test first, then the fix (`sdlc-feedback-loop`). Incident findings from monitoring use it too (`sdlc-close-the-loop`) |
| Task | `task.yml` | `task` | A bounded piece of decided work. Its definition of done is the check the assistant runs before reporting complete |

`config.yml` turns off blank issues so every issue picks a form.

## Steps

1. Look at `.github/ISSUE_TEMPLATE/` and `.github/pull_request_template.md`. Report which exist. If the repo already has templates, extend them, do not overwrite: keep the owner's fields, add what is missing, and show the diff.
2. Copy the files from this skill's `assets/` into the repo: `assets/ISSUE_TEMPLATE/{feature,bug,task,config}.yml` to `.github/ISSUE_TEMPLATE/`, and `assets/pull_request_template.md` to `.github/pull_request_template.md`.
3. Adapt to the repo, with the owner:
   - Labels: run `gh label list` and match the repo's names (for example `type: bug`). Creating labels changes the repo's settings on GitHub, so ask before creating any. GitHub ignores a label that does not exist, so a missing label is harmless but loses the automatic tagging.
   - Fields: add the repo's own needs (component dropdown, environment, affected customers), and remove what does not apply.
   - Security: if the repo has a security policy, add a `contact_links` entry in `config.yml` so vulnerabilities go to the private channel, not a public bug.
   - Title prefixes (`[Feature]`, `[Bug]`, `[Task]`) are optional; keep them if the repo sorts by title.
4. Validate: the files parse as YAML, each form has `name`, `description` and a non-empty `body`, and any `required` field has a label. GitHub only renders valid forms, so a typo silently falls back to nothing.
5. Commit with a one-line message in the repo's style (for example `chore: add issue and PR templates`). Do not open a PR or push unless asked.
6. Non-GitHub trackers: use the same fields as Markdown templates (GitLab: `.gitlab/issue_templates/*.md`). Keep the section names identical so they map to `intent.md` the same way.

## Turning an issue into the chain

- **Feature issue**: read it, then run `sdlc-intent` seeded from the form fields. Ask only about what the form left empty. Link both ways: the issue number goes in the `intent.md` header, and the issue gets a comment with the intent file's path.
- **Bug issue**: reproduce it as a failing test from the steps, commit the test, then fix (`sdlc-feedback-loop`). Cite the issue in the commit and PR.
- **Task issue**: skip intent and spec. Plan only if the work is non-trivial (`sdlc-plan`), implement, and prove each line of the definition of done.
- A request that arrives in the wrong form (a bug filed as a feature) gets relabeled and its missing fields requested, not worked around.
- Findings from monitoring go in through the bug form when something is broken, and through the feature form when the finding is a proposed improvement (`sdlc-close-the-loop`).

## Pull request template

The PR template asks for Summary, Testing, Risk and How to revert. The assistant can draft all four; the person who asked for the change signs off (`sdlc-pr-review`, `sdlc-risk-tiers`).

## Measure

- Leading: share of issues that arrive complete enough to start without a follow-up question.
- Lagging: time from issue to first commit; bugs that reach a failing test on the first attempt; issues relabeled after filing (a high number means the forms are unclear).
