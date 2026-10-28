# Plan: prove the Writer sends retention and legal hold on every upload path (from intent.md and spec.md)

Branch: `test-issue-193-writer-object-lock`, stacked on `test-issue-191-shared-recording-server`. One PR
for issue #193 carries this folder and the tests. Apply the lean-code ladder from `AGENTS.md`: reuse
`internal/b2wiretest` and the existing JSON fixtures in `b2/*_test.go`, add only what the tests need.

## Files that change

```
b2/writer_object_lock_wire_test.go      (new)  R1-R5
intent/writer-object-lock/plan.md       (edit only if the implementation departs from this plan)
```

No production file changes (R6).

## Order of work

0. **Wait for #191's code.** Run `git merge test-issue-191-shared-recording-server` (merge, never rebase).
   If `internal/b2wiretest/server.go` is still missing, stop: blocked on #191. Read
   `intent/b2wiretest/spec.md` for the helper API.
1. **Fixture.** `newObjectLockWire(t)`:
   - script the authorization (reuse the shape of `updateAuthJSON` from `b2/update_cors_name_test.go`);
   - script one bucket. Reuse `noReplicationBucketJSON` (`b2/replconfig_repro_test.go`) or
     `updateBucketJSON` rather than writing a new bucket JSON;
   - script `upload_file`, `b2_start_large_file`, `upload_part` and `b2_finish_large_file`. For the file
     JSON returned by `upload_file` and `b2_finish_large_file`, reuse an existing fixture string if one
     fits (search `b2/*_wire_test.go` for `"fileId"`); otherwise add one constant.
2. **Upload paths** (R1–R3). `TestWriterSendsObjectLock`: a table over {small, large} × {lock, no lock}.
   - Small: write 10 bytes with the default `ChunkSize`.
   - Large: set `w.ChunkSize = 10` and write 25 bytes (three parts). Check today's code accepts this.
     If it rejects small parts, use the smallest size it accepts and note it in `plan.md`.
   - Lock rows assert the three headers (small) or the three body fields (large). No-lock rows assert their
     absence.
3. **Resume** (R4, R5). `TestWriterResumeChecksTheUnfinishedLock`: script
   `b2_list_unfinished_large_files` with one file named like the object and the documented
   `b2_get_file_info` Object Lock response shape carrying the mismatched lock (R4) or matching lock
   (R5), and `b2_list_parts` with no parts (R5).
   - R4: `Close` errors, and no `upload_part` or `b2_finish_large_file` is recorded.
   - R5: a `b2_list_parts` request is recorded.

   Follow how `getLargeFile` (`b2/writer.go:403-450`) lists and checks, so the scripted calls match the
   code path. `b2_list_unfinished_large_files` populates the candidate file's cached `FileInfo`, so
   `b2File.getFileInfo` returns that cache without a `b2_get_file_info` request. Putting the documented
   response shape in the list response exercises the real resume path without a dead script.
4. **Check and self-review.** `make ci`; then a fresh-context review of the diff per `AGENTS.md`, answering
   each finding.

## Risks

- **A new test fails on current code.** Then it found a product bug. Stop and report; do not fix it here.
- **Retries.** If a scripted response is malformed, the `Writer` may retry and record extra calls. Assert
  with `Last` and exact counts only where the spec requires them.
- **Concurrency.** Large uploads send parts concurrently. Do not assume part order in the recording;
  assert on the start request and on counts.
- **Changelog.** Test-only diff: `make changelog-check` passes without an entry.

## Proof

Apply each break, run `go test -count=1 ./b2 -run 'ObjectLock|ResumeChecks'`, record the failing output,
revert, record the passing output.

| Req | Break to apply | Test that must fail |
| --- | --- | --- |
| R1 | `b2/writer.go:369`: pass `nil, ""` instead of `w.retention, w.legalHold` | `TestWriterSendsObjectLock` (small, lock) |
| R2 | `b2/writer.go:417`: pass `nil, ""` instead of `w.retention, w.legalHold` | `TestWriterSendsObjectLock` (large, lock) |
| R4 | `b2/writer.go:436`: delete the `checkResumedLock` call and its `if` | `TestWriterResumeChecksTheUnfinishedLock` (mismatch) |
| R3 | `b2/writer.go:369`: pass `LegalHoldOn` instead of `w.legalHold` | `TestWriterSendsObjectLock` (small, no lock) |

Also record `go test -race -count=5 -shuffle=on ./b2` passing with `B2_APPLICATION_KEY_ID`,
`B2_APPLICATION_KEY`, `B2_ACCOUNT_ID` and `B2_ACCOUNT_KEY` unset, and `make ci` passing.
