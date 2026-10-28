# Spec: prove the Writer sends retention and legal hold on every upload path
From: intent.md (Felipe Fumero). Issue: #193. Status: ready for review.
Skills applied: none beyond `AGENTS.md`, `REVIEW.md` and `docs/testing.md`.

Sources of truth (each test links the page):
- B2 `b2_upload_file`: request headers `X-Bz-File-Retention-Mode` (`governance` or `compliance`),
  `X-Bz-File-Retention-Retain-Until-Timestamp` (milliseconds since the epoch), `X-Bz-File-Legal-Hold`
  (`on` or `off`).
- B2 `b2_start_large_file`: body fields `fileRetention` (`{"mode":..., "retainUntilTimestamp":...}`) and
  `legalHold`.
- B2 `b2_get_file_info`: response `fileRetention` and `legalHold`, each wrapped in
  `{"isClientAuthorizedToRead": true, "value": ...}`.

Fixed values used throughout: retention mode `compliance`, retain-until `time.UnixMilli(1767225600000)`
(sent as `1767225600000`), legal hold `on`.

## Requirements

- **R1. Small object.** `NewWriter(ctx, WithFileRetention(&FileRetention{Mode: RetentionCompliance,
  RetainUntil: until}), WithLegalHold(LegalHoldOn))`, writing fewer bytes than `ChunkSize` and closing,
  sends exactly one `upload_file` request with:
  - `X-Bz-File-Retention-Mode: compliance`;
  - `X-Bz-File-Retention-Retain-Until-Timestamp: 1767225600000`;
  - `X-Bz-File-Legal-Hold: on`.

  It sends no `b2_start_large_file`. (Kills `b2/writer.go:369`.)
- **R2. Large object.** The same options with `ChunkSize` set below the data size (two parts) send one
  `b2_start_large_file` whose body has `fileRetention.mode` `"compliance"`,
  `fileRetention.retainUntilTimestamp` `1767225600000` and `legalHold` `"on"`. The upload then completes
  with `b2_finish_large_file`. (Kills `b2/writer.go:417`.)
- **R3. No lock asked, none sent.** A `Writer` without the options sends no retention or legal-hold header
  on `upload_file`, and no `fileRetention` or `legalHold` field on `b2_start_large_file`.
- **R4. Resume refuses a mismatched lock.** With `w.Resume = true`, `WithFileRetention` and
  `WithLegalHold(LegalHoldOn)`, and B2 reporting one unfinished upload of the same name
  (`b2_list_unfinished_large_files`) whose `b2_get_file_info` has no retention and legal hold `off`,
  `Close` returns an error. No `upload_part` and no `b2_finish_large_file` request is recorded.
  (Kills `b2/writer.go:436`.)
- **R5. Resume accepts a matching lock.** The same setup, with `b2_get_file_info` reporting the requested
  retention and legal hold, proceeds past the lock check: a `b2_list_parts` request is recorded.
- **R6. Test-only.** No file outside `*_test.go` and `intent/writer-object-lock/` changes. Existing tests
  stay as they are.

## Design

One new file, `b2/writer_object_lock_wire_test.go` (package `b2`), using `internal/b2wiretest`:

- A local fixture `newObjectLockWire(t)` returns the server and a `*Bucket`. It scripts
  `b2_authorize_account`, `b2_list_buckets` (or `b2_create_bucket`), `b2_get_upload_url` (built in),
  `upload_file` (a B2 file JSON), `b2_start_large_file` (`{"fileId":"large-1"}`), `upload_part`,
  `b2_get_upload_part_url` (built in) and `b2_finish_large_file` (a B2 file JSON).
- R1 to R3 are one table-driven test over {small, large} × {lock, no lock}. "Large" sets `w.ChunkSize` to a
  value smaller than the data, so the `Writer` takes the multipart path.
- R4 and R5 script `b2_list_unfinished_large_files` with one file named like the object, and
  `b2_get_file_info` with the mismatched or matching lock; R5 also scripts `b2_list_parts` with no parts.
- Header assertions use `AssertHeader` on `Last(t, "upload_file")`; body assertions use `AssertJSON` and
  `AssertNoJSON` on `Last(t, "b2_start_large_file")`; R4 checks `len(srv.Calls("upload_part")) == 0` and
  `len(srv.Calls("b2_finish_large_file")) == 0`.

## How each constraint in the intent is met

| Constraint | Met by |
| --- | --- |
| Test-only | R6 |
| Expected values from the B2 docs | The fixed literals above, each test linking its docs page |
| Credential-free, race-safe | `b2wiretest`; run under `-race -count=5 -shuffle=on` |
| Built on #191 | Only `internal/b2wiretest`, no new fake |

## Areas of concern

1. **Stacked on #191.** This branch starts from #191's branch; #191 merges first. Owner: Felipe.
2. **Small `ChunkSize` in tests.** B2 requires parts of at least 5 MB except the last; #182 is about
   validating that. If #182 adds that validation, R2's test must switch to a recommended part size from the
   scripted authorization and large enough data. Owner: whoever lands #182 second.
3. **Overlap with `b2/lock_wire_test.go:109,298`.** Kept for now (intent open question 1). Owner: @goanpeca.
