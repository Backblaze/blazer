# Testing blazer

A green test run is only evidence for what it exercised. This file says what each kind of test proves,
how to run it, and how to show that a test pins the behavior a PR claims.

## Test levels

| Level | Where | Run it with | Proves | Does not prove |
| --- | --- | --- | --- | --- |
| Unit, fake backend | `b2/*_test.go` using the fake in `b2/b2_test.go` | `go test ./...` | Logic inside the `b2` package | What is sent on the wire |
| Wire | New tests use `internal/b2wiretest`; existing per-feature fakes stay until rewritten (`*_wire_test.go`) | `go test ./...` | The exact request body, headers and query, and how a response is decoded | That B2 answers that way |
| Repo-owned SDK harness contract | `.sdkharness/` ([README](../.sdkharness/README.md)) | See below | Behavior against the B2 simulator ([`backblaze-labs/b2-simulator`](https://github.com/backblaze-labs/b2-simulator)), one scenario per capability | Behavior the simulator does not model (encryption keys, retention, replication effects) |
| Live integration | `b2/integration_test.go`, `base/integration_test.go`, `b2/lock_test.go` | `B2_APPLICATION_KEY_ID=... B2_APPLICATION_KEY=... go test ./b2/ ./base/` | Real B2 behavior for that account on that day | Other accounts, regions or later B2 releases |

Object Lock behavior in `b2/lock.go` is live-only for service semantics. The existing
`TestObjectLockLive` in `b2/lock_test.go` exercises retention and legal hold uploads and updates
against B2; it skips without credentials. Local validation and wire tests cannot prove B2 enforces
those settings.

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

For the shared recording-server change (#205), the simulator contract is **NOT RUN**. The helper is
imported only by `_test.go` files; no SDK production code, harness scenarios or contract executables
change, so this test-infrastructure change cannot affect simulator contract behavior.

## Live integration tests

`b2/integration_test.go` skips unless `B2_APPLICATION_KEY_ID` and `B2_APPLICATION_KEY` are set. Use a
throwaway account or key, never one with production data. The tests create and delete their own buckets;
clean up leftovers with `go run ./internal/bin/cleanup`. Some tests have a one-minute deadline and can
time out when the network is busy; a single passing re-run does not make a failure flaky; treat a failure
as flaky or pre-existing only when the same test has failed intermittently across unrelated commits, and
until then record it as unresolved and note the re-run.

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

## Replication helper verification (#339)

`SetupReplication` takes caller-owned key IDs; it does not provision keys. `ReplicationStatus`
reports configured rules, including disabled rules, rather than per-file replication progress.
The tests and godoc in #339 leave executable SDK code unchanged. Record the exact tested SDK commit
and the separate simulator commit in the implementation PR's Verification section after committing
the change. That lets the evidence identify the final reviewed commit without putting a
self-referential commit hash into this file. The snapshot below describes the checks exercised;
it is not evidence for later revisions of main.

The focused rejection tests cover an otherwise matching disabled rule with and without a destination
mapping, and priority 2147483648 before any bucket reads or writes. The wire test preserves the literal
default priority 128 assertion and checks that the inclusive maximum 2147483647 reaches the source
update unchanged. The oversized input cannot be represented by `int` on 32-bit targets, so only that
case skips there; the maximum-priority wire case still runs.
`TestReplicationStatusIncludesDisabledRules` independently checks that status returns both enabled
and disabled rules with every configured field unchanged, without updating either bucket.

Each independent break was killed and passed after restoration:

| Break in `b2/replication.go` | Focused test | Red result |
| --- | --- | --- |
| Remove `\|\| !rule.IsEnabled` | `TestSetupReplicationRejectsDisabledRule` | Both mapping cases returned nil instead of the rule-conflict error. |
| Remove `\|\| settings.priority > maxReplicationPriority` | `TestSetupReplicationRejectsTooLargePriority` | Priority 2147483648 returned nil instead of the priority-validation error. |
| Change `settings.priority > maxReplicationPriority` to `>=` | `TestSetupReplicationDefaultAndMaximumPriorityOnWire/maximum` | Valid priority 2147483647 was rejected. |
| Filter out disabled rules in `ReplicationStatus` | `TestReplicationStatusIncludesDisabledRules` | Only the enabled rule was returned instead of both configured rules. |

On 2026-11-10 the registered scenario passed against a fresh loopback B2 simulator:

```sh
SDKHARNESS_TEST_LEVEL=conformance SDKHARNESS_SCENARIO=bucket.replication_helper \
SDKHARNESS_SIMULATOR_URL=http://127.0.0.1:<port> \
  .sdkharness/tests/run-conformance
```

Observed simulator result on that date (revision identifiers are recorded separately in the PR):

```text
NOTE blazer bucket.replication_helper: source rules=1 destination mappings=1 repeat=unchanged
SDKHARNESS_RESULT conformance bucket.replication_helper PASS -
```

A separate real-B2 run passed all 17 checks in the existing local
`blazer-go-demo replicationhelper` cap, including cleanup:

- Setup preserved an existing destination mapping and added the caller's source/destination key IDs.
- Repeating setup left the rule and mapping counts unchanged. A second destination reused the source
  key, and status returned both configured rules with the expected fields and priorities 128 and 133.
- All five option-conflict cases and both key-conflict cases returned their specific errors and left
  both configurations unchanged.
- Real restricted keys without `readBucketReplications` demonstrated unreadable source and destination
  rejection by both setup and status, with configurations unchanged.
- Cleanup deleted all three scratch buckets and six scratch keys. No key secrets were printed.

```text
PASS replicationhelper: 17 checks passed
```

To reproduce against the checked-out SDK without changing the shared demo's module replacement, set
`DEMO_DIR` to the existing configured live rig, then run from the SDK repository. This is an external,
local-only rig, not a program shipped in the SDK; the offline SDK tests do not require it.

```sh
sdk=$(pwd)
scratch=$(mktemp -d)
cp "$DEMO_DIR/go.mod" "$scratch/demo.go.mod"
if test -f "$DEMO_DIR/go.sum"; then cp "$DEMO_DIR/go.sum" "$scratch/demo.go.sum"; fi
(
  cd "$DEMO_DIR" &&
  GOWORK=off go mod edit -modfile="$scratch/demo.go.mod" \
    -replace="github.com/Backblaze/blazer=$sdk" &&
  GOWORK=off go run -modfile="$scratch/demo.go.mod" . replicationhelper \
    --report "$scratch/replicationhelper.md"
)
```

The live rig reads its existing `.env`, uses unique scratch resources and defers cleanup. Its report
is separate from simulator evidence. Neither run claims to verify per-file replication effects.
