# Intent: prove the Writer sends retention and legal hold on every upload path
Author: Felipe Fumero. Issue: #193. Parent: #97. Depends on: #191. Status: draft.

## Problem

`WithFileRetention` and `WithLegalHold` ask the `Writer` to upload an object under Object Lock. No
credential-free test drives those options through a real `Writer`, so each of these changes leaves
`go test ./...` green at a5e0d13:

- `b2/writer.go:369`: the simple upload passes `nil, ""` instead of `w.retention, w.legalHold`;
- `b2/writer.go:417`: `b2_start_large_file` gets `nil, ""` instead of the requested lock;
- `b2/writer.go:436`: the `checkResumedLock` call is removed, so `Resume` reuses an unfinished upload that
  lacks the requested lock.

What exists today:

- `TestObjectLockLive` (`b2/lock_test.go:26`) is the only test that goes through a `Writer` with these
  options, and it skips without B2 credentials.
- `TestUploadAndStartLargeFileCarryObjectLock` (`b2/lock_wire_test.go:109`) checks the wire, but calls
  the internal `uploadFile` and `startLargeFile` directly, so the `Writer` code above is never run.
- `TestResumeRefusesAnUnfinishedUploadWithoutTheRequestedLock` (`b2/lock_wire_test.go:298`) calls
  `checkResumedLock` directly, so whether `Resume` calls it is never checked.

A user who asks for a compliance-mode lock and silently gets an unlocked object can lose the protection
they rely on for retention or legal reasons. CI should catch that.

## Proposed outcome

Credential-free tests that write through `(*Object).NewWriter` with `WithFileRetention` and
`WithLegalHold`, and fail on each change above:

- a small object (one `b2_upload_file`) carries the retention and legal-hold headers;
- a large object (`b2_start_large_file` plus parts) carries them in the start request;
- `Resume` refuses an unfinished upload whose lock does not match, and sends no part.

## Affected users and APIs

- `(*Object).NewWriter`, `WithFileRetention`, `WithLegalHold` and `(*Writer).Resume`, as observed on the
  wire. Their behavior does not change.
- Test code only.

## Constraints

- **Test-only.** No production code changes. If a new test fails on current code, it found a product bug:
  stop and open a separate fix issue.
- Expected values come from the B2 docs for `b2_upload_file` (headers), `b2_start_large_file` (body) and
  `b2_get_file_info` (response), never from blazer's output.
- Credential-free, offline, safe under `go test -race -count=5 -shuffle=on`.
- Built on the shared recording server from #191; no new private fake.
- No new dependency; Go 1.26.10.

## Open questions

1. Whether the existing direct-call tests at `b2/lock_wire_test.go:109,298` should stay once these cover
   the same wire through the `Writer`. This intent keeps them. (@goanpeca)
