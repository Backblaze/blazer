# Intent: run the x/consistent tests by default
Author: Felipe Fumero. Issue: #198. Parent: #97. Related: #186. Status: draft.

## Problem

`TestMain` in `x/consistent/consistent_test.go:25-31` calls `m.Run()` only when `BLAZER_TEST_CONSISTENT` is
set. Its comment explains why: the package is experimental, not currently maintained, and some of its tests
are flaky. All three tests in the file are live tests that also need B2 credentials.

The effect is that `go test ./...` prints `ok` for `x/consistent` while running no test at all. It does not
print a skip either, so the output looks like a pass. All of `x/consistent/consistent.go` is at 0% coverage
in a credential-free run. A regression in `Group.Operate`, the package's main promise ("no other callers
have modified the contents of name in the meantime"), would pass CI unnoticed. #186 plans changes to this
package, and it has no credential-free test to change it against.

## Proposed outcome

- `go test ./...` runs the `x/consistent` package and reports what it ran and what it skipped. The existing
  live tests stay gated exactly as today (credentials and `BLAZER_TEST_CONSISTENT`), and each one says why
  it skipped.
- New credential-free tests cover `Group.Operate`'s contract against a stateful in-memory B2 (`x/b2sim`):
  - sequential updates are all applied;
  - `OperateJSON` round-trips a value.

## Affected users and APIs

- `x/consistent` tests. The package's behavior does not change.
- Test code only.

## Constraints

- **Test-only.** No production code changes. If a new test fails on current code, it found a product bug:
  stop and open a separate fix issue (or add it to #186).
- **Keep the maintainers' gate on the flaky live tests.** They must not start running in CI. Only the
  package-wide `TestMain` gate changes.
- Expected values come from the package's documented contract (the `Operate` doc comment) and literals,
  never from its output.
- Credential-free, offline, safe under `go test -race -count=5 -shuffle=on`.
- `x/b2sim` is the in-memory B2 the repo already uses for behavior tests. `x/consistent` needs stateful
  bucket revisions and conflicts, which a recording server cannot model.

## Open questions

Concurrent `Operate` is not tested here. A concurrent test fails under `-race` on current code
(`Bucket.Attrs` replaces the shared `Bucket.b`, `b2/b2.go:543`); that is #186 item 4, so the test lands
with #186's fix.

1. `x/b2sim`'s future is still being decided (keep it, remove it, or replace it with an external
   simulator). If it is removed, these tests need another stateful fake. (@goanpeca)
2. `Mutex.Lock` sleeps one second between attempts, so a contended mutex test would be slow. This intent
   covers an uncontended lock and unlock only. (@goanpeca)
