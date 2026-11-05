# Surviving B2 mutants (#290, Part A)

The #290 baseline used Gremlins v0.6.0, no race detector, two workers, and a timeout
coefficient of 20 on `c3b00f1`. It reported 754 killed, 27 lived, 27 not covered,
and 17 timed out (96.54% efficacy, 96.66% mutator coverage).

Three of those survivors change observable behavior. The other 24 remain `LIVED`
in Gremlins and are classified equivalent by human review of reachable SDK states.
The equivalence table below is the sole record of those classifications; comments
do not suppress mutants, and no source file or mutation operator is excluded from
the complete B2 run.

## Whole-package remeasurement

The complete B2 subtree was remeasured after rebasing on #291 (`2c1d1c8`), using
Gremlins v0.6.0, no race detector, and live credentials unset. The measured test
commit is `286f28d`:

```sh
env -u B2_APPLICATION_KEY_ID -u B2_APPLICATION_KEY -u B2_ACCOUNT_ID -u B2_ACCOUNT_KEY \
  gremlins unleash --timeout-coefficient 40 --workers 2 --output mutation-b2-full.json ./b2
```

| Run | Killed | Lived | Not covered | Timed out | Efficacy | Mutator coverage |
| --- | --- | --- | --- | --- | --- | --- |
| #290 baseline (`c3b00f1`, coefficient 20) | 754 | 27 | 27 | 17 | 96.54% | 96.66% |
| Part A review rerun (`286f28d`, coefficient 40) | 774 | 24 | 27 | 0 | 96.99% | 96.73% |

```text
Mutation testing completed in 46 minutes 48 seconds
Killed: 774, Lived: 24, Not covered: 27
Timed out: 0, Not viable: 0, Skipped: 0
Test efficacy: 96.99%
Mutator coverage: 96.73%
```

The final report below classifies mutants by file, line, column, and operator.
This matters at `writer.go:610:26`: its boundary mutation was a baseline survivor,
while its negation mutation was a baseline timeout. They are different mutants,
and each receives exactly one current status. Historical timeout is provenance,
not a second current classification.

Matching those identities against the saved baseline confirms **3 of the 27
original survivors are now KILLED and 24 remain LIVED**, with no additional LIVED
mutants. All **17 historical timeout mutants are now KILLED**; none is currently
timed out. The boundary mutant at `writer.go:610:26` remains LIVED, while the
negation mutant at the same location is KILLED.

**96.99% is the efficacy Part A delivers**, up from 96.54%. For the unchanged
production code and reviewed equivalents, this is the realistic ceiling:
`774 / (774 + 24) = 96.99%`. Equivalent LIVED mutants remain in that denominator;
they are not suppressed to inflate the score. The 27 not-covered mutations are
Part B's separate coverage work, including live-only limitations; they affect
mutator coverage, not efficacy's denominator.

## Scoped remeasurement

The selected run evaluated all 27 original survivors and all 17 original
timeouts, plus neighboring mutation operators on those lines:

```text
Mutation testing completed in 5 minutes 7 seconds
Killed: 54, Lived: 23, Not covered: 0
Timed out: 0, Not viable: 0, Skipped: 748
Test efficacy: 70.13%
Mutator coverage: 100.00%
```

This earlier scoped run reported the three behavioral survivors and the 17
historical timeouts `KILLED`, but also reported `reader.go:269:18`
(`CONDITIONALS_BOUNDARY`) `KILLED`, leaving 23 `LIVED`. Applying that exact mutation
alone did not reproduce its kill in three full test-suite replays:

```text
go test -count=3 -failfast ./b2
ok  github.com/Backblaze/blazer/b2  33.060s
```

The source review classifies that equal-value clamp as equivalent. Its earlier
scoped kill is anomalous and is not counted among the three behavioral kills;
the current whole-package result above is the authoritative measured status.
The scoped run concentrates on known survivors, so its percentages are not
comparable to a whole-package baseline.

## Behavioral gaps and break proofs

Each mutation below was applied alone, the named test run, and the production
line restored before the passing run. Line numbers refer to the baseline.

| Site | Exact break | Test | Red output | Restored |
| --- | --- | --- | --- | --- |
| `accountinfo.go:162:42` | cleanup `err != nil` → `err == nil` | `TestAccountInfoFilePersistence/cleanup_failure_after_replacement` | `write error = <nil>, want temporary file cleanup failed` | PASS |
| `accountinfo.go:221:35` | retry `err == nil` → `err != nil` | `TestAccountInfoRenameRetriesUntilSuccessOrDeadline` | `rename calls = 2, want 3`; `rename error = <nil>, want last sharing violation`; `rename calls = 2, want 4` | PASS |
| `baseline.go:414:26` | `len(attrs.CORSRules) > 0` → `>= 0` | `TestMappingLifecycleUpdateKeepsCORS` | `b2_update_bucket body corsRules: missing, want [{"allowedOperations":["b2_download_file_by_name"],"allowedOrigins":["https://example.com"],"corsRuleName":"browser"}]` | PASS |

The CORS response is scripted independently of the request; the assertion checks
the actual outbound body. Session tests inject specific errors, verify operation
counts, and check that the complete replacement remains readable after a cleanup
failure.

## Equivalent survivors

The 24 historical mutants represented below remain visible as `LIVED`;
"equivalent" is a human classification, not a Gremlins status or suppression
directive. Locations refer to the unchanged production code from `c3b00f1`.
The final three rows are additional audits for #295; they do not revise the
historical whole-package measurements above.

| Baseline site(s) | Mutation | Why behavior is unchanged |
| --- | --- | --- |
| `writer.go:516`, `replication.go:251`, `copy.go:162` | allocation hint `+1` → `-1` | A map hint changes allocation, not its entries or ability to grow. |
| `writer.go:610`, `reader.go:286` | `< 1` → `<= 1` | At equality the assignment stores 1 in an existing 1. |
| `writer.go:621` (two mutants) | negate `-1`; change its arithmetic sign | The initial value is always overwritten, or the method returns before using it. |
| `writer.go:699`, `reader.go:266` | `< 0` → `<= 0` | Assigning zero at zero changes nothing. |
| `writer.go:731`, `reader.go:269`, `writer.go:1017` | include equality in the clamp | The assigned value already equals the limit. |
| `writer.go:1022` | `<=` → `<` | At equality this only adds a no-op CAS; progress does not increase. |
| `writer.go:1023` | `>` → `>=` | At equality the additional delta is zero. |
| `reader.go:459` | `< 10s` → `<= 10s` | Starting at 1ms and doubling reaches 8192ms then 16384ms, never 10000ms. The existing sequence test pins reachable delays. |
| `replication.go:215` | `>` → `>=` | Assigning the same maximum at equality changes nothing. |
| `replication.go:219` | `>=` → `>` | At equality the following addition returns the same capped priority. |
| `iterator.go:102:29`, `iterator.go:108` | `>` → `>=` | Assigning a page cap at equality changes nothing. |
| `b2.go:774` | `>= 0` → `> 0` | Zero-length reads return before workers consult `bounded`. |
| `compose.go:274:21` | `< 0` → `<= 0` | Zero is already rejected by the size inequality: a validated range has at least one byte. |
| `progress.go:49:19` | `<= 0` → `< 0` | The extra zero wake produces no callback; terminal delivery is unchanged. |
| `sync/sync.go:351` | `> 0` → `>= 0` | Hashing an empty slice does not change SHA-1. |
| `backend.go:101` | `++` → `--` | Only equality of generation tokens is observed; both directions distinguish refreshes with the same uint64 wrap period. |
| `b2.go:901` (#295 audit) | `obj.Name() > name` → `>=` | The `== name` branch immediately above always returns. Control reaches this comparison only when `Name() != name`, where `>` and `>=` are identical. |
| `monitor.go:226` (#295 audit) | `ds[i] < ds[j]` → `<=` | Durations come from distinct map keys, so there is no equality case for the boundary to affect. |
| `sync/sync.go:298` (#295 audit) | `Failed[i].Rel < Failed[j].Rel` → `<=` | Only equal-Rel entries can be reordered; their order is unspecified either way. Pinning their relative order would require an over-fit test. |

## Mutation timeout checks

The Sync worker-startup check in #295 was measured separately at test commit
`e7e471e`, using Gremlins v0.6.0, unit tests without `-race`, and all four B2
credential variables blank:

```sh
gremlins unleash --workers 2 --timeout-coefficient 40 --output mutation-sync.json ./b2/sync
```

```text
KILLED INCREMENT_DECREMENT at sync.go:266:41
Mutation testing completed in 51 seconds 530 milliseconds
Killed: 72, Lived: 2, Not covered: 2
Timed out: 0, Not viable: 0, Skipped: 0
Test efficacy: 97.30%
Mutator coverage: 97.37%
```

The worker-loop decrement is now `KILLED`, rather than timed out, with the
early `a_worker_pool_test.go` regression. Only the boundary mutations at
`sync/sync.go:298:78` and `sync/sync.go:351:8` remain `LIVED`; both are equivalent
as audited above. This scoped result does not replace the historical
whole-package measurement.

`.gremlins.yaml` retains main's comments, two workers, and advisory zero thresholds;
its only change from main is coefficient 20 to 40. The two Make targets and both
workflow commands no longer pass `--timeout-coefficient 20`, so the config is the
single source of the timeout coefficient for local and CI runs. A diagnostic using
Gremlins v0.6.0's CLI entrypoint with no timeout flag resolved
`unleash.timeout-coefficient` to 40 from this repository's config. Both
`make mutation` and `make mutation-diff` completed for `b2` and `base` with the
real CLI in dry-run mode; the complete mutation measurement is the run above.

Transfer and startup-error checks share a five-second context helper that cancels
and joins the operation
before reporting timeout, so fixture cleanup cannot race the operation. An isolated
test process contains both writer startup and transfers: mutated reader/writer
launch loops can ignore cancellation or block on internal channels. The common
one-minute process deadline allows slow race-enabled startup; cancellation kills
and joins the child and its goroutines. In Gremlins'
unshuffled, fail-fast invocation they run before longer suites, so broken loops
and lost wakeups produce test failures instead of a mutation timeout.

New checks cover multipart `Write`/seekable `ReadFrom`, exact uploaded part bytes,
bounded downloads, empty progress ranges, startup errors, final listing pages,
resumed part pagination, reader-at offsets, context readers, and hashing.

The review reran the two reader mutations that ignore cancellation, one at a time:
`reader.go:294:23` (`INCREMENT_DECREMENT`) and `reader.go:301:28`
(`CONDITIONALS_NEGATION`). Each failed `TestTransferOperationsComplete` normally
with the parent killing and joining its child, before the outer 90-second test
timeout:

```text
--- FAIL: TestTransferOperationsComplete (60.00s)
operation did not complete within 1m0s; after cancellation: isolated completion: signal: killed
```

After restoring both production lines, the same focused command passed:

```text
ok  github.com/Backblaze/blazer/b2  3.601s
```

Install the CLI outside the SDK module dependencies:

```sh
go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0
```

Gremlins v0.6.0 has two diff limitations: selecting `./b2` from the repo root
produces package-relative source paths that do not match Git's `b2/...` paths,
and grouped diff hunks can omit later changed lines. To remeasure specific sites,
temporarily add same-line comments to those production lines, then use a root
scan with zero-context diffs and exclude packages outside this Part A scope:

```sh
env -u B2_APPLICATION_KEY_ID -u B2_APPLICATION_KEY -u B2_ACCOUNT_ID -u B2_ACCOUNT_KEY \
  GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=diff.context GIT_CONFIG_VALUE_0=0 \
  gremlins unleash --diff origin/main --timeout-coefficient 40 --workers 2 \
    --exclude-files '^(base|internal|x|bin|examples|\.sdkharness)/' \
    --output mutation-b2.json .
```

Restore the temporary comments after measuring. A tests-only diff otherwise has
no production mutation sites. A full B2-only run, independent of diff selection,
is `gremlins unleash --timeout-coefficient 40 --workers 2 ./b2`.

This work changes tests and tooling only. Live B2 and the simulator contract are
NOT RUN: no SDK behavior, request implementation, or harness scenario changes.
The untested-code work and all `base` survivors remain separate work.

The 17 historical timeouts were also applied individually before this review.
The excerpts below are those saved historical proofs, not fresh output from the
revised helper. Every row exited 1 through its regression test (no `go test`
timeout), then passed in the restored focused suite. The operator and column
identify each distinct mutant; the current full-run status is reconciled below.

| Baseline site | Operator | Test | Historical red output (excerpt) | Restored | Current full run |
| --- | --- | --- | --- | --- | --- |
| `compose.go:325:29` | `CONDITIONALS_NEGATION` | `TestComposeContextReaderReturnsBytes` | `Read = 0, <nil>, "\x00\x00\x00"; want 3, nil, abc` | PASS | KILLED |
| `iterator.go:263:9` | `CONDITIONALS_NEGATION` | `TestTerminalListingPageReturnsEOF` | `terminal page = [], <nil>, <nil>; want no objects, nil cursor, EOF` | PASS | KILLED |
| `iterator.go:301:9` | `CONDITIONALS_NEGATION` | `TestTerminalListingPageReturnsEOF` | `terminal page = [], <nil>, <nil>; want no objects, nil cursor, EOF` | PASS | KILLED |
| `reader.go:257:35` | `CONDITIONALS_NEGATION` | `TestZeroLengthProgressRangeDoesNotHead` | `Read = 0, context deadline exceeded; want 0, EOF` | PASS | KILLED |
| `reader.go:294:23` | `INCREMENT_DECREMENT` | `TestTransferOperationsComplete` | `nine-byte upload and download did not complete within 5s` | PASS | KILLED |
| `reader.go:301:28` | `CONDITIONALS_NEGATION` | `TestTransferOperationsComplete` | `nine-byte upload and download did not complete within 5s` | PASS | KILLED |
| `readerat.go:33:9` | `CONDITIONALS_NEGATION` | `TestReaderAtReturnsRequestedBytesAndPreservesPosition` | `ReadAt = 0, <nil>, "\x00\x00"; want 2, nil, bc` | PASS | KILLED |
| `readerat.go:38:49` | `CONDITIONALS_NEGATION` | `TestReaderAtReturnsRequestedBytesAndPreservesPosition` | `ReadAt = 0, <nil>, "\x00\x00"; want 2, nil, bc` | PASS | KILLED |
| `sync/sync.go:345:24` | `ARITHMETIC_BASE` | `TestHashReaderCompletesWithDigest` | `hashReader = "", hash reader received an empty buffer; want abc SHA-1, nil` | PASS | KILLED |
| `sync/sync.go:357:10` | `CONDITIONALS_NEGATION` | `TestHashReaderCompletesWithDigest` | `hashReader = "", <nil>; want abc SHA-1, nil` | PASS | KILLED |
| `writer.go:475:11` | `CONDITIONALS_NEGATION` | `TestResumeStopsAtFinalPartPage` | `getLargeFile = <nil>, b2_list_parts: 400: unexpected extra part page; want resumed file, nil` | PASS | KILLED |
| `writer.go:610:26` | `CONDITIONALS_NEGATION` | `TestTransferOperationsComplete` | `nine-byte upload and download did not complete within 5s` | PASS | KILLED |
| `writer.go:613:41` | `INCREMENT_DECREMENT` | `TestWriterWorkerStartupCompletes` | `one-worker startup did not complete within 5s` | PASS | KILLED |
| `writer.go:617:9` | `CONDITIONALS_NEGATION` | `TestSendChunkReturnsStartupError` | `sendChunk blocked instead of returning its startup error` | PASS | KILLED |
| `writer.go:646:9` | `CONDITIONALS_NEGATION` | `TestTransferOperationsComplete` | `panic: runtime error: invalid memory address or nil pointer dereference` | PASS | KILLED |
| `writer.go:723:11` | `CONDITIONALS_BOUNDARY` | `TestTransferOperationsComplete` | `nine-byte upload and download did not complete within 5s` | PASS | KILLED |
| `writer.go:745:32` | `CONDITIONALS_NEGATION` | `TestTransferOperationsComplete` | `upload = 6, <nil>; want 9, nil` | PASS | KILLED |
