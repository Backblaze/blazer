# Surviving B2 mutants (#290, Part A)

The #290 baseline used Gremlins v0.6.0, no race detector, two workers, and a timeout
coefficient of 20 on `c3b00f1`. It reported 754 killed, 27 lived, 27 not covered,
and 17 timed out (96.54% efficacy, 96.66% mutator coverage).

Three of those survivors change observable behavior. The other 24 are equivalent
for reachable SDK states and are annotated in `b2/mutation_equivalence_test.go`.
No source file or mutation operator is excluded from normal testing to hide them.

## Whole-package remeasurement

The complete B2 subtree was remeasured with Gremlins v0.6.0, no race detector,
and live credentials unset:

```sh
env -u B2_APPLICATION_KEY_ID -u B2_APPLICATION_KEY -u B2_ACCOUNT_ID -u B2_ACCOUNT_KEY \
  gremlins unleash --timeout-coefficient 40 --workers 2 --output mutation-b2-full.json ./b2
```

| Run | Killed | Lived | Not covered | Timed out | Efficacy | Mutator coverage |
| --- | --- | --- | --- | --- | --- | --- |
| #290 baseline (`c3b00f1`, coefficient 20) | 754 | 27 | 27 | 17 | 96.54% | 96.66% |
| Part A (coefficient 40) | 774 | 24 | 27 | 0 | 96.99% | 96.73% |

```text
Mutation testing completed in 40 minutes 59 seconds
Killed: 774, Lived: 24, Not covered: 27
Timed out: 0, Not viable: 0, Skipped: 0
Test efficacy: 96.99%
Mutator coverage: 96.73%
```

Matching the full report to the original mutation types and locations confirms
all three behavioral survivors and all 17 historical timeouts are `KILLED`.
All 24 equivalent survivors are `LIVED`. The 27 uncovered mutations remain Part B.

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

All three real survivors and all 17 historical timeouts are now `KILLED`.
The remaining 24 original survivors are equivalent. Gremlins reported 23 of
them as `LIVED`; its `KILLED` result for the equal-value clamp at `reader.go:269`
did not reproduce with the exact mutation applied alone:

```text
go test -count=3 -failfast ./b2
ok  github.com/Backblaze/blazer/b2  33.060s
```

That anomalous result is not counted as a behavioral kill; the later full run
also reports it as `LIVED`. The scoped run deliberately concentrates on known
survivors, so its percentages are not comparable to a whole-package baseline.

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

## Mutation timeout checks

`.gremlins.yaml` raises the coefficient to 40, retains two workers, and keeps both
quality thresholds at zero. Completion checks use a five-second failure bound
and channels; ordinary runs complete without waiting for that bound. An isolated
test process contains the worker-launch loop, which can otherwise spawn enough
goroutines to strand context cancellation. In Gremlins'
unshuffled, fail-fast invocation they run before longer suites, so broken loops
and lost wakeups produce test failures instead of a mutation timeout.

New checks cover multipart `Write`/seekable `ReadFrom`, exact uploaded part bytes,
bounded downloads, empty progress ranges, startup errors, final listing pages,
resumed part pagination, reader-at offsets, context readers, and hashing.

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

The 17 historical timeouts were also applied individually. Every row exited 1
through its regression test (no `go test` timeout), then passed in the restored
focused suite:

| Baseline site | Test | Red output (excerpt) | Restored |
| --- | --- | --- | --- |
| `compose.go:325` | `TestComposeContextReaderReturnsBytes` | `Read = 0, <nil>, "\x00\x00\x00"; want 3, nil, abc` | PASS |
| `iterator.go:263` | `TestTerminalListingPageReturnsEOF` | `terminal page = [], <nil>, <nil>; want no objects, nil cursor, EOF` | PASS |
| `iterator.go:301` | `TestTerminalListingPageReturnsEOF` | `terminal page = [], <nil>, <nil>; want no objects, nil cursor, EOF` | PASS |
| `reader.go:257` | `TestZeroLengthProgressRangeDoesNotHead` | `Read = 0, context deadline exceeded; want 0, EOF` | PASS |
| `reader.go:294` | `TestTransferOperationsComplete` | `nine-byte upload and download did not complete within 5s` | PASS |
| `reader.go:301` | `TestTransferOperationsComplete` | `nine-byte upload and download did not complete within 5s` | PASS |
| `readerat.go:33` | `TestReaderAtReturnsRequestedBytesAndPreservesPosition` | `ReadAt = 0, <nil>, "\x00\x00"; want 2, nil, bc` | PASS |
| `readerat.go:38` | `TestReaderAtReturnsRequestedBytesAndPreservesPosition` | `ReadAt = 0, <nil>, "\x00\x00"; want 2, nil, bc` | PASS |
| `sync/sync.go:345` | `TestHashReaderCompletesWithDigest` | `hashReader = "", hash reader received an empty buffer; want abc SHA-1, nil` | PASS |
| `sync/sync.go:357` | `TestHashReaderCompletesWithDigest` | `hashReader = "", <nil>; want abc SHA-1, nil` | PASS |
| `writer.go:475` | `TestResumeStopsAtFinalPartPage` | `getLargeFile = <nil>, b2_list_parts: 400: unexpected extra part page; want resumed file, nil` | PASS |
| `writer.go:610` | `TestTransferOperationsComplete` | `nine-byte upload and download did not complete within 5s` | PASS |
| `writer.go:613` | `TestWriterWorkerStartupCompletes` | `one-worker startup did not complete within 5s` | PASS |
| `writer.go:617` | `TestSendChunkReturnsStartupError` | `sendChunk blocked instead of returning its startup error` | PASS |
| `writer.go:646` | `TestTransferOperationsComplete` | `panic: runtime error: invalid memory address or nil pointer dereference` | PASS |
| `writer.go:723` | `TestTransferOperationsComplete` | `nine-byte upload and download did not complete within 5s` | PASS |
| `writer.go:745` | `TestTransferOperationsComplete` | `upload = 6, <nil>; want 9, nil` | PASS |
