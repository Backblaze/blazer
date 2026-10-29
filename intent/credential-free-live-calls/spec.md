# Spec: credential-free tests for calls only covered by live tests
From: intent.md (Felipe Fumero). Issue: #203. Status: ready for review.
Skills applied: none beyond `AGENTS.md`, `REVIEW.md` and `docs/testing.md`.

Sources of truth (each test links the page): B2 `b2_hide_file`, `b2_delete_file_version`,
`b2_get_download_authorization`, `b2_list_keys`, `b2_delete_key`, `b2_cancel_large_file`; B2 "Download
authorization" (the `Authorization` and `b2ContentDisposition` URL parameters); the `x/transport` doc comments.

## Requirements

### b2 (wire tests with `internal/b2wiretest`)

- **R1. Hide.** `Object.Hide` sends one `b2_hide_file` with `bucketId` and `fileName`, and returns nil; a
  scripted 400 error is returned to the caller.
- **R2. Reveal.** When the newest version of the name is a `hide` marker, `Bucket.Reveal` sends
  `b2_delete_file_version` with that marker's `fileId` and `fileName`. When the newest version is an upload,
  it sends no delete.
- **R3. Download authorization token.** `Bucket.AuthToken(ctx, "p/", time.Hour)` sends
  `b2_get_download_authorization` with `bucketId`, `fileNamePrefix` `"p/"`, `validDurationInSeconds` `3600`,
  no `b2ContentDisposition`, and returns the response `authorizationToken`.
- **R4. Download URL.** `Object.AuthURL(ctx, time.Hour, "attachment; filename=\"a b.txt\"")` sends
  `fileNamePrefix` equal to the object name and `b2ContentDisposition` equal to that value, and returns a URL
  whose query has `Authorization` equal to the token and `b2ContentDisposition` equal to the value (decoded).
  With an empty disposition the URL has no `b2ContentDisposition`.
- **R5. List keys.** `Client.ListKeys(ctx, 10, "")` sends `b2_list_keys` with `accountId` and `maxKeyCount`
  `10` and no `startApplicationKeyId`; a response with two keys and `nextApplicationKeyId` `"k-3"` returns two
  keys (name, ID, expiration from milliseconds) and cursor `"k-3"`; a second call with `"k-3"`
  sends `startApplicationKeyId` `"k-3"`. An empty `keys` list returns `io.EOF`.
  Capabilities mapping is tracked separately in #220.
- **R6. Delete key.** `Key.Delete` sends `b2_delete_key` with `applicationKeyId`.
- **R7. Cancel large file.** `Object.Cancel` on an unfinished (`start`) file sends `b2_cancel_large_file`
  with its `fileId`; on an uploaded file it returns an error and sends no cancel.

### base (`httptest`, as the existing `base/*_test.go` do)

- **R8. Upload after a connection hang.** This mirrors the live `TestUploadAuthAfterConnectionHang`: while one
  upload on an upload URL is hung, a second upload with the same URL and token is refused by B2 with
  `400 bad_request` and the message prefix `more than one upload using auth token` (restic/restic#1207).
  Against an `httptest` server: the first `b2_upload_file` handler blocks (released at test end); the second
  answers that 400. `Action(err)` for the second upload is `AttemptNewUpload`. Also: an upload whose
  transport fails before any response (a connection reset, not a context error) yields `AttemptNewUpload`.
  A context the caller cancelled or let expire is not an upload fault: `Action` is `Punt` (R9 covers the
  error itself).
- **R9. Cancellation and deadline.** A request made with an already-cancelled context returns an error that
  `errors.Is(err, context.Canceled)`; a request whose handler blocks past a short deadline returns an error
  that `errors.Is(err, context.DeadlineExceeded)`, and the handler observes its request context done. No sleeps
  beyond the deadline itself.
- **R10. Unknown CA.** Authorizing against `httptest.NewTLSServer` with the default transport (the test
  certificate is not trusted) fails with a certificate verification error (`*tls.CertificateVerificationError`
  or `x509.UnknownAuthorityError` via `errors.As`).

### x/transport (new `x/transport/transport_test.go`)

- **R11. One test per helper**, against an `httptest` server: `WithFailures(nil)` with no options passes
  requests through; `FailureRate(0)` never fails and `FailureRate(1)` always fails; `MatchPathSubstring`
  fails only matching paths; `Response(503)` with `Body("msg")` returns status 503 and body `msg`; `Stall`
  returns no earlier than its duration or when the request context ends, whichever is first; `AfterNBytes`
  calls its effect exactly once after roughly n body bytes; `Trigger(ctx)` fails every request after `ctx` is
  cancelled. If the `Trigger` test fails under `-race`, drop it and note #176 in the PR.

### Coverage

- **R12.** Record `go test -coverprofile` statement coverage for the module, credential-free, before and after
  this change in the PR description.
- **R13. Test-only.** Only `*_test.go` files and `intent/credential-free-live-calls/` change.

## Design

- `b2/live_only_calls_wire_test.go`: R1–R7.
- `base/live_only_calls_test.go`: R8–R10.
- `x/transport/transport_test.go`: R11.

## How each constraint is met

| Constraint | Met by |
| --- | --- |
| Test-only | R13 |
| Expected values from docs | Field names from the B2 pages; literal values |
| Credential-free, race-safe | `b2wiretest`, `httptest`; `-race -count=5 -shuffle=on` |
| `Trigger` race is #176's | R11 rule |

## Areas of concern

1. **R2 Reveal listing.** `Reveal` lists with `ListHidden()`; the scripted `b2_list_file_versions` page must
   put the newest version first, as B2 does. Owner: @ffumero2003.
2. **R10 error type.** Go wraps TLS failures in `*url.Error`; assert with `errors.As`, not string matching.
   Owner: @ffumero2003.
