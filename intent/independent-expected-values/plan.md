# Plan: replace self-referential expected values in five tests (from intent.md and spec.md)

Branch: `test-issue-200-independent-expected-values`, from `main`. One PR for #200 carries this folder and
the tests. Apply the lean-code ladder from `AGENTS.md`: reuse existing helpers (`readTruncated`, `x/b2sim`),
add only what the tests need.

## Files that change

```
base/strings_test.go                    (edit)  R1
b2/b2_test.go                           (edit)  R2
b2/accountinfo_test.go                  (edit)  R3
b2/attrs_no_roundtrip_test.go           (new)   R4
b2/integration_test.go                  (edit)  R4 (live test indexing only)
b2/reader_retry_test.go                 (edit)  R5
```

No production file changes (R6).

## Order of work

1. R1 `TestEscapeLiterals`.
2. R2 `TestFileBuffer` with the fixed input and literal digest.
3. R3 `TestAccountInfoConcurrency` rewrite.
4. R5 Reader budget literals.
5. R4 new simulator test, then the one-line live-test indexing fix.
6. `make ci`, then a fresh-context review of the diff per `AGENTS.md`, answering each finding.

## Risks

- A corrected test fails on current code: product bug; stop and report.
- `x/b2sim` lacks an endpoint the R4 path needs: stop and report; do not extend the simulator here.

## Proof

Apply each break in a scratch commit, run the named test, record the failure, revert, record the pass.

| Req | Break | Test that must fail |
| --- | --- | --- |
| R1 | `base/strings.go:23`: `strings.Replace(..., "%2F", "/", -1)` → `1` | `TestEscapeLiterals` |
| R2 | `fileBuffer.Write` (`b2/buffer.go:177`): write `p[:len(p)/2]` to `fb.w`, return `len(p)` | `TestFileBuffer` |
| R3 | `InMemoryAccountInfo.Set`, then `AccountInfoFile.Set`: return without storing (one at a time) | `TestAccountInfoConcurrency` |
| R4 | `Object.Attrs` path: bypass the cached file info so `b2_get_file_info` is always called | the new R4 test |
| R5 | `b2/reader.go:37`: `maxShortReadAttempts = 21` → `30` | `TestReaderBudgetMatchesTheDownloadRetryBudget` |

Also record `go test -race -count=5 -shuffle=on ./base ./b2/...` passing with `B2_APPLICATION_KEY_ID`,
`B2_APPLICATION_KEY`, `B2_ACCOUNT_ID` and `B2_ACCOUNT_KEY` unset, and `make ci` passing.
