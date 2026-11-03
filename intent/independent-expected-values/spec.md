# Spec: replace self-referential expected values in five tests
From: intent.md (Felipe Fumero). Issue: #200. Status: ready for review.
Skills applied: none beyond `AGENTS.md`, `REVIEW.md` and `docs/testing.md`.

Sources of truth:
- B2 "String Encoding" (file names in URLs and headers are percent-encoded UTF-8; `/` is not encoded).
- The doc comments on `fileBuffer`, `AccountInfo`, `Object.Attrs` and the Reader retry budget
  (`b2/reader_retry_test.go:136-139`: "base lets a download retry 20 times after its first attempt").

## Requirements

- **R1. Encoding literals.** `base/strings_test.go` gains a table test of `escape` with literal outputs,
  including at least: `"a/b/c"` → `"a/b/c"`, `"a b"` → `"a+b"`, `"é"` → `"%C3%A9"`, `"a%b"` → `"a%25b"`,
  and `"a/b c/d"` → `"a/b+c/d"`, plus the matching `unescape` of each. `TestEncodeDecode` and `Fuzz` stay.
- **R2. File buffer against a known digest.** `TestFileBuffer` writes exactly 10,000,000 bytes of
  `"0123456789"` repeated (not `zReader`, whose trailing bytes per `Read` are unset) and asserts:
  `Len() == 10000000`; `Hash() == "71a0f071f6603f3c0b3a52d340cd713f0fe15a97"` (SHA-1 computed outside blazer);
  and the bytes read back from `Reader()` equal the input.
- **R3. Account info under concurrency.** `TestAccountInfoConcurrency`, for both stores: 8 goroutines each `Set`
  an `AuthData` whose `AccountID` and `AuthToken` both carry the goroutine index (`"account-3"`,
  `"token-3"`), 20 times, reading `AuthData()` after each `Set`. Every read must be a complete written
  value (index in `AccountID` equals index in `AuthToken`), never the zero value. After `wg.Wait()`, the final
  value is one of the written values; for `AccountInfoFile`, a fresh `NewAccountInfoFile(path)` loads that
  same value.
- **R4. Attrs after List makes no request.** A new credential-free test on an `x/b2sim` client: upload one
  object, `List` it, then call `Attrs` on the listed object and assert the number of HTTP requests did not
  change. Count requests with a wrapping `http.RoundTripper` passed through `b2.Transport`, not with
  `Status()`. The live `TestAttrsNoRoundtrip` is corrected to sum `table()` over methods.
- **R5. Reader budget literals.** `TestReaderBudgetMatchesTheDownloadRetryBudget` asserts: 20 truncated
  downloads then a whole one succeed with exactly 21 download calls and the full content; 21 truncated
  downloads fail. Both numbers are literals.
- **R6. Test-only.** Only `*_test.go` files and `intent/independent-expected-values/` change.

## Design

- R1: `base/strings_test.go`, a `TestEscapeLiterals` table.
- R2, R5: edit the existing tests in place.
- R3: rewrite the body of `TestAccountInfoConcurrency`.
- R4: new `b2/attrs_no_roundtrip_test.go` (package `b2`), client from `b2.NewClient(ctx, "test-id",
  "test-key", b2.Transport(counter))` where `counter` wraps `b2sim.New()`. `x/b2sim` has no blazer imports,
  so package `b2` can import it.

## How each constraint is met

| Constraint | Met by |
| --- | --- |
| Test-only | R6 |
| Expected values are literals | R1 table, R2 digest, R3 written values, R4 zero delta, R5 numbers |
| Credential-free, race-safe | All new tests offline; `-race -count=5 -shuffle=on` |

## Areas of concern

1. **R1 `+` for space.** `url.QueryEscape` encodes a space as `+`; B2 accepts `+` or `%20` for a space in
   names. If the team prefers `%20`, that is a product change for its own issue; this spec pins current
   output. Owner: @ffumero2003.
2. **R4 cache.** If `Attrs` after `List` does make a request on current code, that is a product bug
   (stop and report). Owner: @ffumero2003.
