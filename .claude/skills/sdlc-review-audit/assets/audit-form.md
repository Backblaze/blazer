# Repo audit form: code review setup

Answer each item Yes, No or N/A and cite the file or setting you checked. Run `audit.sh` first: the "Script row" column names the rows that answer each item. An item is Yes only when every row listed for it is PASS and the question is fully met; a WARN, MISSING or UNKNOWN in any of them means No, or check by hand before answering. A PASS is evidence, not the answer: read the question and confirm what the row cannot see. Items marked settings need GitHub admin read access, and items marked ask need the owner; do not answer those from repo files alone.

Adapted from Edu's repo audit checklist.

| # | Question | Script row, or how to answer |
|---|---|---|
| **A. Before the pull request** | | |
| 1 | Is there a short root agent instruction file (any of `AGENTS.md`, `CLAUDE.md`, `GEMINI.md` or `.github/copilot-instructions.md`) stating the stack, conventions, goals and non-goals? | `agent instruction file`, `instruction file states goals and non-goals`. The stack and conventions are not checked: read the file. |
| 2 | Does it give runnable commands for tests, build and lint, so an agent can verify its own work? | `instruction file has test, build and lint commands` |
| 3 | Is it short and pointing to other docs rather than duplicating them? | `agent instruction file` (flags it over 120 lines), `instruction file points to other docs` |
| 4 | Are formatters or linters wired to run automatically (agent hooks, pre-commit or equivalent)? | `formatters or linters run automatically` |
| 5 | Is there a `REVIEW.md` (or equivalent) saying what failure looks like in this repo? | `REVIEW.md` |
| 6 | Does it cap nits, list paths to skip, and require file and line evidence for every finding? | `REVIEW.md caps nits`, `REVIEW.md lists paths to skip`, `REVIEW.md requires file and line evidence` |
| **B. On every pull request** | | |
| 7 | Does CI run lint, type checks, tests and a build on every PR? | `CI runs lint, types, tests and build` |
| 8 | Are those checks required before merge? (settings) | `branch protection: required status checks`. PASS means at least one required check matches a PR job; confirm the lint, type, test and build jobs are each in the required list it prints. |
| 9 | Does CI cover every platform the project ships on, if more than one? N/A for one platform. | `CI covers every platform you ship` |
| 10 | Does the PR template have Testing, Risk and How-to-revert fields? | `PR template` |
| 11 | Is an automated AI code review configured to run on each PR? | `AI review wired in CI`. Anthropic's managed Code Review and Copilot code review are settings the script cannot see: check them by hand. |
| 12 | If so, is its depth or strictness set deliberately, stricter for higher-cost repos? | `AI review depth set deliberately` |
| 13 | Is an automated security review configured (the security review action, a `/security-review` step or equivalent)? | `automated security pass in CI` |
| 14 | Can open security findings block the merge? (settings) | `open security findings block the merge` |
| 15 | Must all review threads be resolved before merge? (settings) | `branch protection: threads must be resolved` |
| 16 | Is there a documented cap on review rounds (three is a good default), after which the author is asked to reconsider the approach? | `review round cap documented` |
| **C. Approval scaled by risk** | | |
| 17 | Are risk tiers written down, based on the change and not the author? | `risk tiers config` |
| 18 | Does a `CODEOWNERS` file cover sensitive paths (auth, billing, installers, CI and release workflows, secrets)? | `CODEOWNERS covers sensitive paths`, `CODEOWNERS covers agent configuration` |
| 19 | Do CI workflow files require an owner's review? | `CODEOWNERS covers CI workflows` |
| 20 | Does branch protection require at least one approving review on the default branch? (settings) | `branch protection: at least one approving review`. A repo with a no-human risk tier sets the count to 0 on purpose; answer Yes if a required check enforces the classifier's `humans_required` instead. |
| 21 | Is "Require review from Code Owners" on? (settings) | `branch protection: code owner review`, `CODEOWNERS names only people and teams` |
| 22 | Are approvals dismissed when new commits are pushed? (settings) | `branch protection: dismiss stale approvals` |
| 23 | Is any lighter review path limited to an explicit trusted-author list? N/A if there is no lighter path. | `lighter path limited to trusted authors` |
| 24 | Is it true that a bot or agent approval cannot satisfy a required human review? (settings, plus a look at any auto-approve workflows) | `no workflow approves pull requests`, `GitHub Actions cannot approve pull requests`, `branch protection: no bypass of PR reviews`, `only people can supply required approvals`. These rows passing is not enough: answer Yes only after checking that no GitHub App installed on the repo has write access. |
| **D. Merge and after** | | |
| 25 | Is a merge queue enabled that re-runs required checks against the latest default branch? (settings) | `merge queue` |
| 26 | Does new behavior ship behind feature flags? | `feature flags in use` |
| 27 | Is there a staged release path (canary then full, or latest then stable)? | `staged release path` |
| 28 | Is a one-step rollback or revert path documented or scripted? | `rollback path documented` |
| 29 | Can flags be turned off without a deploy? | `flags can be turned off without a deploy` |
| 30 | Is there a documented on-call or incident-response owner and process? | `on-call or incident process documented` |
| 31 | Do past incidents or reverts feed back into something (regression tests, eval set, `REVIEW.md` updates)? Look in the repo and recent revert PRs. | `reverts feed back into evals or incident reviews` |
