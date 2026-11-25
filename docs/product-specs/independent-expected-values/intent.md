# Intent: replace self-referential expected values in five tests
Author: Felipe Fumero. Issue: #200. Parent: #97. Status: draft.

## Problem

Five tests compare the code's output with a value the same code produced, or never reach their assertion. Each
stays green when the behavior it names is broken (found by mutation and claim review at a5e0d13):

- `TestEncodeDecode` (`base/strings_test.go`) checks only that `unescape(escape(s)) == s`. `escape` replacing
  only the first `%2F` (`base/strings.go:23`) still round-trips, because `url.QueryUnescape` decodes `%2F`.
- `TestFileBuffer` (`b2/b2_test.go:1755`) compares `fileBuffer.Hash()` with a hash of `fileBuffer.Reader()`.
  Both see the same bytes, so a buffer that drops data on both paths passes.
- `TestAccountInfoConcurrency` (`b2/accountinfo_test.go:200`) has every goroutine store the same `AuthData` and
  discards every read. A `Set` that does nothing passes.
- `TestAttrsNoRoundtrip` (`b2/integration_test.go:884`) indexes `Status().table()["1m"]`, but `table()` is keyed
  by method name first, so both loops run zero times. It is also live-only, so CI never runs it.
- `TestReaderBudgetMatchesTheDownloadRetryBudget` (`b2/reader_retry_test.go:140`) checks only a nil error, and
  `TestReaderBudgetBoundary` derives its boundary from `maxShortReadAttempts` itself. Raising the constant
  passes both.

## Proposed outcome

Each test asserts against literals or independently computed values, and fails when its target is broken.

## Affected users and APIs

Test code only. No behavior change.

## Constraints

- Test-only. If a corrected test fails on current code, it found a product bug: stop and open a fix issue.
- Expected values are literals: from the B2 docs where a wire format is involved, otherwise from the documented
  contract (doc comments) or computed outside blazer, never from blazer's output.
- Credential-free and safe under `go test -race -count=5 -shuffle=on`.

## Open questions

None.
