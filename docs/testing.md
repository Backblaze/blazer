# Testing blazer

A green test run is only evidence for what it exercised. This file says what each kind of test proves,
how to run it, and how to show that a test pins the behavior a PR claims.

## Test levels

| Level | Where | Run it with | Proves | Does not prove |
| --- | --- | --- | --- | --- |
| Unit, fake backend | `b2/*_test.go` using the fake in `b2/b2_test.go` | `go test ./...` | Logic inside the `b2` package | What is sent on the wire |
| Wire | `httptest` server or `RoundTripper` stub through `NewClient` (`*_wire_test.go`) | `go test ./...` | The exact request body, headers and query, and how a response is decoded | That B2 answers that way |
| Repo-owned SDK harness contract | `.sdkharness/` ([README](../.sdkharness/README.md)) | See below | Behavior against the B2 simulator ([`backblaze-labs/b2-simulator`](https://github.com/backblaze-labs/b2-simulator)), one scenario per capability | Behavior the simulator does not model (encryption keys, retention, replication effects) |
| Live integration | `b2/integration_test.go`, `base/integration_test.go`, `b2/lock_test.go` | `B2_APPLICATION_KEY_ID=... B2_APPLICATION_KEY=... go test ./b2/ ./base/` | Real B2 behavior for that account on that day | Other accounts, regions or later B2 releases |

`make ci` runs build, vet, gofmt, tidy, verify, lint, the race tests, b2keys, cross-compiles and the
changelog check. Cross-compiling proves the code builds for a platform, not that it runs there; native
Windows and macOS runs happen in CI.

## Showing a test pins the fix

For every behavior a PR adds or fixes:

1. Write the test first and watch it fail on the old code.
2. After the fix, undo the one line that makes it work (or remove the guard) and run the test again. It
   must fail. Paste that failure in the PR.
3. If undoing the fix still passes, the test is not pinning it. Common causes: the fake returns the
   expected value no matter what was sent, the assertion accepts any error (assert the specific one),
   or the scenario never reaches the changed path.

Tests must also fail for the right reason. An unrelated error, such as an aborted request or a test
server that returns an empty `200`, must not satisfy an assertion meant for a specific failure.

## Repo-owned SDK harness contract

`.sdkharness/tests.tsv` lists one row per scenario: level (`conformance`, `resilience`, `health`), scenario
id, target and the executable that runs it. Each scenario runs against a fresh simulator and prints one
`SDKHARNESS_RESULT` line. The central SDK harness runs every row daily against a pinned commit of
[`backblaze-labs/b2-simulator`](https://github.com/backblaze-labs/b2-simulator).

To run a scenario yourself, start that simulator on loopback (see the [`.sdkharness` README](../.sdkharness/README.md))
and run, for example:

```sh
SDKHARNESS_TEST_LEVEL=conformance SDKHARNESS_SCENARIO=enc.sse_c \
SDKHARNESS_SIMULATOR_URL=http://127.0.0.1:<port> \
  ./.sdkharness/tests/run-conformance
```

The health scenario also needs `B2_APPLICATION_KEY_ID`, `B2_APPLICATION_KEY` and `B2_BUCKET_NAME` set to the
simulator's test credentials and an existing bucket. Resilience scenarios need the simulator's fault
control URL in `SDKHARNESS_SIMULATOR_CONTROL_URL`.

When you add or remove a row, update the counts in `b2/sdkharness_contract_test.go` in the same change. Two
PRs that each add a row will conflict on those counts; the second to merge sets them to the merged total.

GitHub CI does not run the simulator contract, so a change can be green in CI and still fail a scenario.
Run the scenarios for the area you changed before asking for review.

## Live integration tests

`b2/integration_test.go` skips unless `B2_APPLICATION_KEY_ID` and `B2_APPLICATION_KEY` are set. Use a
throwaway account or key, never one with production data. The tests create and delete their own buckets;
clean up leftovers with `go run ./internal/bin/cleanup`. Some tests have a one-minute deadline and can
time out when the network is busy; re-run before treating that as a failure.

In CI, `.github/workflows/test-live.yml` runs them on every push to `main` (each merged PR), weekly, and
on demand. Pull requests do not trigger them, so the live key is not automatically exposed to PR code.
A manual `workflow_dispatch` run can still target any branch with the key, so anyone with write access
can run unmerged code against it; restricting the `live-tests` environment to `main` would close that.
The job fails if any test fails or skips, or if any top-level test in `b2/integration_test.go`,
`b2/lock_test.go` or `base/integration_test.go` does not pass, so a green run means those tests ran
against B2. A failure on `main` opens (or comments on) the issue "Live integration tests are failing on
main". Runs are serialized because the tests share bucket names, so rapid merges can share one run; its
summary lists every commit merged since the last green live run.

## When a PR needs a live test

Wire tests prove what blazer sends; only a live test proves how B2 answers. Add or extend a live test when
the PR:

- adds a B2 call, or changes the request body, headers or query of an existing one;
- depends on B2 behavior: encryption, Object Lock and retention, replication, large files and copies,
  file info, error codes, retries or reauthorization;
- fixes a bug caused by B2 behaving differently from its docs.

Pure client logic (option validation, redaction, concurrency inside blazer), docs and CI changes need none.
When a live test is needed but cannot be written (the test key lacks a capability, or the feature needs a
second account), say so in the PR with what you verified instead; that check is NOT RUN, not PASS.

Writing one:

- Put it in `b2/integration_test.go` (use `startLiveTest`, which skips without credentials, creates a
  uniquely named bucket and returns a cleanup function) or `base/integration_test.go`. The live workflow
  requires every top-level test in those files and `b2/lock_test.go` to pass, so a live test anywhere else
  would not be checked.
- Keep it short and give it a deadline; the live suite runs serially on every merge.
- Clean up everything it creates, on a fresh context, even when it fails.
- Pull requests do not run live tests; a new test first runs after merge, unless a maintainer starts
  `test-live` on the branch by hand.

## Reporting what you ran

In the PR, list each level you ran and its result. A check that did not run is **NOT RUN**, never PASS,
and an optional tool that is missing is not a passing check. Say what you could not verify (for example,
no live B2 access) so a reviewer knows what is still open.
