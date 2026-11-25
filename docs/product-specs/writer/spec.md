# Spec: Writer upload contract
From: [intent.md](intent.md) (Felipe Fumero). Status: ready for review.

This standing spec describes `2df47dc`. Code citations refer to that revision.
A named test supplies evidence only for its assertions. This document does not claim that tests ran against live B2.

## Requirements

- **R1. Construction and options.** `Object.NewWriter` returns a Writer for that object's name and the supplied context.
  Client default options run first. Explicit options run synchronously before `NewWriter` returns.
  Source: `b2/b2.go:843`. Tests: `TestDefaultWriterOptionsRunOnlyForRealWriters`, `TestOrdinaryWriterOptionRemainsDefaultable`.

- **R2. Default chunk size.** A zero `ChunkSize` selects 100,000,000 bytes. Public Writer fields must be set before the first write.
  Source: `b2/writer.go:38`, `b2/writer.go:269`. No test pins this.

- **R3. Upload boundary.** An upload of at most the effective `ChunkSize` uses a simple upload.
  A larger upload starts a large file, uploads numbered parts and finishes the large file.
  This boundary applies to `Write` and seekable `ReadFrom`.
  Source: `b2/writer.go:337`, `b2/writer.go:795`, `b2/writer.go:844`.
  Test: `TestWriterChunkBoundaryWire`.

- **R4. Limits.** Negative `ChunkSize` returns an error before upload. The documented part-size limits are 5,000,000 to 5,000,000,000 bytes.
  The final part can be smaller than the minimum. B2 permits part numbers 1 through 10,000.
  The Writer does not locally enforce those positive size limits or a 10,000-part cap.
  Source: `b2/writer.go:61`, `b2/writer.go:270`, `b2/writer.go:649`.
  Service minimum and part numbers: `docs/b2-contracts.md:76`. Maximum: `docs/guides/large-and-resumable-uploads.md:3`.
  Test: `TestWriterNegativeChunkSizeRejectsBeforeUpload` pins negative rejection. No test pins this.

- **R5. Concurrent uploads.** `ConcurrentUploads` controls the number of large-file upload workers.
  Values below 1 select one worker. Each worker obtains a part upload URL and can reuse it for later parts.
  Source: `b2/writer.go:204`, `b2/writer.go:633`.
  Tests: `TestWriterStatusIncludesAllThreeConcurrentParts`, `TestTransferOperationsComplete`.

- **R6. Buffering.** Ordinary writes use memory buffers by default. `UseFileBuffer` selects temporary files under `FileBufferDir`.
  An empty directory setting uses the operating system's temporary directory. Buffer creation errors reach `Write` and `Close`.
  Close removes the current scratch file. Workers close their part buffers, including on resume mismatch or reader failure.
  Source: `b2/writer.go:70`, `b2/writer.go:236`, `b2/writer.go:277`, `b2/writer.go:827`; `b2/buffer.go:164`.
  Tests: `TestFileBufferLive` (live), `TestWriterCloseAfterBufferInitFailure`,
  `TestRegression_ResumeMismatchRemovesTempFile`, `TestWriterChunkReaderFailureRemovesTempFile`.

- **R7. Source reads.** `ReadFrom` reads through EOF. A seekable source without resume streams from its current position without local payload buffering.
  If seeking fails, `ReadFrom` uses the buffered path. Resume uses the buffered path even for a seekable source.
  Callers must not mix `Write` and `ReadFrom`, or call `ReadFrom` repeatedly on one Writer.
  Source: `b2/writer.go:676`, `b2/writer.go:726`.
  Tests: `TestWriterReadFromSeekableReaderUsesCurrentOffset`, `TestFindingB08_PipeSourceDoesNotProduceEmptyObject`,
  `TestReadFromFallsBackWhenEndSeekFails`, `TestProgressResumePipeWithoutCallback`.
  No test pins this. The gap concerns mixing or repeating calls.

- **R8. Empty upload.** Closing an unused Writer uploads an empty object through the simple path.
  Source: `b2/writer.go:823`, `b2/writer.go:844`. Test: `TestZeroByteObjectWire`.

- **R9. Content type.** Both upload paths use `application/octet-stream` when no content type is supplied.
  `WithAttrsOption` supplies an explicit content type.
  Source: `b2/writer.go:392`, `b2/writer.go:452`, `b2/writer.go:885`. Test: `TestWriterSendsContentType`.

- **R10. File info.** `WithAttrsOption` copies `Attrs.Info`. It adds nonempty `Attrs.SHA1` as `large_file_sha1` while fewer than 10 entries exist.
  It then adds nonzero `Attrs.LastModified` as `src_last_modified_millis` if space remains. The timestamp uses milliseconds.
  These derived entries can replace same-named caller entries only when the corresponding space check passes.
  Source: `b2/writer.go:885`, `b2/writer.go:894`.
  Test: `TestMappingFileInfoEntryLimit` pins the 9-entry and 10-entry boundary. `TestAttrs` (live) checks attribute round-trip.
  No test pins this. The gap concerns replacement of same-named caller entries at this boundary.

- **R11. Checksums.** Buffered uploads send the computed payload SHA1. Each buffered large-file part has its own SHA1.
  Seekable streaming uses `hex_digits_at_end` and appends a 40-character SHA1 trailer for each request.
  The Writer does not compute a whole-file `large_file_sha1` automatically; callers supply it through `Attrs.SHA1`.
  Source: `b2/writer.go:258`, `b2/writer.go:391`, `b2/writer.go:900`; `b2/buffer.go:72`, `b2/buffer.go:139`.
  Tests: `TestLargeUploadSendsEachPartSHA1`, `TestNonBuffer`, `TestZeroByteObjectWire`, `TestMappingFileInfoEntryLimit`.
  See the [checksum change spec](../sha1-upload-and-verify/spec.md).

- **R12. Resume selection.** Resume applies only to large uploads. It lists unfinished files using the object name as a prefix.
  Only exact names qualify. If several qualify, the last matching entry in iteration order is selected.
  If none qualifies, the Writer starts a new upload. It does not compare ordinary file info or content type before reuse.
  Source: `b2/writer.go:446`.
  Test: `TestResumeWriterWithoutExtantFile` (live) covers a missing candidate. No test pins this. The gaps concern multiple-candidate selection and ignored ordinary metadata.

- **R13. Resume parts.** The Writer lists all part pages and compares the source chunk hash with each existing part's hash by part number.
  Equal hashes skip the upload. Different hashes return a resume mismatch error.
  The source must be supplied from the beginning with the same chunk boundaries.
  Existing trailing parts beyond the new source prevent finishing.
  Source: `b2/writer.go:242`, `b2/writer.go:482`, `b2/writer.go:865`.
  Tests: `TestMappingResumeListsEveryPartPage`, `TestRegression_ResumeMismatchRemovesTempFile`,
  `TestRegression_ResumeShorterSourceIsRejected`, `TestResumeShorterSourceWithNewFinalPart`.

- **R14. Resume Object Lock.** Each supplied retention mode, retain-until millisecond and legal hold must match the unfinished upload.
  A mismatch returns an error before part reuse. Unspecified lock settings retain the unfinished upload's settings.
  Source: `b2/writer.go:475`, `b2/writer.go:556`.
  Tests: `TestWriterResumeChecksTheUnfinishedLock`, `TestWriterResumeRefusesEachMismatchedLockField`.
  See the [Object Lock change spec](../writer-object-lock/spec.md).

- **R15. Resume encryption.** Requested SSE-B2 must match the unfinished upload's mode and algorithm.
  SSE-C requires mode `SSE-C`, algorithm `AES256`, and matching nonempty caller and stored `sse_c_key_id` values.
  If B2 returns a key digest, it must also match. Resume without an encryption option rejects an SSE-C candidate.
  Caller IDs must identify the same key; skipped parts cannot prove possession of that key to B2.
  Source: `b2/writer.go:578`.
  Tests: `TestSSEB2ResumeRejectsWrongOrUnverifiableEncryption`, `TestSSECResumeRejectsDifferentOrMissingKeyIdentifier`,
  `TestSSECResumeRejectsContradictoryServerDigest`, `TestSSECResumeWithoutKeyRejectsEncryptedCandidate`,
  `TestSSECResumeMatchingKeySkipsExistingParts`.

- **R16. Upload encryption.** Without an encryption option, the Writer omits per-request encryption and leaves the bucket default applicable.
  `WithSSEB2("AES256")` applies B2-managed encryption to the simple upload or large-file start, not each part.
  Other algorithms return an error before an upload request. `WithSSECKey` sends the caller's configuration on the start and every part.
  Nil and zero-value SSE-C keys return errors. An explicit encryption option replaces inherited encryption defaults.
  Explicit SSE-B2 and SSE-C options in one `NewWriter` call conflict.
  Source: `b2/writer.go:295`, `b2/writer.go:978`, `b2/writer.go:1005`; `b2/b2.go:855`.
  Tests: `TestWriterSSEB2PerRequestWireAndMetadata`, `TestWithSSEB2RejectsInvalidAlgorithmsWithoutUpload`,
  `TestSSECMultipartSendsKeyOnStartAndEveryPart`, `TestNilSSECKeyCannotSilentlyUpload`,
  `TestZeroSSECKeyRejectedByPublicOperations`, `TestExplicitSSECOverridesDefaultSSEB2Wire`,
  `TestSSEB2AndSSECWriterOptionsAreMutuallyExclusive`.

- **R17. SSE-C identifiers.** Both upload paths add the caller's optional ID as `sse_c_key_id`, without adding key-derived data.
  Caller spellings match case-insensitively. Conflicting values, an ID for a key without one, or more than 10 entries after insertion return errors.
  The returned file info contains only the canonical spelling of the ID field.
  Source: `b2/writer.go:513`. Tests: `TestSSECSimpleUploadKeyIDFileInfo`, `TestMappingSSECKeyIDCaseConflict`,
  `TestSSECStartRejectsFileInfoOverLimit`, `TestSSECStartWithoutKeyIDDoesNotAddFingerprint`.

- **R18. Request-only keys.** `WithSSECKey` cannot be a client default, including with a nil key.
  `NewClient` rejects that default before authorization. Ordinary Writer options remain defaultable.
  Source: `b2/writer.go:1001`, `b2/writer.go:1047`.
  Tests: `TestDefaultWriterOptionsRejectWrappedSSECBeforeAuthorization`, `TestSSECKeyCannotBeClientWriterDefault`,
  `TestOrdinaryWriterOptionRemainsDefaultable`.

- **R19. Upload Object Lock.** Dedicated options send retention and legal hold on simple uploads and large-file starts.
  `WithAttrsOption` does not apply `Attrs.Retention` or `Attrs.LegalHold`.
  Invalid retention modes, zero retain-until times and invalid legal-hold values return errors.
  B2 enforces bucket settings and capabilities.
  Source: `b2/writer.go:295`, `b2/writer.go:407`, `b2/writer.go:456`, `b2/writer.go:938`; `b2/lock.go:50`.
  Tests: `TestWriterSendsObjectLock`, `TestWriterRejectsInvalidObjectLock`. `TestObjectLockLive` requires real credentials.
  See [Object Lock](../../object-lock.md) and [deletion enforcement boundaries](../../delete-options.md#verification-boundaries).

- **R20. Retry and URL reuse.** Retryable HTTP responses use the backend's bounded backoff and reset the source for each attempt.
  `Retry-After` can retry the same URL. Errors classified as `AttemptNewUpload` use a fresh upload URL and bounded reupload attempts.
  Part workers retain the refreshed part URL. Simple uploads return the last URL to the bucket pool only after success.
  Source: `b2/writer.go:359`, `b2/writer.go:380`, `b2/writer.go:404`, `b2/writer.go:1070`;
  `b2/backend.go:765`, `b2/backend.go:998`; `base/base.go:120`.
  Tests: `TestSmallUploadFetchesNewURLAfterConnectionFault`, `TestSmallUploadKeepsURLOnRetryAfter`,
  `TestFailedUploadDoesNotPoolURL`, `TestSuccessfulUploadPoolsURL`, `TestWriterLimitNewUploadAttempts`,
  `TestReuploadPartWithMaxReuploadsReached`.

- **R21. Close and errors.** Callers must close the Writer and check the returned error.
  Close waits for workers. It uploads the remaining part and finishes only when the multipart context has no error.
  Successful upload stores the returned file on the Object. The Writer retains its first recorded non-EOF error.
  Later writes and `ReadFrom` return `ErrClosed` after Close.
  Source: `b2/writer.go:154`, `b2/writer.go:323`, `b2/writer.go:706`, `b2/writer.go:815`.
  Tests: `TestWriterCloseDrainsWorkersAfterFailure`, `TestWriterBufferWriteErrorIsSticky`,
  `TestWriterWriteAfterClose`, `TestWriterReadFromAfterClose`, `TestZeroByteObjectWire`.

- **R22. Source and context errors.** A source error observed by `ReadFrom` prevents publication and reaches Close.
  A non-seekable blocked read need not finish before cancelled `ReadFrom` returns. The copy cannot write to the Writer afterward.
  Writer-caused cancellation returns the recorded upload error. Caller cancellation returns `context.Canceled` or `context.DeadlineExceeded` from `ReadFrom`.
  An `io.WriterTo` source can bypass `ReadFrom`; after any `io.Copy` error, callers must cancel before Close.
  Source: `b2/writer.go:685`, `b2/writer.go:713`; `b2/reader.go:478`.
  Tests: `TestFindingC5_PartialSourceIsNotCommittedByClose`, `TestWriterReadFromCancellationStopsWriteAfterClose`,
  `TestWriterReadFromReturnsInjectedUploadError`, `TestWriterReadFromCallerContext`, `TestUploadCopyErrorCancelsWriterTo`.

- **R23. Error cleanup.** By default, failure can leave an unfinished large file in B2.
  `WithCancelOnError` requests cancellation after a permanent error if a large-file handle exists.
  It uses a fresh context from `ctxf`; `errf` receives the cancellation result. Close waits for that callback.
  Callers must bound the cleanup context. The callback must return promptly and must not call Close.
  Current and worker-owned buffers are closed on their exit paths. Buffer-close errors are logged rather than returned as upload failures.
  Source: `b2/writer.go:154`, `b2/writer.go:236`, `b2/writer.go:809`, `b2/writer.go:1026`.
  Tests: `TestLargeFileCancellation`, `TestWriterCloseWaitsForCancelDeadline`, `TestWriterCloseWaitsForErrorCallback`,
  `TestWriterCloseBufferWarning`, `TestWriterChunkReaderFailureRemovesTempFile`.

- **R24. Progress.** `WithProgress` reports cumulative payload bytes, including skipped resume parts, without counting retries twice.
  A single goroutine delivers coalesced, monotonic callbacks. Close waits for the final callback; callbacks must not call Close.
  A successful size probe supplies the remaining seekable source length. Otherwise the total is -1.
  Progress measures bytes read, not B2 acceptance. Callers must check Close even if progress reaches the total.
  Source: `b2/writer.go:246`, `b2/writer.go:731`, `b2/writer.go:960`, `b2/writer.go:1135`.
  Tests: `TestProgressCallbacks`, `TestProgressMeterResetDoesNotDoubleCount`, `TestProgressResumeSkipDoesNotReadClosedBuffer`,
  `TestProgressConcurrentCloseWaitsForFinalCallback`, `TestProgressRejectedSimpleUploadWire`.

- **R25. Caller concurrency.** Callers must not call `Write`, `ReadFrom` or `Close` from several goroutines at the same time on one Writer.
  `ConcurrentUploads` applies only to internal part-upload workers.
  The write path takes a shared `closeWrite.RLock`. The file buffer changes its size and SHA1 state without a lock.
  Source: `b2/writer.go:318`, `b2/writer.go:633`; `b2/buffer.go:177`.
  No test pins this.

- **R26. Status progress.** `Client.Status` returns active writers in `StatusInfo.Writers`.
  Each `WriterStatus.Progress` entry gives a chunk's completion ratio. The index is the chunk ID minus one.
  Source: `b2/monitor.go:29`, `b2/monitor.go:99`, `b2/monitor.go:115`.
  Tests: `TestMappingStatusCleanup` checks active-writer registration and removal.
  `TestWriterStatusIncludesAllThreeConcurrentParts` checks `Progress` for three concurrent parts.

## Design

This document records the existing Writer. It introduces no implementation, options or wire fields.
The focused [Object Lock](../writer-object-lock/spec.md) and [checksum](../sha1-upload-and-verify/spec.md) specs retain their change-specific requirements.

## How each constraint is met

| Constraint | Met by |
| --- | --- |
| Current behavior only | Code citations use the fixed baseline `2df47dc`. |
| Defaults | R2, R5, R6 and R9 describe chunk size, workers, buffers and content type. |
| Limits | R4 distinguishes service limits from SDK checks. R10 and R17 describe metadata checks. |
| Caller-visible failures | R13 through R23 describe resume refusal, validation, retries, source errors and cleanup. |
| Test traceability | Each requirement names tests or states the missing test pin. |
| No code changes | Only this spec and its intent are added. |

## Areas of concern

1. R2 lacks a direct test for the exact 100,000,000-byte default. Owner: maintainers.
2. R4 documents service limits that the Writer does not validate locally. Tests using tiny chunks prove control flow, not B2 acceptance. Owner: maintainers.
3. R7 prohibits mixed or repeated input methods in godoc, without a test that enforces this caller obligation. Owner: maintainers.
4. R10 derives SHA1 before modification time. Near the entry threshold, one or both derived fields can be omitted. Owner: maintainers.
5. R12 selects the last exact-name candidate. The sources do not establish that candidate as the newest unfinished upload. Owner: maintainers.
6. R23 does not promise cleanup without `WithCancelOnError`. A failed start without a returned handle cannot use that handle for cleanup. Owner: maintainers.
7. ReadFrom counts scheduled seekable bytes before Close confirms completion (`b2/writer.go:787`). Treat its byte count as distinct from upload success. Owner: maintainers.
