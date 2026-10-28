# Plan: run the x/consistent tests by default (from intent.md and spec.md)

Branch: `test-issue-198-consistent-tests-run`, from `main` (it does not use #191's helper). One PR for
issue #198 carries this folder and the tests. Apply the lean-code ladder from `AGENTS.md`: reuse `x/b2sim`
and the package's own API, add only what the tests need.

## Files that change

```
x/consistent/consistent_test.go            (edit)  R1, R2
x/consistent/consistent_sim_test.go        (new)   R3-R6
intent/consistent-tests-run/plan.md        (edit only if the implementation departs from this plan)
```

No production file changes (R8). `x/b2sim` is not modified.

## Order of work

1. **Gate** (R1, R2). In `consistent_test.go`:
   - `TestMain` becomes `os.Exit(m.Run())`;
   - add `requireLive(t *testing.T)`, which skips with the spec's message when `BLAZER_TEST_CONSISTENT` is
     unset;
   - call it as the first line of `TestOperationLive`, `TestOperationJSONLive` and `TestMutex`.

   Run `go test -json ./x/consistent` and confirm three skips.
2. **Simulator fixture.** In `consistent_sim_test.go`, add `newSimBucket(t)`: `b2sim.New()`,
   `b2.NewClient(..., b2.Transport(sim))`, `client.NewBucket(ctx, "consistent", nil)`.
3. **Operate** (R3, R4). An `inc` helper. `TestOperateAppliesSequentialUpdates` (5 calls, expect `"5"`) and
   `TestOperateLosesNoConcurrentUpdates` (4 × 5 under goroutines with a `sync.WaitGroup`, expect `"20"`). Read
   the final value with one more `Operate` that returns its input unchanged and captures it.
4. **JSON and Mutex** (R5, R6). `TestOperateJSONRoundTrips` and `TestMutexLockUnlockUncontended`.
5. **Check and self-review.** `make ci`; then a fresh-context review of the diff per `AGENTS.md`, answering
   each finding.

## Risks

- **A new test fails on current code** (especially R4). Then it found a product bug. Stop and report; do not
  fix it here (spec concern 1).
- **A missing simulator endpoint.** If `x/b2sim` answers 501 or 404 for a call the package needs, stop and
  report it (spec concern 2).
- **Hangs.** Every test has a context timeout; `Mutex.Lock` sleeps one second per retry, so keep R6
  uncontended.
- **Changelog.** Test-only diff: `make changelog-check` passes without an entry.

## Proof

Apply each break, run `go test -count=1 ./x/consistent`, record the failing output, revert, record the
passing output.

| Req | Break to apply | Test that must fail |
| --- | --- | --- |
| R3 | `OperateStream` (`x/consistent/consistent.go:100`): skip `w.Close()` (return nil after the copy) | `TestOperateAppliesSequentialUpdates` |
| R4 | `save` (`x/consistent/consistent.go:318`): change `oldAI.Serial != ci.Serial-1` to `false` (no conflict detection) | `TestOperateLosesNoConcurrentUpdates` (report how many runs of `-count=5` fail; a lost update may not appear every run) |
| R4 | `Writer.Close` (`x/consistent/consistent.go:199`): change `ok && old != w.key` to `false` | `TestOperateLosesNoConcurrentUpdates` (same note) |
| R1, R7 | Restore the old `TestMain` | `go test -json ./x/consistent` shows no `"Action":"run"` events |

Also record `go test -race -count=5 -shuffle=on ./x/consistent` passing with `BLAZER_TEST_CONSISTENT`,
`B2_APPLICATION_KEY_ID`, `B2_APPLICATION_KEY`, `B2_ACCOUNT_ID` and `B2_ACCOUNT_KEY` unset, and `make ci`
passing.
