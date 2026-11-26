# Spec: Sync product contract
From: [intent.md](intent.md) (Felipe Fumero). Status: ready for review.

This standing spec describes `286e90d`. Code citations refer to that revision.
A named test supplies evidence only for its assertions. This document does not claim that tests ran against live B2.

## Requirements

- **R1. Purpose and direction.** `Sync` pushes regular files below `localRoot` into `bucket` below `prefix`.
  The local tree is the source of truth. A local file that differs under the selected policy overwrites the cloud object, even when the cloud object is newer.
  Source: `b2/sync/sync.go:15-21`, `b2/sync/sync.go:138-145`; `README.md:258-272`.
  Test: `b2/sync/sync_test.go:566` `TestSyncPublicUploadWire` pins the B2 upload name, payload, SHA1 and source modification time.

- **R2. Destination names and local selection.** `Sync` appends `/` to a nonempty prefix that lacks one. It lists only that destination prefix and maps a local relative path to slash-separated `prefix + rel`.
  `Sync` resolves a symlinked root before walking. It skips non-regular entries, including symlinks below the root.
  Source: `b2/sync/sync.go:181-212`, `b2/sync/sync.go:227-250`.
  Tests: `b2/sync/sync_prefix_wire_test.go:15` `TestSyncListsOnlyTheDestinationPrefix`,
  `b2/sync/sync_test.go:803` `TestSyncFollowsSymlinkRoot`,
  `b2/sync/sync_test.go:822` `TestSyncSkipsSymlinksBelowRoot`, and
  `b2/sync/regression_unix_test.go:57` `TestRegression_NonRegularFilesAreSkippedAndRegularFilesUploaded`.

- **R3. Comparison policy.** The default and `CompareModTime` compare only source and destination modification times at millisecond resolution. Equal times skip even when sizes differ.
  `CompareSize` compares only size. `CompareNone` skips an existing object by name. `CompareSHA1` compares only SHA1 and uploads when the destination SHA1 is absent or `"none"`.
  Source: `b2/sync/sync.go:41-55`, `b2/sync/sync.go:118-136`, `b2/sync/sync.go:165`;
  `docs/upgrading.md:183-191`.
  Tests: `b2/sync/sync_test.go:41` `TestShouldTransfer` and
  `b2/sync/sync_compare_wire_test.go:31` `TestSyncComparisonModesIgnoreOtherMetadata`.

- **R4. Conflict rules and absent objects.** An absent destination always transfers. `Sync` rejects conflicting comparison options, conflicting concurrency options, a nil option, a nonpositive concurrency value and a comparison value outside `CompareModTime` through `CompareSHA1` before it accesses the local root or sends B2 requests.
  Repeated identical comparison or concurrency options are accepted.
  Source: `b2/sync/sync.go:69-92`, `b2/sync/sync.go:118-121`, `b2/sync/sync.go:165-180`.
  Tests: `b2/sync/sync_test.go:84` `TestSyncRejectsConflictingOptions`,
  `b2/sync/options_names_test.go:30` `TestSyncRejectsNilOptionsBeforeRequests`, and
  `b2/sync/sync_stress_test.go:64` `TestSyncRejectsInvalidInputs`.

- **R5. Deletes, overwrite and resume.** `Sync` never deletes a cloud-only object. When a local file requires transfer, `Sync` creates a Writer for the same destination name and sends the local content.
  By default, `Sync` does not set `Writer.Resume`, so a changed large file starts a fresh upload. Client default writer options can set `Resume`; the Writer can then reuse matching unfinished parts.
  The package godoc says it does not resume interrupted uploads, but client defaults can change that behavior.
  Source: `b2/sync/sync.go:15-20`, `b2/sync/sync.go:301`, `b2/sync/sync.go:390-413`; `b2/b2.go:843-862`; `b2/writer.go:47-59`, `b2/writer.go:446-473`.
  No test pins deletion or resume behavior under client defaults. Search: `rg -n -i 'delete|resume|unfinished|DefaultWriterOptions' b2/sync/*_test.go`; the matches do not assert either behavior.

- **R6. Dry run.** `WithDryRun` records every planned transfer in `Result.Uploaded`, leaves `Result.BytesUploaded` at zero and sends no upload request.
  It still walks files, evaluates the comparison policy and records per-file failures, including names longer than 1024 bytes.
  Source: `b2/sync/sync.go:91-101`, `b2/sync/sync.go:251-254`, `b2/sync/sync.go:280-287`.
  Tests: `b2/sync/sync_stress_test.go:121` `TestSyncDryRunAndCancellation` and
  `b2/sync/regression_b14_b29_test.go:42` `TestRegression_DryRunFlagsNamesB2WouldReject`.

- **R7. File-worker limit.** `Sync` uses four upload workers when no `WithConcurrency` option is supplied. A positive `WithConcurrency(n)` starts `n` workers and limits simultaneous file uploads to `n`.
  The workers receive jobs through an unbuffered channel and wait before `Sync` returns.
  Source: `b2/sync/sync.go:165`, `b2/sync/sync.go:290-329`.
  Tests: `b2/sync/sync_test.go:611` `TestSyncUsesRequestedConcurrency` and
  `b2/sync/a_worker_pool_test.go:12` `TestSyncEmptyTreeUsesBoundedWorkerPool` pin the requested limit and a bounded pool.
  No test pins the exact default of four. Search: `rg -n 'WithConcurrency\(4\)|concurrency: 4|workers.*4|4.*workers' b2/sync/*_test.go` returned no matches.

- **R8. Hashing and upload buffering.** `CompareSHA1` reads each selected local file through a 32 KiB buffer before deciding whether to transfer. If it uploads that file, `Sync` passes the computed SHA1 and source modification time to the Writer.
  `Sync` does not set Writer chunk or buffer options; client default Writer options can change them. The default chunk size is 100,000,000 bytes.
  Without `CompareSHA1` or `Resume`, a nonempty seekable local file uses the Writer's non-buffered section-reader path when seeking succeeds. `CompareSHA1` wraps the file in `io.TeeReader`; that non-seekable reader uses the buffered path. `Resume` also uses the buffered path. The buffered path uses memory by default or a file when `UseFileBuffer` is set. Empty files and seek failures fall back to the buffered path.
  Source: `b2/sync/sync.go:260-268`, `b2/sync/sync.go:370-388`, `b2/sync/sync.go:390-426`; `b2/b2.go:843-862`; `b2/writer.go:61-73`, `b2/writer.go:269-281`, `b2/writer.go:704-791`.
  Tests: `b2/sync/a_hash_completion_test.go:25` `TestHashReaderCompletesWithDigest` and
  `b2/sync/sync_test.go:850` `TestSyncSHA1LargeFileRoundTrip` pin SHA1 completion and use for a large upload.
  No test pins the exact 32 KiB buffer. Search: `rg -n '32\s*\*\s*1024|32768' b2/sync/*_test.go` returned no matches.

- **R9. Results and ordering.** `Result` separates uploaded, skipped and failed relative paths. A completed non-dry-run sync sorts `Uploaded` and `Failed` by relative path before it returns.
  The implementation does not sort `Skipped`. A dry run returns before the completed-run sorting step.
  `BytesUploaded` is the sum of successful upload byte counts only.
  Source: `b2/sync/sync.go:94-107`, `b2/sync/sync.go:270-287`, `b2/sync/sync.go:301-329`.
  Tests: `b2/sync/sync_result_test.go:25` `TestSyncSortsFailureResults` pins failed-path ordering, and
  `b2/sync/sync_stress_test.go:13` `TestSyncConcurrentTree` pins successful byte accumulation.

- **R10. Partial failure.** A failed local inspection, destination attribute read, SHA1 read, invalid object name or upload records one `FileError` and lets unrelated files continue.
  After work completes, `Sync` returns the aggregate error `sync: N files failed` when `Result.Failed` is nonempty.
  Source: `b2/sync/sync.go:196-215`, `b2/sync/sync.go:220-279`, `b2/sync/sync.go:301-336`.
  Tests: `b2/sync/sync_test.go:380` `TestSyncContinuesAfterMissingFile`,
  `b2/sync/sync_test.go:494` `TestSyncContinuesAfterSHA1ReadError`, and
  `b2/sync/sync_test.go:725` `TestSyncContinuesAfterInvalidB2Name`.

- **R10a. Destination listing failure.** If destination listing returns an iterator error, `Sync` stops and returns a nil `Result` and that error. A listing error may be a context error; the iterator error is returned directly. If an attribute read during listing fails while the context is cancelled, `Sync` returns a nil `Result` and `ctx.Err()` instead. A non-context attribute error is recorded for the matching local file, as described in R10 and R14; it is not a terminal listing failure.
  Source: `b2/sync/sync.go:196-215`, `b2/sync/sync.go:220-225`, `b2/sync/sync.go:255-257`, `b2/sync/sync.go:326-336`.
  No test pins this: terminal destination listing failure with a nil `Result`. Search: `rg -n -i 'b2_list_file_names|list.*(error|fail)|iter\.Err|cancel' b2/sync/*_test.go`; the matches do not assert this return.

- **R11. File integrity during sync.** Before hashing or uploading, `Sync` opens the walked path and verifies that the opened and current entries remain the walked regular file.
  It rejects an object that grows after the walk or whose streamed SHA1 differs from the pre-upload SHA1. It cancels the Writer and does not publish that object.
  Source: `b2/sync/sync.go:355-367`, `b2/sync/sync.go:426-457`.
  Tests: `b2/sync/sync_test.go:516` `TestSyncRejectsSymlinkSwapBeforeUpload`,
  `b2/sync/sync_test.go:975` `TestUploadFileRejectsGrowthBeforePublishing`, and
  `b2/sync/sync_test.go:1050` `TestUploadFileRejectsChangeDuringStream`.

- **R12. Large-upload failure cleanup.** For an upload error, `Sync` configures Writer cleanup with a fresh context that has a 30-second timeout. If B2 rejects the cancellation, `Sync` adds that cleanup error to the file error.
  A failed large-file upload does not stop other files.
  Source: `b2/sync/sync.go:401-418`, `b2/sync/sync.go:433-457`.
  Tests: `b2/sync/sync_test.go:747` `TestSyncCancelsFailedLargeUploadAndContinues`,
  `b2/sync/sync_test.go:779` `TestSyncReportsFailedLargeUploadCleanup`, and
  `b2/sync/sync_test.go:1018` `TestSyncCancelsMultipartCleanupContext`.

- **R13. Cancellation.** During destination listing, `Sync` returns the iterator error or, for a failed attribute read with a cancelled context, `ctx.Err()` as described in R10a. During walking or SHA1 calculation, the code checks `ctx.Err()`. After job dispatch and workers finish, it returns `ctx.Err()` if set.
  A cancelled worker stops taking further jobs. The result can contain work recorded before cancellation.
  Source: `b2/sync/sync.go:200-215`, `b2/sync/sync.go:227-279`, `b2/sync/sync.go:297-329`, `b2/sync/sync.go:370-387`.
  Tests: `b2/sync/sync_stress_test.go:121` `TestSyncDryRunAndCancellation` covers pre-cancellation and cancellation during walking;
  `b2/sync/sync_result_test.go:48` `TestSyncReturnsCancellationAfterWalk` covers cancellation after walking and pins the final context check after the workers finish (`b2/sync/sync.go:326`: without that check the test fails with `error = <nil>, want context canceled`); and
  `b2/sync/sync_test.go:1121` `TestHashReaderStopsAfterCancellation` covers the hash reader.

- **R14. Error identity.** On an unsupported platform, `Sync` returns a wrapped `errors.ErrUnsupported`, so callers can use `errors.Is(err, errors.ErrUnsupported)`.
  For cancellation, `Sync` returns `ctx.Err()` directly, so callers can use `errors.Is` with the context error. For a per-file failure, callers inspect `Result.Failed[i].Err`; the aggregate `sync: N files failed` error does not wrap that file error.
  No `errors.As` target is specified in code or godoc.
  Source: `b2/sync/open_other.go:26-31`, `b2/sync/sync.go:206-209`, `b2/sync/sync.go:220-225`, `b2/sync/sync.go:326-336`.
  Tests: `b2/sync/open_other_test.go:33` `TestSyncUnsupportedPlatform`,
  `b2/sync/sync_result_test.go:48` `TestSyncReturnsCancellationAfterWalk`, and
  `b2/sync/sync_test.go:459` `TestSyncContinuesAfterEntryInfoError`.
  No test pins an `errors.As` contract. Search: `rg -n 'errors\.As' b2/sync/*_test.go` returned no matches.

- **R15. Platform and local-root limits.** `Sync` supports Linux except Android, macOS except iOS, and Windows. On Android, iOS and other targets, it returns a nil result and an error wrapping `errors.ErrUnsupported` with `GOOS/GOARCH` before it walks files or makes B2 requests.
  A local root must resolve to a directory. An object name longer than 1024 bytes is a per-file failure in normal and dry runs.
  Source: `b2/sync/sync.go:155-160`, `b2/sync/sync.go:184-194`, `b2/sync/sync.go:251-254`; `b2/sync/open_unix.go:15-24`, `b2/sync/open_windows.go:15-22`, `b2/sync/open_other.go:15-27`; `docs/upgrading.md:185-191`.
  Tests: `b2/sync/open_other_test.go:33` `TestSyncUnsupportedPlatform`,
  `b2/sync/sync_stress_test.go:64` `TestSyncRejectsInvalidInputs`, and
  `b2/sync/options_names_test.go:86` `TestSyncObjectNameByteLimit`.

- **R16. Caller concurrency.** Within one `Sync` call, worker goroutines protect mutations of `Result` with a mutex.
  The code and godoc do not specify whether callers can invoke `Sync` concurrently for the same bucket, prefix or local root. No test pins that caller-concurrency behavior.
  Source: `b2/sync/sync.go:138-155`, `b2/sync/sync.go:290-329`.
  Test: `b2/sync/sync_stress_test.go:13` `TestSyncConcurrentTree` exercises internal workers, not concurrent calls.
  Search: `rg -n -i 'concurrent.*sync.*call|sync.*concurrent.*call|same.*bucket.*sync|same.*root.*sync' b2/sync/*_test.go` returned no matches.

## Design

This document records the existing `sync` package. It introduces no implementation, options or wire fields.

## How each constraint is met

| Constraint | Met by |
| --- | --- |
| Current behavior only | R1 through R16 cite the `286e90d` implementation and existing documents. |
| Direction and conflict rules | R1 through R5 describe local authority, comparison and no-delete behavior. |
| Defaults and limits | R6 through R8 and R15 describe dry runs, workers, buffering and names. |
| Errors, partial failure and cancellation | R10 through R14, including R10a, describe results, cleanup, cancellation and error matching. |
| Concurrent use | R7 and R16 distinguish internal workers from the unspecified caller contract. |
| Test traceability | Each requirement names opened tests or records its test gap and search. |
