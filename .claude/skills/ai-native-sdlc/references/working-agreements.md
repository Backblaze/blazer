# Working agreements for agent-written code

A template for the team habits that make light, fast review safe. It turns the culture section of the [Claude Code review lessons](code-review-at-anthropic.md#8-culture-and-direction) into statements a team can adopt, edit and keep in its repo. Replace the bracketed parts, delete what you disagree with, and add your own. Keep it to a page.

Used with: [`sdlc-risk-tiers`](../../sdlc-risk-tiers/SKILL.md), [`sdlc-pr-review`](../../sdlc-pr-review/SKILL.md), [`sdlc-close-the-loop`](../../sdlc-close-the-loop/SKILL.md).

---

# Working agreements: [team or repo]

Agreed on [date] by [names or roles]. Review again on [date].

## Ownership

- **You land it, you own it.** Whoever asked for a change owns what it does in production, whether a person or an agent wrote it.
- The person who prompts an agent is accountable for the result and is a required reviewer of the PR.
- An agent's approval never counts as a human approval.
- Every area has named owners in `CODEOWNERS`. Owners write that area's `REVIEW.md` and decide what is high risk there.

## Size and reversibility

- Prefer small changes. They are easier to review, debug and revert.
- **Test first.** A behavior change starts with a test that fails without it; refactors are separate and leave tests alone ([how](../../sdlc-feedback-loop/references/test-driven-development.md)).
- Every change says how to revert it, in the PR template.
- New behavior ships behind a flag when it can, on for the team first.
- A mistake we can undo in minutes is cheaper than a slow process we run on every change.

## Blast radius sets the bar

- The tier comes from what the change touches, not from who wrote it (`risk-tiers.json`).
- Low risk (docs, copy, internal-only, additive behind a flag): automated review and security checks, no human required.
- Medium: one person. High: two people.
- Always a person, whatever the tier: [auth, billing, CI workflows, installers, migrations, infra].
- We start strict and loosen only with evidence from the revert and incident rates.

## What a reviewer looks at

- Humans spend time on: whether we should build it at all, risky code, architecture others will copy, and the evidence (test output, commands run, screenshots, reviewer findings).
- If you cannot say what the checks would miss, ask for a real review.
- Style, naming and anything CI already enforces are not human review topics.
- Each review finding is fixed, or closed with a reason. We do not merge with open threads.

## Changing direction

- Agents make starting work cheap, so some work should not land. Closing a PR is normal, not a loss.
- Raise a different direction early, in the channel or the issue, not as a blocking comment on a finished PR.
- The author decides whether to close or reshape, with the owner.
- After three review rounds on a PR, stop line-fixing and reconsider the approach together.

## When something goes wrong

- Review the system, not the person. The question is which layer should have caught it: verification before the PR, AI review, security review, automated checks, human approval, merge queue and flags, detection and rollback.
- Every incident review ends with at least one encoded change: a rule, check, tier, skill or eval, with an owner and a date (`incident-review.md` template in `sdlc-close-the-loop`).
- If revert or incident rates rise, we tighten tiers first, then ask why.
- Rollback is a rehearsed single step, and anyone on call may use it without asking.

## Staying informed

- Work in progress happens in open channels, so anyone can see it, pick up a thread, or spot a gap.
- Before changing a module you do not know, ask for an explainer (`sdlc-explain-module`) and read the tests.
- We do not rely on reading every diff to learn the code. We read the module we are about to change.

## Measuring

We track, per month: human approvals per PR (median and p95), time a PR waits on a person, revert rate, and incident rate. Human time is only a win if the revert and incident rates stay flat or fall.
