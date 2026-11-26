---
type: index
title: "Blazer contributor docs"
description: "Read AGENTS.md first: commands, architecture, conventions and the short list of mistakes reviewers keep finding."
status: active
owner: ["@goanpeca", "@sophiecarreras", "@ffumero2003", "@david-santacruz-backblaze"]
stale_after: 2027-05-26T00:00:00+00:00
---
# Blazer contributor docs

Read [AGENTS.md](../AGENTS.md) first: commands, architecture, conventions and the short list of mistakes
reviewers keep finding. The files here hold the detail behind those rules.

| File | Read it when |
| --- | --- |
| [design-docs/index.md](design-docs/index.md) | You need recorded design decisions and threat models. |
| [design-docs/threat-model-credentials.md](design-docs/threat-model-credentials.md) | You review credential flows and controls. |
| [exec-plans/tech-debt.md](exec-plans/tech-debt.md) | You review known defects. |
| [writing-style.md](writing-style.md) | You write or edit documentation or godoc. |
| [b2-contracts.md](b2-contracts.md) | You touch a B2 call, encryption, file info, bucket settings, downloads, retries or large files. The file lists B2 and reference SDK behavior. It gives sources and tests that pin each fact. |
| [testing.md](testing.md) | You write or run tests. The guide explains test levels and what each level proves. It shows how to run the repo-owned SDK harness contract and live tests. It shows how to prove a test pins a fix. |
| [testing-standard.md](testing-standard.md) | You write or review tests. The standard sets the TDD loop and five kinds of test. It states what evidence a PR carries and who reviews what. |
| [delete-options.md](delete-options.md) | You use governance-bypass deletion or migrate pre-v0.9.0 delete interfaces and function values. |
| [object-lock.md](object-lock.md) | You use per-file retention or legal hold, resume a locked upload, or adapt a low-level upload interface to the Object Lock signatures. |
| [review-lessons.md](review-lessons.md) | You review a PR or prepare one for review. The file groups problems from earlier reviews. It links to the PRs that raised them. |
| [upgrading.md](upgrading.md) | You upgrade across a release and your build or a call starts failing. The guide lists only what callers must change for each release. It includes before/after code. |

## Additional documents

| File | Description |
| --- | --- |
| [exec-plans/completed/b2-mapping-edges/plan.md](exec-plans/completed/b2-mapping-edges/plan.md) | Plan: cover the remaining b2 request-mapping edges (from intent.md and spec.md). |
| [exec-plans/completed/b2wiretest/plan.md](exec-plans/completed/b2wiretest/plan.md) | Plan: a shared recording B2 server for wire tests (from intent.md and spec.md). |
| [exec-plans/completed/base-mapping-edges/plan.md](exec-plans/completed/base-mapping-edges/plan.md) | Plan: cover the remaining base request and response mapping edges (from intent.md and spec.md). |
| [exec-plans/completed/consistent-tests-run/plan.md](exec-plans/completed/consistent-tests-run/plan.md) | Plan: run the x/consistent tests by default (from intent.md and spec.md). |
| [exec-plans/completed/create-key-wire/plan.md](exec-plans/completed/create-key-wire/plan.md) | Plan: pin the b2_create_key request, response and errors (from intent.md and spec.md). |
| [exec-plans/completed/credential-free-live-calls/plan.md](exec-plans/completed/credential-free-live-calls/plan.md) | Plan: credential-free tests for live-only calls (from intent.md and spec.md). |
| [exec-plans/completed/independent-expected-values/plan.md](exec-plans/completed/independent-expected-values/plan.md) | Plan: replace self-referential expected values in five tests (from intent.md and spec.md). |
| [exec-plans/completed/key-restrictions/plan.md](exec-plans/completed/key-restrictions/plan.md) | Plan: expose a key's bucket restriction and name prefix (from intent.md and spec.md). |
| [exec-plans/completed/listing-prefix-cursor/plan.md](exec-plans/completed/listing-prefix-cursor/plan.md) | Plan: assert the listing prefix and cursor on every page, in b2 and sync (from intent.md and spec.md). |
| [exec-plans/completed/notification-rules-secret/plan.md](exec-plans/completed/notification-rules-secret/plan.md) | Plan: round-trip the notification rule signing secret and suspension fields (from intent.md and spec.md). |
| [exec-plans/completed/sha1-upload-and-verify/plan.md](exec-plans/completed/sha1-upload-and-verify/plan.md) | Plan: pin the SHA1 on upload parts and make Reader.Verify catch a bad hash (from intent.md and spec.md). |
| [exec-plans/completed/unbound-incremental-conformance/plan.md](exec-plans/completed/unbound-incremental-conformance/plan.md) | Plan: a conformance check for unbound uploads (from intent.md and spec.md). |
| [exec-plans/completed/upload-content-type/plan.md](exec-plans/completed/upload-content-type/plan.md) | Plan: assert the Content-Type sent on every upload (from intent.md and spec.md). |
| [exec-plans/completed/writer-object-lock/plan.md](exec-plans/completed/writer-object-lock/plan.md) | Plan: prove the Writer sends retention and legal hold on every upload path (from intent.md and spec.md). |
| [guides/custom-transport.md](guides/custom-transport.md) | Custom transport. |
| [guides/debugging.md](guides/debugging.md) | Debugging. |
| [guides/errors-and-retries.md](guides/errors-and-retries.md) | Errors and retries. |
| [guides/keys-and-sessions.md](guides/keys-and-sessions.md) | Keys and sessions. |
| [guides/large-and-resumable-uploads.md](guides/large-and-resumable-uploads.md) | Large and resumable uploads. |
| [product-specs/README.md](product-specs/README.md) | Change intents. |
| [product-specs/b2-mapping-edges/intent.md](product-specs/b2-mapping-edges/intent.md) | Intent: cover the remaining b2 request-mapping edges found by mutation. |
| [product-specs/b2-mapping-edges/spec.md](product-specs/b2-mapping-edges/spec.md) | Spec: cover the remaining b2 request-mapping edges found by mutation. |
| [product-specs/b2wiretest/intent.md](product-specs/b2wiretest/intent.md) | Intent: a shared recording B2 server for wire tests. |
| [product-specs/b2wiretest/spec.md](product-specs/b2wiretest/spec.md) | Spec: a shared recording B2 server for wire tests. |
| [product-specs/base-mapping-edges/intent.md](product-specs/base-mapping-edges/intent.md) | Intent: cover the remaining base request and response mapping edges. |
| [product-specs/base-mapping-edges/spec.md](product-specs/base-mapping-edges/spec.md) | Spec: cover the remaining base request and response mapping edges. |
| [product-specs/consistent-tests-run/intent.md](product-specs/consistent-tests-run/intent.md) | Intent: run the x/consistent tests by default. |
| [product-specs/consistent-tests-run/spec.md](product-specs/consistent-tests-run/spec.md) | Spec: run the x/consistent tests by default. |
| [product-specs/create-key-wire/intent.md](product-specs/create-key-wire/intent.md) | Intent: pin the b2_create_key request, response and errors. |
| [product-specs/create-key-wire/spec.md](product-specs/create-key-wire/spec.md) | Spec: pin the b2_create_key request, response and errors. |
| [product-specs/credential-free-live-calls/intent.md](product-specs/credential-free-live-calls/intent.md) | Intent: credential-free tests for calls only covered by live tests. |
| [product-specs/credential-free-live-calls/spec.md](product-specs/credential-free-live-calls/spec.md) | Spec: credential-free tests for calls only covered by live tests. |
| [product-specs/independent-expected-values/intent.md](product-specs/independent-expected-values/intent.md) | Intent: replace self-referential expected values in five tests. |
| [product-specs/independent-expected-values/spec.md](product-specs/independent-expected-values/spec.md) | Spec: replace self-referential expected values in five tests. |
| [product-specs/key-restrictions/intent.md](product-specs/key-restrictions/intent.md) | Intent: expose a key's bucket restriction and name prefix. |
| [product-specs/key-restrictions/spec.md](product-specs/key-restrictions/spec.md) | Spec: expose a key's bucket restriction and name prefix. |
| [product-specs/keys/intent.md](product-specs/keys/intent.md) | Intent: Application key management contract. |
| [product-specs/keys/spec.md](product-specs/keys/spec.md) | Spec: Application key management contract. |
| [product-specs/listing/intent.md](product-specs/listing/intent.md) | Intent: Object listing iterator contract. |
| [product-specs/listing/spec.md](product-specs/listing/spec.md) | Spec: Object listing iterator contract. |
| [product-specs/listing-prefix-cursor/intent.md](product-specs/listing-prefix-cursor/intent.md) | Intent: assert the listing prefix and cursor on every page, in b2 and sync. |
| [product-specs/listing-prefix-cursor/spec.md](product-specs/listing-prefix-cursor/spec.md) | Spec: assert the listing prefix and cursor on every page, in b2 and sync. |
| [product-specs/notification-rules-secret/intent.md](product-specs/notification-rules-secret/intent.md) | Intent: round-trip the notification rule signing secret and suspension fields. |
| [product-specs/notification-rules-secret/spec.md](product-specs/notification-rules-secret/spec.md) | Spec: round-trip the notification rule signing secret and suspension fields. |
| [product-specs/reader/intent.md](product-specs/reader/intent.md) | Intent: Reader download contract. |
| [product-specs/reader/spec.md](product-specs/reader/spec.md) | Spec: Reader download contract. |
| [product-specs/review-process/intent.md](product-specs/review-process/intent.md) | Intent: review and merge process scaled by risk. |
| [product-specs/sha1-upload-and-verify/intent.md](product-specs/sha1-upload-and-verify/intent.md) | Intent: pin the SHA1 on upload parts and make Reader.Verify catch a bad hash. |
| [product-specs/sha1-upload-and-verify/spec.md](product-specs/sha1-upload-and-verify/spec.md) | Spec: pin the SHA1 on upload parts and make Reader.Verify catch a bad hash. |
| [product-specs/sync/intent.md](product-specs/sync/intent.md) | Intent: Sync product contract. |
| [product-specs/sync/spec.md](product-specs/sync/spec.md) | Spec: Sync product contract. |
| [product-specs/unbound-incremental-conformance/intent.md](product-specs/unbound-incremental-conformance/intent.md) | Intent: a conformance check for unbound (unknown-length) uploads. |
| [product-specs/unbound-incremental-conformance/spec.md](product-specs/unbound-incremental-conformance/spec.md) | Spec: a conformance check for unbound (unknown-length) uploads. |
| [product-specs/upload-content-type/intent.md](product-specs/upload-content-type/intent.md) | Intent: assert the Content-Type sent on every upload. |
| [product-specs/upload-content-type/spec.md](product-specs/upload-content-type/spec.md) | Spec: assert the Content-Type sent on every upload. |
| [product-specs/writer/intent.md](product-specs/writer/intent.md) | Intent: Writer upload contract. |
| [product-specs/writer/spec.md](product-specs/writer/spec.md) | Spec: Writer upload contract. |
| [product-specs/writer-object-lock/intent.md](product-specs/writer-object-lock/intent.md) | Intent: prove the Writer sends retention and legal hold on every upload path. |
| [product-specs/writer-object-lock/spec.md](product-specs/writer-object-lock/spec.md) | Spec: prove the Writer sends retention and legal hold on every upload path. |
| [verification/get-file-info-by-name.md](verification/get-file-info-by-name.md) | GetFileInfoByName verification. |

Other process docs live at the repository root: [REVIEW.md](../REVIEW.md), [CONTRIBUTING.md](../CONTRIBUTING.md),
[RELEASE.md](../RELEASE.md) and [SECURITY.md](../SECURITY.md).

When a review teaches something that will matter again, add it here in the same change: a B2 fact goes in
`b2-contracts.md` with its source, a review pattern in `review-lessons.md` with the PR link, and a rule
that must always hold in `AGENTS.md` (only when it has come up on at least two PRs).
