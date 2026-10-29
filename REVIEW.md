# Review instructions

For human reviewers and review agents. Approval always comes from a code owner in
`.github/CODEOWNERS`; an agent's findings never approve or block on their own, and the agent that wrote
a change never reviews it.

## What failure looks like here

Blazer fails by sending the wrong request to B2 (a dropped, extra or mis-shaped field), by leaving
resources behind (unfinished large files, keys, replication config), by hanging (a lock or a wait that
never ends), or by leaking a credential into a log or error. A test that passes either way is also a
failure: it claims coverage it does not give.

## Before the passes

1. Read the linked issue and, if present, `intent/<slug>/spec.md` and `plan.md`.
2. Merge the PR onto current `main` locally and run `make ci`. Say what you ran.
3. For each behavior the PR claims to fix or add, revert that line and confirm a test fails. Name any
   behavior no test pins.

## Passes

Tag each finding with its pass.

- **Bugs**: logic errors, off-by-one thresholds, nil derefs, error paths that leak resources, lost
  wakeups, locks held across callbacks or channel sends.
- **Wire**: the request body matches the B2 docs for the API version used; omitted vs `null` vs `[]` is
  deliberate; `b2_update_bucket` sends only fields the caller changed; retry, reauth and `Retry-After`
  behave like sibling calls; gzip `Content-Encoding` downloads still report length and SHA1.
- **Security**: secrets (application keys, auth tokens, SSE-C keys) never logged or put in errors;
  capability checks fail closed; integer conversions are bounded (CodeQL).
- **Compatibility**: exported signatures in every package, not only `b2` (a new variadic parameter breaks
  function values; a new `base` parameter breaks callers); returned shapes (`nil` vs empty, map keys);
  changed behavior of an existing field or default. Each needs a `**Breaking:**` changelog entry that
  says what callers must do. Compare with `b2-sdk-python` and the `b2` CLI ([docs/b2-contracts.md](docs/b2-contracts.md)).
- **Compliance**: the change matches the issue, `spec.md` and `plan.md`; the PR description matches the
  code; a `.changes/unreleased` entry exists and reads well for users; godoc says what is not supported;
  `AGENTS.md` is updated when the change makes it stale or breaks a convention in it.

## Severity

- **Important**: breaks existing callers, sends a wrong request to B2, loses or leaks data or
  credentials, hangs, or leaves resources behind. A claimed behavior with no test pinning it is Important.
- **Nit**: style, naming, comments. At most five per review; summarize the rest as a count.
- **Pre-existing**: a real bug this PR did not introduce. Report it so it is not lost, but it does not
  block this PR; suggest an issue.

## Evidence

Every finding cites `file:line` and says what goes wrong, with the input or sequence that triggers it.
Do not infer behavior from a function's name; read it. If you could not verify something (no live B2, no
simulator), say so.

## Converging

- Round 1 reviews the whole diff. From round 3 on, review only the commits since your last review.
- Name the class of bug, not one instance, so the author fixes every occurrence in one round.
- Never repeat a finding already posted on the PR, resolved or not.
- After three rounds with Important findings still open, stop and ask the author to reconsider the
  approach instead of posting more line comments.

## Answering findings

Every finding gets an answer before merge: fix it, or resolve the thread with a one-line reason
(false positive, intentional, out of scope with an issue link). Do not resolve a thread silently.

## Format

Start with a count (`2 Important, 1 Nit, 0 Pre-existing`, or `No issues found`), then what you checked
and against what (issue number, live B2 or simulator, commands run). Then a table sorted by severity:
`Severity | File:Line | Pass: finding`. Do not report generated files or what CI already enforces
(gofmt, vet, lint).

## Reviewing tests

Applies to every added or changed test, using the passes, severities and format above. The standard is
[docs/testing-standard.md](docs/testing-standard.md). For each test, answer with `file:line`:

1. **Claim**: what does the name promise?
2. **Assertion**: does it assert exactly that? Weaker than the name is `weak`.
3. **Fails when broken**: break or revert the production line it names; run the test alone and quote the
   output. If you could not run it, label the finding "unverified". Never write "catches bugs" without the
   break and the evidence level (L1 read, L2 reached by coverage, L3 one break once, L4 three runs alone,
   L5 historical bug).
4. **Vacuous**: can the assertion never fail (a loop that may run zero times, an expected value computed by
   the code under test, a nil-only check, a skipped body, an unreachable branch)?
5. **Flaky**: sleeps, real clock, network, ordering, shared state, unseeded randomness.
6. **Coverage only**: does it add covered lines but check no claim?
7. **Kind**: is it the right kind (unit, integration, contract, system, security)? Does a contract claim
   have its row in `.sdkharness/tests.tsv`?
8. **Removed, skipped, loosened or retired** tests are Important unless a human approval is linked.
9. **Missing**: for each behavior the change adds or fixes, name the test that pins it. None is Important.
10. **Secrets**: no real credential in any test, fixture or log; a change to auth, logging, errors, URLs,
    credentials or parsing has a security test.

Important: a claimed behavior with no test that fails when it is broken; a vacuous or weak test on a risky
path; a removed or loosened test; a real secret. Naming and style are Nits and follow the cap above.

When a bug ships that a test should have caught, add that test and record what the review missed in
[docs/review-lessons.md](docs/review-lessons.md).
