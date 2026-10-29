# Plan: assert the Content-Type sent on every upload (from intent.md and spec.md)

Branch: `test-issue-197-upload-headers`, stacked on `test-issue-191-shared-recording-server`. One PR for
issue #197 carries this folder and the tests. Apply the lean-code ladder from `AGENTS.md`: reuse
`internal/b2wiretest` and existing fixtures, add only what the tests need.

## Files that change

```
b2/upload_content_type_wire_test.go        (new)  R1-R3
intent/upload-content-type/plan.md         (edit only if the implementation departs from this plan)
```

No production file changes (R4).

## Order of work

0. **Get #191's code.** Run `git merge test-issue-191-shared-recording-server` (merge, never rebase). If
   `internal/b2wiretest/server.go` is still missing, stop: blocked on #191. Read
   `intent/b2wiretest/spec.md` for the helper API.
1. **Fixture.** Script the authorization (the `updateAuthJSON` shape from `b2/update_cors_name_test.go`),
   one bucket (reuse an existing bucket JSON constant), `upload_file` and `b2_finish_large_file` (reuse an
   existing file JSON if one fits), and `b2_start_large_file` (`{"fileId":"large-1"}`).
2. **Table test** (R1–R3). `TestWriterSendsContentType` with four rows:
   - small, explicit: write 10 bytes, `Close`, `AssertHeader(Content-Type, "text/html; charset=utf-8")`;
   - small, default: the same without the option, expecting `application/octet-stream`;
   - large, explicit: `ChunkSize = 10`, write 25 bytes, `AssertJSON(contentType, "text/html; charset=utf-8")`;
   - large, default: expecting `application/octet-stream`.

   If a small `ChunkSize` is rejected, use the smallest accepted value and note it in `plan.md`.
3. **Check and self-review.** `make ci`; then a fresh-context review of the diff per `AGENTS.md`, answering
   each finding.

## Risks

- **A new test fails on current code.** Then it found a product bug. Stop and report; do not fix it here.
- **Header canonicalization.** Go canonicalizes header names, so read `Content-Type` through `AssertHeader`
  (which uses `http.Header.Get`), never by map key.
- **Changelog.** Test-only diff: `make changelog-check` passes without an entry.

## Proof

Apply each break, run `go test -count=1 ./b2 -run WriterSendsContentType`, record the failing output,
revert, record the passing output. Both breaks were checked on 2026-10-28 to leave `./base ./b2/...`
green today.

| Req | Break to apply | Test that must fail |
| --- | --- | --- |
| R1, R3 | `b2/writer.go:355`: `if ctype == "" {` to `if ctype != "" {` | `TestWriterSendsContentType` (small rows) |
| R2, R3 | `b2/writer.go:414`: `if ctype == "" {` to `if ctype != "" {` | `TestWriterSendsContentType` (large rows) |

Also record `go test -race -count=5 -shuffle=on ./b2` passing with `B2_APPLICATION_KEY_ID`,
`B2_APPLICATION_KEY`, `B2_ACCOUNT_ID` and `B2_ACCOUNT_KEY` unset, and `make ci` passing.
