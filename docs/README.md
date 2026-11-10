# Blazer contributor docs

Read [AGENTS.md](../AGENTS.md) first: commands, architecture, conventions and the short list of mistakes
reviewers keep finding. The files here hold the detail behind those rules.

| File | Read it when |
| --- | --- |
| [b2-contracts.md](b2-contracts.md) | You touch a B2 call, encryption, file info, bucket settings, downloads, retries or large files. It lists what B2 and the reference SDKs do, with sources and the tests that pin each fact. |
| [testing.md](testing.md) | You write or run tests. It explains the test levels, what each one proves, how to run the repo-owned SDK harness contract and the live tests, and how to show a test pins a fix. |
| [delete-options.md](delete-options.md) | You use governance-bypass deletion or migrate pre-v0.9.0 delete interfaces and function values. |
| [object-lock.md](object-lock.md) | You use per-file retention or legal hold, resume a locked upload, or adapt a low-level upload interface to the Object Lock signatures. |
| [review-lessons.md](review-lessons.md) | You review a PR or prepare one for review. It groups the problems earlier reviews found, with links to the PRs where they were raised. |
| [upgrading.md](upgrading.md) | You upgrade across a release and your build or a call starts failing. It lists, per release, only what callers must change, with before/after code. |

Other process docs live at the repository root: [REVIEW.md](../REVIEW.md), [CONTRIBUTING.md](../CONTRIBUTING.md),
[RELEASE.md](../RELEASE.md) and [SECURITY.md](../SECURITY.md).

When a review teaches something that will matter again, add it here in the same change: a B2 fact goes in
`b2-contracts.md` with its source, a review pattern in `review-lessons.md` with the PR link, and a rule
that must always hold in `AGENTS.md` (only when it has come up on at least two PRs).
