# Spec: run the x/consistent tests by default
From: intent.md (Felipe Fumero). Issue: #198. Status: ready for review.
Skills applied: none beyond `AGENTS.md`, `REVIEW.md` and `docs/testing.md`.

Source of truth: the `x/consistent` doc comments, mainly `OperateStream`/`Operate`: "Operate guarantees that
no other callers have modified the contents of name in the meantime (as long as all other callers are using
this package)."

## Requirements

- **R1. The package always runs.** `TestMain` calls `os.Exit(m.Run())` unconditionally. The package-wide
  skip and its log line are removed.
- **R2. Live tests keep their gate.** `TestOperationLive`, `TestOperationJSONLive` and `TestMutex` call a
  helper at their start. The helper skips with
  `"set BLAZER_TEST_CONSISTENT to run the live x/consistent tests (experimental, flaky)"` when that variable
  is unset, before checking credentials. With the variable set, behavior is the same as today.
- **R3. Sequential updates.** On an `x/b2sim` bucket, five sequential `g.Operate(ctx, "counter", inc)` calls
  (where `inc` parses the bytes as an integer, empty meaning 0, and returns n+1) leave a value that a fresh
  `Operate` reads as `"5"`.
- **R4. Moved to #186.** A concurrent `Operate` test (four goroutines, no lost update) fails under `-race`
  on current code: `Bucket.Attrs` replaces the shared `Bucket.b` without synchronization
  (`b2/b2.go:543`), reached through `Group.info`. That is #186 item 4, whose acceptance criteria already
  require `go test -race` on concurrent `Operate` to be clean. The concurrent test lands in #186's PR as
  its failing regression test. This change adds no concurrent test.
- **R5. JSON.** `OperateJSON` with a `struct{ N int }` value, incremented three times, reads back `N == 3`.
- **R6. Mutex, uncontended.** `g.Mutex(ctx, "m").Lock()` then `Unlock()` returns, and a second `Lock()` then
  `Unlock()` on the same name also returns, within a 10-second test context.
- **R7. The default run shows tests.** `go test -json ./x/consistent` with no environment variables set
  contains `"Action":"run"` events for the R3, R5 and R6 tests, and `"Action":"skip"` for the three live tests.
- **R8. Test-only.** No file outside `x/consistent/*_test.go` and `intent/consistent-tests-run/` changes.

## Design

- `x/consistent/consistent_test.go`: change `TestMain` (R1) and add `requireLive(t)` (R2), called first
  in the three live tests. `startLiveTest` stays as it is.
- `x/consistent/consistent_sim_test.go` (new): R3, R5 and R6. The client is created as in
  `x/b2sim/sim_test.go`: `b2.NewClient(ctx, "test-id", "test-key", b2.Transport(b2sim.New()))`, then
  `client.NewBucket(ctx, "consistent", nil)`.
- Every new simulator test (R3, R5, R6) uses a context with a 10-second timeout, so a livelock fails
  instead of hanging. The live tests keep `context.Background()`, unchanged (R2).

## How each constraint in the intent is met

| Constraint | Met by |
| --- | --- |
| Test-only | R8 |
| Live tests stay gated | R2 |
| Expected values from the contract | R3 and R5 literals derived from the `Operate` doc comment |
| Credential-free, race-safe | `x/b2sim`; `-race -count=5 -shuffle=on` |

## Areas of concern

1. **Concurrency is #186's.** R4 moved there after the concurrent test exposed the `Bucket.Attrs` race
   (2026-10-28). Do not add a concurrent `Operate` test here. Owner: @ffumero2003.
2. **`x/b2sim` support.** It implements `b2_update_bucket` with `ifRevisionIs` conflicts
   (`x/b2sim/sim.go:383-390`), which `save` relies on. If a needed endpoint is missing, stop and report it;
   don't extend the simulator in this change.
