# Lessons from how the Claude Code team does code review

Source: ["Solving code review at Anthropic"](https://claude.dev/blog/solving-code-review-at-anthropic/), a write-up of how the Claude Code team reviews code when Claude writes most of it. The practices apply to any AI assistant; [AI assistants](ai-assistants.md) maps the Claude-specific pieces to Codex, Copilot and Gemini CLI.

**Sourcing caveat.** I read the article through a page-fetch tool that returns a summary, not the raw text, so the numbers and wording below come from that summary. Check any figure you plan to quote against the original. Everything is the team's own report on its own repos, with no controlled comparison.

How this relates to the rest of the folder: it is the most detailed account of the playbook's [PR review lesson](lesson-notes/10-ai-in-the-pr-review-loop.md) in practice, and it is the best evidence for the middle of the [gate dial](playbook-harness-translation.md#4-the-one-thing-that-doesnt-translate-mechanically-the-gates). The skills that apply it are listed at the end.

## 1. Premise and results

- Claude writes about 90% of Claude Code's own code, so review had to change.
- June to September: merged PRs more than doubled and the revert rate fell by more than 40%.
- Anthropic's monorepo grew from about 2,500 changes a day in May to over 30,000 in September.
- Review was not removed. Most of the reading moved to machines. The stated goal is **the minimum human review time that does not raise reverts or incidents**.

## 2. Reframing review

- The question changed from "did a human read this?" to **"what could go wrong, and which check, automated or human, should catch it?"**
- Review does two jobs: it finds and fixes problems before they ship, and it spreads understanding of the codebase. The first moved to machines. The second needs deliberate attention (section 7).
- Review moved earlier and runs continuously, closer to a spellcheck than an end-of-line gate. Most changes are reviewed and fixed by Claude at least once before a person sees them.

## 3. Success metrics

| Kind | Metric |
|---|---|
| Target | Most PRs merge with at most one human in the loop, who may be the author |
| Human time | Human approvals per PR (median and p95), human comments, time a PR waits on a person |
| Guardrail | Revert and incident rates. If they rise, human time was only moved, not saved |

## 4. The layered pipeline

Review is a stack of layers. Each catches some problems and misses others, and rollback covers what gets through.

| Layer | What it does | Playbook skill |
|---|---|---|
| 1. Verification before the PR | Claude runs tests, a build or a screenshot compare and shows output instead of claiming success. A fresh-context subagent reviews the diff. Engineers run skills that hunt bugs, simplify the diff and check end-to-end behavior before opening the PR. | `sdlc-feedback-loop`, `sdlc-subagents` |
| 2. AI review on every PR | A reviewer reads the diff against the whole repo and the PR's stated intent, with findings checked before they post | `sdlc-pr-review`, `sdlc-review-tuning` |
| 3. Blocking security review | A separate automated pass whose open findings block merge | `sdlc-pr-review` |
| 4. Blocking automated checks | Lint, types, tests on every platform, builds. The PR template asks how the change was tested and how to revert it | `sdlc-cicd` |
| 5. Risk-scaled human approval | Zero, one or two people, depending on the change | `sdlc-risk-tiers` |
| 6. Merge queue, flags, gradual release | Checks re-run against current main; new behavior behind flags, employees first; staged channels | `sdlc-cicd` |
| 7. Fast detection and rollback | Flags off without a deploy, an on-call rotation, incident reviews that ask which layer should have caught it | `sdlc-cicd`, `sdlc-close-the-loop` |

### Layer 1 details

- Boris Cherny's view, as quoted: giving Claude a way to verify its work is probably the biggest factor, with an estimated 2 to 3 times improvement in final results.
- **Fix recurring findings at the source**: in a skill, a lint rule, `CLAUDE.md` or `REVIEW.md`, not by repeating the comment on each PR.
- `CLAUDE.md` holds the stack, conventions, goals and non-goals. Keep it short, with pointers to other docs. The reviewer loads it and flags convention violations and stale lines.
- `REVIEW.md` is written by code owners for their part of the codebase. Effective ones:
  - are short;
  - define what failure looks like in that area (for example, docs "lie", config "misroutes");
  - cap nits and list paths to skip;
  - require file-and-line evidence instead of guesses;
  - require a summary that starts with a count and says plainly when nothing needs fixing.
  - The team also checks separately that the reviewer actually follows the file.

### Layer 2 details

- The reviewer asks whether the change solves its problem, fits the system, or breaks something else. For bug fixes it looks for the root cause.
- When the research preview launched, the share of internal PRs getting substantive review comments went from 16% to 54%. Most PRs over 1,000 lines get findings and most small PRs come back clean.
- Admins set each repo's depth to Quick, Standard or Deep. Deep is for areas where one missed bug costs more than the extra review time.
- **Findings are verified before they post.** Several agents hunt for bugs in parallel. A separate group of verifiers votes on each finding, and it posts only if enough agree it is real. Style-only, diagnostic and cleanup findings are dropped. The filters were A/B tested on internal PRs before adoption.
- Surviving findings appear as inline comments sorted by severity and tagged **Important**, **Nit** or **Pre-existing**. The Pre-existing tag keeps bugs the PR did not introduce from being pinned on its author.
- Every finding gets resolved: fix it, or close the thread with a reason. Agents often apply the fixes overnight.

### Layer 3 details

Open findings from the bug review or the security review block merge. Closing a thread with a short reason clears a false positive, and a later commit that fixes the issue clears the finding automatically.

## 5. Risk-scaled human approval

| Risk | Examples | Human approvals |
|---|---|---|
| Low | Docs, copy, internal-only features, additive changes behind a flag | None, once automated review and security checks are green |
| Medium | Ordinary product changes; sensitive areas such as caching, the auto-updater, system prompts | One person |
| High | The highest-risk code, plus a set of paths that always need two | Two people |

Rules the team applies:

- **The change sets the tier, not the author.** One classifier assigns the tier. A second can require a human even when the tier would not. Paths owners mark as sensitive, and every CI workflow file, always need a person.
- **Only trusted authors get lighter paths**, mostly the owning team. Everyone else needs a second approver.
- **Agent approvals never count as human approvals.** Automated review can clear low-risk changes but cannot fill a human slot.
- **The person prompting an agent is a required reviewer** on agent-written PRs.
- **Approvals are per commit.** Commits the system cannot vouch for, such as an agent push or a hand-resolved merge, need re-approval.
- Start strict and loosen with evidence.

## 6. Improving the reviewer itself

**Incidents become evals.** Past PRs that caused incidents, with their fixes and reverts, were collected into a set. Engineers who knew each area confirmed every bug and where it lived. The reviewer runs against the set daily. Every change to its prompts, tools or model is scored against all cases, and a change that does not improve the score is dropped.

**Measure noise.** Each reviewer comment is classified by the author's response: fixed, marked wrong, marked intentional, or marked as not mattering. The reviewer also runs on known-clean PRs, counting how often it reports something that would lead to a harmful change. The written priority is to catch real bugs first and reduce review rounds second. Noise costs more with agent authors, because each nit triggers another commit and another review round.

**Convergence problems.** Agent authors and agent reviewers can loop forever. One CLI PR went through more than 50 review rounds overnight before it was stopped. Git blame showed about 59% of what the reviewer kept flagging was code present since its first pass, so later rounds mostly found a few more old bugs, not new ones. Multiple rounds had become the default, which is worth it for risky changes and wasteful for routine ones.

Fixes the team applied:

- a round cap: after three rounds, the reviewer asks the author to reconsider the approach;
- diff-only re-review after the first few passes;
- flagging the **bug class**, so every instance gets fixed and not just one line;
- never reposting a finding already raised;
- nightly review agents over existing code, to catch old bugs early.

| Internal PRs | Before | After |
|---|---|---|
| Passed after one review-and-fix round | 65% | 78% |
| Needed five or more rounds | 8.9% | 1.5% |
| Mean reviews to pass | 2.08 | 1.32 |

## 7. Where humans stay in the loop, and sharing context

Humans stay for:

- deciding whether to build something at all (direction, taste, scope);
- reviewing risky code such as billing, the auto-updater and CI workflows;
- reviewing architecture that others will copy;
- reading the evidence: test output, commands run and their results, screenshots, reviewer findings. If you cannot say what the checks would miss, ask for a real review;
- ownership: a person is accountable for every shipped change, whoever wrote it.

On shared understanding: with machines doing most of the reading, nobody holds a full mental model of the codebase. Engineers ask Claude for an explainer on the slice they are about to change, which takes minutes and reflects the current code. Some have Claude build small visual artifacts that teach a module the way that person learns. Reading trivial diffs teaches little. Explaining the module you are about to change teaches much more. Open Slack channels let people see work in progress.

## 8. Culture and direction

- **"You land it, you own it."** Small changes are easier to review, debug and revert.
- Reversibility (flags, reverts, staged releases) undoes most mistakes in minutes.
- Blast radius sets the bar: expensive code gets more people and more checks.
- Incident reviews examine the system, not individuals.
- Agents make starting work cheap, so some work should not land. Raise alternative directions early in a thread, not as blocking comments on a finished PR. Closing a PR does not feel like a loss when the code was quick to write.

## 9. The known gap

The team has no circuit breakers yet that tighten approval automatically when revert or incident numbers move. Engineers want them before more code moves to zero-human merges. The playbook's [control bands](lesson-notes/13-closing-the-loop-on-metrics.md) are a ready-made mechanism: put a band on the revert and incident rate, and let a breach tighten the risk tiers. See `sdlc-risk-tiers` and `sdlc-close-the-loop`.

Detection still relies on the author, the on-call engineer and revert and incident dashboards, and the team says getting faster at noticing and fixing problems beats slowing down.

## 10. Adoption checklist (condensed)

**Before the PR**
- Give the agent runnable checks, listed in a short `CLAUDE.md`, with hooks for formatters and linters.
- Run a bug-hunt and a security review locally, or use a fresh-context subagent.
- Add a `REVIEW.md` and move repeated review comments into it.

**On every PR**
- Require CI. Add Testing, Risk and How to revert to the PR template.
- Add an AI reviewer to one repo, tune it for a few weeks, then expand.
- Add automated security review. Require all review threads resolved before merge.
- Match review depth to risk.
- Cap review rounds, and after the first few re-review only the new diff.

**Approval**
- Define risk tiers by change, not author. Start strict and loosen with evidence.
- Keep lighter paths only for trusted authors.
- Use CODEOWNERS and branch protection so auth, billing, CI, release workflows and installers always need a person.
- A bot's approval never replaces a required human's. Dismiss approvals on new commits.

**Merge and after**
- Merge queue, feature flags, internal-first rollout, staged releases, one-step rollback.
- Turn incidents and reverts into an eval set, and rescore the reviewer whenever its prompts, tools or model change.
- Track human time, with revert and incident rates as the guardrail.

## 11. How this fits the playbook and harness engineering

| Point | This article | Playbook | Harness engineering |
|---|---|---|---|
| Who approves | Depends on risk: none, one or two | Human code-owner on every PR | Optional, agents often self-merge |
| Agent approval counts as human | Never | Never (separation of duties) | Not addressed in the notes |
| `REVIEW.md` | Written by owners per area | Written by the tech lead at the repo root | Review agents, no named file |
| Short `CLAUDE.md` with pointers | Yes | Yes, under a page | Yes (`AGENTS.md` map) |
| Incidents become evals | Yes, and they score the reviewer | Yes, and they test the agent's configuration | Mentions eval harnesses |
| Reducing wait on humans | Tiering, round caps, diff-only re-review | Not detailed | Minimal gates |
| Gradual release and rollback | Flags, internal-first, staged channels, rollback | MCP deploy tiers per environment | Not covered |

The article sits between the other two. It keeps the playbook's human accountability but lets low-risk changes merge without a person, which is the middle setting of the gate dial. It also goes further than either on running the reviewer as a measured system.

## 12. What this folder now has from it

- A 31-item Yes/No/N/A repo audit form from a teammate's checklist (`sdlc-review-audit/assets/audit-form.md`), with each item mapped to an `audit.sh` row. It also widened the audit: CLAUDE.md content, REVIEW.md content, round cap, CI content, auto-approve workflows, bypass allowances, CODEOWNERS sensitive paths, trusted authors, merge queue, rollback, on-call and revert feedback.
- Working agreements template: [working-agreements.md](working-agreements.md), the culture section as statements a team can adopt.

- Reference: this file.
- New skills: `sdlc-risk-tiers` (approval tiers, CODEOWNERS, per-commit approvals; ships a path-based classifier, a sample tier config and `CODEOWNERS`, and a review metrics script), `sdlc-review-tuning` (reviewer evals, noise, convergence; ships a scorer and a nightly review workflow), `sdlc-review-audit` (a read-only repo audit against the checklist) and `sdlc-explain-module` (on-demand explainers of the module you are about to change).
- New templates: an incident review template with the seven-layer table (in `sdlc-close-the-loop`) and issue and PR templates (in `sdlc-issue-templates`).
- Updated skills: `sdlc-pr-review` (verified findings, severity tags, round cap, security pass, PR template), `sdlc-evals` (evals for the reviewer), `sdlc-feedback-loop` (pre-PR review), `sdlc-cicd` (merge queue, flags, staged release, rollback), `sdlc-agent-instructions` (goals and non-goals), `sdlc-hook-gate` (approval rules), `sdlc-close-the-loop` (circuit breakers), and the router `ai-native-sdlc`.
