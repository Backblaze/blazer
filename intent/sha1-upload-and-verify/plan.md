# Plan: pin the SHA1 on upload parts and make Reader.Verify catch a bad hash (from intent.md and spec.md)

Branch: `test-issue-195-sha1-upload-and-verify`, stacked on `test-issue-191-shared-recording-server`. One
PR for issue #195 carries this folder and the tests. Apply the lean-code ladder from `AGENTS.md`: reuse
`internal/b2wiretest` and existing fixtures, add only what the tests need.

## Files that change

```
b2/sha1_wire_test.go                        (new)  R1-R5
intent/sha1-upload-and-verify/plan.md       (edit only if the implementation departs from this plan)
```

No production file changes (R6).

## Order of work

0. **Get #191's code.** Run `git merge test-issue-191-shared-recording-server` (merge, never rebase). If
   `internal/b2wiretest/server.go` is still missing, stop: blocked on #191. Read
   `intent/b2wiretest/spec.md` for the helper API. Note that `Call` is the last path segment of any
   request, so a download is recorded under the object name.
1. **Fixture.** Script the authorization (the `updateAuthJSON` shape from `b2/update_cors_name_test.go`)
   and one bucket (reuse an existing bucket JSON constant). For uploads, script `b2_start_large_file`
   (`{"fileId":"large-1"}`) and `b2_finish_large_file` (reuse an existing file JSON if one fits).
2. **Part hashes** (R1). `TestLargeUploadSendsEachPartSHA1`: `ChunkSize = 10`, write
   `[]byte("abcdefghijklmnopqrstuvwxy")` (25 bytes), `Close`. Then, for each recorded `upload_part`:
   - the header equals SHA1 of its body;
   - the part number maps to the expected slice (`[0:10]`, `[10:20]`, `[20:25]`).

   If today's code rejects a 10-byte `ChunkSize`, use the smallest it accepts and note it in `plan.md`.
3. **Verify** (R2–R5). `TestReaderVerify`, table-driven with four rows:
   - match;
   - one byte changed;
   - range read at offset 3;
   - header `none`.

   Each row scripts its own object name and response, reads, and asserts the `(error, bool)` pair.
   For the error row, assert `err != nil` and `ok == true`; don't assert the error text.
4. **Check and self-review.** `make ci`; then a fresh-context review of the diff per `AGENTS.md`, answering
   each finding.

## Risks

- **A new test fails on current code.** Then it found a product bug. Stop and report; do not fix it here.
- **Concurrent parts.** Parts upload concurrently; never assume order in the recording. Match by
  `X-Bz-Part-Number`.
- **Download fake shape.** If the reader rejects the scripted response, compare it with the one in
  `b2/progress_wire_test.go` and copy its headers. Do not change production code.
- **Changelog.** Test-only diff: `make changelog-check` passes without an entry.

## Proof

Apply each break, run `go test -count=1 ./b2 -run 'PartSHA1|ReaderVerify'`, record the failing output,
revert, record the passing output.

| Req | Break to apply | Test that must fail |
| --- | --- | --- |
| R1 | `base/base.go:1534`: send `strings.Repeat("0", 40)` instead of `sha1` | `TestLargeUploadSendsEachPartSHA1` |
| R3 | `b2/reader.go:185`: change `r.sha1 != sha1` to `r.sha1 == sha1` (never records it) | `TestReaderVerify` (mismatch) |
| R3 | `b2/reader.go:367`: change `r.offset > 0` to `r.offset >= 0` | `TestReaderVerify` (mismatch) |
| R2 | `(*Reader).Verify`: in `if r.sha1 == got`, compare against `""` instead of `r.sha1` | `TestReaderVerify` (match) |

Also record `go test -race -count=5 -shuffle=on ./b2` passing with `B2_APPLICATION_KEY_ID`,
`B2_APPLICATION_KEY`, `B2_ACCOUNT_ID` and `B2_ACCOUNT_KEY` unset, and `make ci` passing.
