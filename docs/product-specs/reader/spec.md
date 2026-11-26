# Spec: Reader download contract
From: [intent.md](intent.md) (Felipe Fumero). Issue: #528. Status: ready for review.

This standing spec describes `286e90d`. Code citations refer to that revision.
A named test supplies evidence only for its assertions.
This document does not claim that tests ran against live B2.

## Requirements

- **R1. Construction.** `Object.NewReader` creates a range reader at offset zero with length -1.
  `NewRangeReader` creates a Reader using the supplied context, offset, length and optional SSE-C key.
  Source: `b2/b2.go:879`, `b2/b2.go:918`. No test pins this.

- **R2. Range length.** A zero length returns no bytes and makes no download request. A negative length reads the rest of the object.
  Source: `b2/b2.go:865`, `b2/reader.go:373`. Tests: `TestReaderZeroLengthRangeMakesNoRequests`, `TestProgressZeroAndNegativeRangeLengthWire`.

- **R3. Version selection.** A resolved Object supplies its file ID. Otherwise the first download resolves the name and pins the returned ID for later chunks.
  If that response has no file ID, the reader returns an error. Source: `b2/reader.go:139`, `b2/reader.go:315`.
  Tests: `TestReaderPinsListedVersion`, `TestReaderParallelChunksPinFirstVersion`, `TestReaderRejectsMissingVersionID`.

- **R4. Worker defaults.** `ConcurrentDownloads` below one uses one worker. `ChunkSize` below one selects 10,000,000 bytes.
  Source: `b2/reader.go:358`, `b2/reader.go:362`. `TestReaderChunkSizeOneAndDefaults` pins the chunk size. No test pins the worker count: tests pass 1 or 10 and never a value below one.

- **R5. End of file.** Once a final chunk reaches EOF, subsequent reads return EOF.
  Source: `b2/reader.go:373`, `b2/reader.go:392`. Test: `TestReaderReadAfterEOF`.

- **R6. Close.** Close cancels the reader context, waits for workers and the progress reporter, removes the reader from client status, and returns nil.
  Source: `b2/reader.go:108`. Tests: `TestReaderDoubleClose` checks Close returns nil twice; `TestProgressCloseWaitsForInitialization` checks initialization and the final callback; `TestMappingStatusCleanup` checks status removal; `TestProgressCancelClosesBlockedBody` checks that cancellation closes a blocked body and Close returns.

- **R7. Progress.** Set `Progress` before the first read. It reports cumulative bytes on one goroutine and can coalesce updates.
  Close waits for the final callback. Calling Close from the callback is unsupported.
  A finite range reports available bytes when HEAD determines them; otherwise the total is -1.
  Source: `b2/reader.go:52`, `b2/reader.go:320`, `b2/progress.go:73`. Tests: `TestProgressRangeBeyondEOFUsesActualTotal`, `TestProgressRangeUnknownWhenHEADUnavailable`, `TestProgressCloseWaitsForInitialization`. No test pins calling Close from the callback.

- **R8. Verification.** `Verify` compares the accumulated SHA1 with the reported SHA1. Before a read initializes the hash, it returns `(nil, false)`.
  A mismatch returns an error with `true` only when the offset is zero, the read reached off-end, and the reported hash has 40 characters.
  An exact-length read with a bad checksum can return `(nil, false)` without an off-end read.
  Source: `b2/reader.go:441`. Tests: `TestFindingC37_VerifyBeforeReadDoesNotPanic`, `TestProgressZeroLengthVerifyDoesNotPanic`, `TestReaderVerify`. No test pins an exact-length read with a bad checksum.

- **R9. Short responses.** A truncated chunk is retried. After 21 attempts, the reader wraps `io.ErrUnexpectedEOF` in the error returned by Read.
  Callers can match that cause with `errors.Is`. Source: `b2/reader.go:37`, `b2/reader.go:253`, `b2/reader.go:257`, `b2/reader.go:384`. Tests: `TestReaderGivesUpOnPersistentlyTruncatedChunk`, `TestReaderSurvivesTransientTruncation`.

- **R10. Invalid range response.** `base.ErrInvalidRangeResponse` identifies a range response rejected by the base layer.
  Callers can use `errors.Is` on the Reader error. Source: `base/base.go:2061`, `base/base.go:2080`, `b2/reader.go:227`.
  Test: `TestRangeReaderRejectsFullObjectResponse`.

- **R11. Status.** `Client.Status` includes current readers in `StatusInfo.Readers`. `ReaderStatus.Progress` uses zero-based chunk IDs.
  Missing, retrying or failed chunks are zero; completed chunks are one. Speculative probes after the earliest confirmed EOF are omitted.
  Source: `b2/monitor.go:34`, `b2/monitor.go:108`, `b2/monitor.go:130`; `b2/reader.go:410`. Tests: `TestReaderStatusIncludesAllocatedChunks`, `TestReaderStatusKeepsFailuresBeforeEOFIncomplete`, `TestReaderStatusAfterRetryExhaustion`, `TestMappingStatusCleanup`.

- **R12. Concurrent callers.** A general guarantee for concurrent `Read` calls or concurrent `Close` calls is not specified in code or godoc.
  `Read` mutates the current chunk, hash and read position; initialization uses `sync.Once`.
  Close waits for concurrent first-read initialization. Source: `b2/reader.go:77`, `b2/reader.go:109`, `b2/reader.go:389`. `TestProgressCloseWaitsForInitialization` checks Close against first-read initialization.

- **R13. Error inspection.** `b2.IsNotExist` tests a direct `b2err` value; it does not inspect an outer wrapper.
  `b2err.Unwrap` returns its underlying error. `base.Code` and `base.MsgCode` are separate status inspection APIs.
  A general Reader-specific list of error types supported by `errors.As` is not specified in code or godoc.
  Source: `b2/b2.go:456`, `b2/b2.go:466`, `b2/b2.go:470`; `base/base.go:169`, `base/base.go:178`. `TestGetFileInfoByNameNotFoundUnwraps` checks `IsNotExist` and `errors.Is` for a different API; it does not establish a Reader not-found contract.

- **R14. Offset validation.** `NewRangeReader` stores the supplied offset without validating it at construction.
  This statement does not specify later backend or server validation. Source: `b2/b2.go:878`, `b2/b2.go:890`. No test pins this.

- **R15. SSE-C validation.** `NewRangeReader` records an error for a nil, uninitialized or multiple SSE-C keys.
  The constructor returns a Reader; the first Read returns the recorded error.
  Source: `b2/ssec.go:103`, `b2/b2.go:880`, `b2/b2.go:893`, `b2/reader.go:373`.
  Tests: `TestFindingC37_VerifyBeforeReadDoesNotPanic`, `TestZeroSSECKeyRejectedByPublicOperations`, `TestSSECReadRejectsExplicitNilKeyBeforeRequest` cover uninitialized and nil keys. No test pins multiple keys on this constructor.

- **R16. Request retries.** Name and ID downloads use reauthorization inside bounded backoff. An error classified as reauthorization triggers a refresh and one further call.
  For download requests, `base.Action` classifies 401 as reauthorization and 500, 502, 503 and 504 as retry, unless an earlier condition applies.
  Refresh or retry errors are returned. Source: `b2/backend.go:646`, `b2/backend.go:668`, `b2/backend.go:1062`, `b2/backend.go:1099`; `b2/baseline.go:253`; `base/base.go:128`, `base/base.go:138`.
  Test: `TestDownloadFileByIDReauthorizesOn401` checks the separate single-request API. No test pins this for the chunked Reader path: `TestReauth` covers `createBucket` only.

- **R17. Cancellation.** When a chunk waiter observes context cancellation without a recorded error, it returns the context error; an already recorded error takes precedence.
  Close cancels the context and returns nil after waiting. The error from Read can depend on the state of the download.
  Source: `b2/reader.go:106`, `b2/reader.go:283`, `b2/reader.go:304`, `b2/reader.go:373`.
  Tests: `TestReaderCancellationReleasesChunkWaiter`, `TestReaderDoubleClose`.

## Design

This document records the existing Reader. It adds no implementation, options or wire fields.

## How each constraint is met

| Constraint | Met by |
| --- | --- |
| Current behavior | Requirements cite the fixed baseline. |
| Defaults and limits | R2, R4 and R9. |
| Errors and concurrency | R6, R8–R10 and R12–R17. |
| Observable state | R7 and R11. |
| Evidence gaps | Requirements state when no test pins a claim. |

## Areas of concern

1. Concurrent caller guarantees are not stated in the Reader godoc. Owner: maintainer.
2. An exact-length read can leave a checksum mismatch indeterminate (R8). Owner: maintainer.
3. Several claims lack test citations in this document. Owner: maintainer.
