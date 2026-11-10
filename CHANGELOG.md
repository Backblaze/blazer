# Changelog

Going forward from v0.6.0 (the first new version after the move to https://github.com/Backblaze/blazer), all notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Unreleased entries live in [`.changes/unreleased/`](.changes/unreleased/) until the next release. Add one with `changie new` (see [CONTRIBUTING.md](CONTRIBUTING.md#changelog-entries)).

## [0.10.0] - 2026-11-11

See the [upgrade guide](https://github.com/Backblaze/blazer/blob/v0.10.0/docs/upgrading.md) for migration guidance on the 12 breaking changes.

### Added

- `b2.SetupReplication` merges caller-owned source and destination key IDs into bucket replication configurations, and `b2.ReplicationStatus` reads configured rules. Both reject unreadable existing configuration; setup rejects conflicting options and key mappings.
- `b2/sync.Sync` pushes a local directory tree into a B2 prefix with configurable comparison, concurrency, dry-run reporting, and per-file failure results. It follows a symlinked root but not symlinks below it; changed large files are re-uploaded rather than resumed.
- `b2.WithAccountInfo` allows a client to reuse an authorized session through the default in-memory store or an opt-in `AccountInfoFile`. The file backend stores tokens and endpoints atomically with private POSIX permissions and never stores the application key. On Windows, session privacy depends on the directory ACLs; place the file under the user's profile directory, such as `os.UserConfigDir()`. A missing, corrupt or oversized cached session triggers normal authorization and replacement; oversized reads remain bounded.
- Per-request SSE-C keys for uploads, downloads, metadata lookup, and server-side copies. Large-file uploads send the key on the start request and every part; keys cannot be configured as client writer defaults. `NewSSECKeyWithID` accepts an optional caller-chosen key identifier, stored as `sse_c_key_id` in file info for every SSE-C upload and used for large-file resume. Blazer adds no key-derived fingerprint to metadata. Resume rejects missing or different IDs, and an unkeyed writer cannot resume an SSE-C upload. Uploads fail before any request when caller file info sets a conflicting `sse_c_key_id` in any letter case, sets one for a key without an ID, or would exceed 10 entries. Callers must not reuse an ID for another key: B2 rejects wrong keys on uploaded parts, but cannot check a key when every part is skipped.
- `b2.VerifySignature` checks the HMAC-SHA256 signature of a raw B2 event-notification body, and `b2.ParseEvent` decodes its events. The helpers reject missing or malformed signatures, empty signing secrets, and bodies larger than 1 MiB; callers must cap HTTP reads before passing a body to them and deduplicate at-least-once deliveries by event ID.
- `b2.WithSSEB2("AES256")` requests SSE-B2 for one upload without changing the bucket default. Resumed uploads validate the existing encryption mode and algorithm. An explicit writer encryption option overrides inherited client defaults. Passing SSE-B2 and SSE-C options to the same `NewWriter` call causes a non-empty `Write` and `Close` to return an error.
- `b2.WithProgress` reports byte-level upload progress, and `b2.Reader.Progress` reports byte-level download progress. Callbacks are serialized off transfer locks and finish with a terminal value; known upload sizes and finite download ranges report their actual totals. Upload progress also credits completed parts already present when resuming. Reaching the total or receiving a terminal callback does not confirm successful completion or replace checking `Writer.Close` or read errors.
- `(*b2.Bucket).DownloadFileByID` downloads the exact file version identified by its B2 file ID, including historical versions and optional per-request SSE-C keys; `(*base.Bucket).DownloadFileByID` provides low-level GET, HEAD, and range requests.
- `x/b2sim` provides an in-process B2 wire simulator for offline tests through `b2.Transport`, including bucket operations, simple and multipart uploads, downloads and ranges, file listing, and deletion. Unsupported operations return an error; policy enforcement and download-authorization signature checks are not simulated.
- `(*b2.Bucket).Compose` builds a large file from ordered uploaded and server-side copied parts, using `b2_copy_part`. It accepts content type, file info, SHA1, last-modified time, and Object Lock options; defaults its content type to `application/octet-stream`; validates known part sizes, copy ranges, and known totals up to 10 TB (10,000,000,000,000 bytes) before starting; permits overlapping copied ranges; and cancels an unfinished file if a part fails, even after caller cancellation. Final whole-file copies have an unknown size, so B2 enforces their size limits after the large file starts.
- `Key.BucketIDs` and `Key.Prefix` report a key's bucket restriction and name prefix (#227).
- `b2.ErrInvalidAccountInfo` supports `errors.Is` for malformed or oversized data read by `AccountInfoFile.AuthData` and lets custom stores request reauthorization with `ok=false`. `AccountInfo` now documents concurrency and optional `Err` reporting: clients propagate observed storage errors, while a shared store's last error can be overwritten or seen by another caller.

### Changed

- **Breaking:** `(*b2.Client).NewBucket` now applies `attrs.ReplicationConfig` when it creates a bucket, as its documentation states; callers that create buckets from another bucket's `Attrs()` should clear `ReplicationConfig` if they do not want its replication rules. `(*base.B2).CreateBucket` takes the replication configuration as a new parameter; pass `nil` to keep the previous behavior.
- **Breaking:** `(*b2.Object).NewReader`, `NewRangeReader`, and `Attrs` gained variadic `keys ...*SSECKey` parameters. Existing calls are unchanged, but uses as function values with their old method signatures no longer compile.
- **Breaking:** `Writer.Close` now waits for in-flight part uploads and, with `WithCancelOnError`, for `b2_cancel_large_file` and `errf` to finish. An `errf` that calls `Close` or waits on the goroutine calling `Close` deadlocks. Bound the cancellation RPC's wait with a deadline on the context returned by `ctxf`; `errf` must return promptly, as that deadline does not bound the callback.
- **Breaking:** `(*base.B2).CreateKey`, `CreateKeyMultiBucket`, and `(*base.Bucket).GetDownloadAuthorization` now reject negative lifetimes, positive lifetimes below one second, and lifetimes of 1000 days or more with `base.ErrInvalidDuration`. This also applies to `b2.Lifetime`, `b2.Deadline`, `Bucket.AuthToken`, and `Object.AuthURL`. Pass a lifetime of at least one second (longer fractional values truncate to whole seconds) and below 1000 days for keys, or zero for a non-expiring key; download authorization requires 1 to 604800 seconds.
- **Breaking:** Ranged `DownloadFile*` calls now return the new `base.ErrInvalidRangeResponse` for HTTP 200 at a nonzero offset, an overlong HTTP 200 or 206 response, a chunked response without `Content-Length`, or a missing or invalid `Content-Range` on HTTP 206. Callers should handle this error and ensure their server or transport honors `Range` requests (#179).
- **Breaking:** `(*b2.Client).CreateKey` rejects explicitly empty `b2.BucketIDs` scopes or any empty ID with `b2.ErrInvalidBucketIDs`; `(*b2.Bucket).CreateKey` rejects this client-only option even when empty, with `b2.ErrBucketIDsOnBucket`. Omit `BucketIDs` for an intentional account-wide key, or provide non-empty IDs through `Client.CreateKey` for a scoped key.
- **Breaking:** `b2.CopyToBucket(nil)` now makes `Bucket.Copy` return an error before sending any request instead of silently copying into the receiver bucket. Pass a valid destination bucket, or omit `CopyToBucket` to copy into the receiver bucket.
- **Breaking:** `(*b2.Client).NewBucket` validates replication settings before creating a bucket, and `(*b2.Bucket).Update` omits settings when `BucketAttrs.ReplicationConfig` is nil. Both reject empty configurations; partial replication updates also reject unreadable existing settings. Use nil to leave replication unchanged, including unreadable settings; supply valid role data with normal bucket-update and `writeBucketReplications` permissions, plus `readBucketReplications` when preserving an unspecified role, or supply both roles for an explicit complete replacement. Nil/empty destination maps retain their existing wire encoding for B2 to validate and are not supported clearing operations. Direct base callers now receive consistent replication unreadability markers from creation and updates, and nil configurations when both response roles are null.
- **Breaking:** The default `b2/sync.Sync` comparison and `CompareModTime` now use modification times alone at millisecond resolution; use `WithCompare(CompareSize)` or `WithCompare(CompareSHA1)` to detect size/content changes with unchanged times. Sync supports Linux, macOS and Windows, rejects unsafe file replacements there, and returns an error wrapping `errors.ErrUnsupported` with GOOS/GOARCH on all other targets before walking or sending requests; those targets remain buildable.
- **Breaking:** `Object.NewRangeReader` with length 0 returns no bytes instead of reading the rest of the object. Pass a negative length to read the remainder.
- **Breaking:** `b2.WriterOption` is now an interface so client defaults can be validated without executing callbacks. Migrate custom `func(w *b2.Writer) { ... }` options to `b2.WriterOptionFunc(func(w *b2.Writer) { ... })`; existing `With...` calls are unchanged. Option values are no longer callable: compose them in a `[]b2.WriterOption` passed to `NewWriter` or `DefaultWriterOptions` instead. Default callbacks run once per initialized writer during `NewWriter`, never during `NewClient` or authorization. SSE-C defaults remain rejected before authorization, including an explicit nil key. A nil `b2.WriterOption` interface or `b2.WriterOptionFunc(nil)` passed as a default also makes `NewClient` return an error before authorization.
- **Breaking:** `(*base.URL).UploadFile` and `(*base.Bucket).StartLargeFile` now reject more than one `base.FileLock` instead of silently ignoring the extras. Combine retention and legal hold in a single `FileLock`; zero or one argument still works as before. The new `(*base.URL).UploadFileWithSSEC` and `(*base.Bucket).StartLargeFileWithSSEC` methods enforce the same limit. See `docs/object-lock.md` for this validation and the original v0.9.0 interface/function-type migration.

### Fixed

- A rejected bucket replication update no longer leaves the attempted configuration in the local bucket cache, where a later unrelated update could resend it.
- `b2/sync` treats an unavailable destination SHA1 as changed, stops hashing after context cancellation, rejects files swapped for symlinks after walking, cancels an upload if the streamed bytes differ from the walk-time SHA1, and reports a destination object whose attributes cannot be read only as a failure of the local file that maps to it.
- `AccountInfoFile` rejects cached sessions read from non-private POSIX paths, incomplete sessions reauthorize and refresh the cache, and verbose authorization-response logging omits the response body entirely.
- Verbose B2 wire logging redacts authorization tokens, newly created application keys, and SSE-C key fields; logs header names without values and omits malformed or failed response bodies and untrusted transport error text. This fixes credential exposure in logs (#149).
- SSE-C large-file starts reject a caller key ID that would exceed B2's 10-entry file-info limit before sending the request.
- Concurrent bounded reads stop allocating download chunks when the requested range is exhausted, rather than requesting data past its end.
- Exact-boundary ranged reads now mark a chunk whose size equals the remaining requested length as final. The shared remaining-length accounting is kept under the reader mutex.
- Repeated `(*b2.Object).Attrs` calls preserve cached file metadata, including `LastModified`, without mutating the cache. Each returned `Attrs.Info` is independent; `large_file_sha1` remains in the public map, while `src_last_modified_millis` remains excluded.
- Large-file Writer uploads retain a replacement upload-part endpoint across later chunks; copied and uploaded part replacement safely retain the verified prior size across ambiguous responses. `File.AsLargeFile().CopyPart` no longer panics, and Compose stops buffering promptly when its context is canceled.
- Download-by-ID logging and free-form error details redact file IDs; HTTP status and B2 error codes remain available.
- Downloads by ID and by name request stored bytes without HTTP gzip decompression, preserving the file's content length and SHA1 for gzip-encoded objects.
- Progress status no longer waits on stalled transfer reads; cancellation closes a blocked download body before waiting for its copy. Reader shutdown now waits for initialization, zero-length ranges can be verified, and resumed uploads tolerate non-seekable inputs while clamping known totals to zero.
- `x/b2sim` now accepts seekable multipart uploads with trailing part SHA1 digests, filters bucket types, protects unfinished uploads during bucket deletion, preserves upload metadata casing, groups delimiter listings, uses finish order for version precedence, and escapes download metadata headers.
- Concurrent requests now use a consistent authorization snapshot while a 401 refreshes the account token. Simultaneous 401s share one reauthorization instead of racing to update shared account state.
- SSE-C server-side copies store the destination key's caller-supplied `sse_c_key_id` instead of retaining the source ID; copying to a key without an ID removes the old ID from file info.
- Server-side copy and other non-upload calls now retry HTTP 408, 502 and 504 responses. Upload calls continue to obtain a new upload URL for those responses.
- Debug log messages drop carriage returns and line feeds from string, error and Stringer arguments, so values taken from requests and errors cannot forge extra log lines.
- `Client.ListKeys` now returns each key's capabilities (#220).
- An upload of exactly `Writer.ChunkSize` bytes is now sent as a single file instead of a one-part large file (#221).
- With a non-seekable reader, `Writer.ReadFrom` now returns the context error on cancellation without waiting for a `Read` in progress, and nothing more from the reader is written after `ReadFrom` returns (#229).
- `Writer.ReadFrom` and `io.Copy` now return the recorded large-file upload error instead of masking it with context cancellation (#245).
- Application keys without an expiration timestamp now report a zero expiration time instead of 1970-01-01; callers comparing `Expires()` to 1970-01-01 or `Unix()` to 0 should use `IsZero()` (#244).
- Transport fault injection is race-free and honors the HTTP RoundTripper contract (#176).
- `window.New` handles sub-resolution sizes and panics when a finite window's resolution is <= 0 (#176).
- Virtual folders returned by file listings now report a zero upload timestamp instead of 1970-01-01 (#253).
- `x/b2sim` bucket responses from `b2_list_buckets`, `b2_create_bucket`, and `b2_update_bucket` now include the documented `accountId` and `options` fields (#234).
- `(*Object).Attrs` now reports `Status` for an unfinished large file from `ListUnfinished`; it was `Unknown` because the listing dropped the action.
- Documentation for `(*b2.Bucket).SetNotificationRules` and `(*base.Bucket).SetNotificationRules` now explicitly explains nil/empty clearing and server-managed suspension fields.
- Large-file HEAD lookups and downloads by ID or name no longer fail on 32-bit systems when `Content-Length` exceeds the platform's maximum int. `base.FileReader.ContentLength` saturates at that maximum without limiting the response body; `Object.Attrs` and `Delete` can still resolve the file ID. Reader progress retains the exact 64-bit HEAD length, including at and above the platform int limit (#343).
- `(*b2.Object).Delete` returns an error for a nil `DeleteOption` before sending any request, instead of panicking.
- The `x/b2sim` `Simulator` reports missing-bucket errors recognized by `b2.IsNotExist` when deleting a bucket or listing its files, and uses `cannot_delete_non_empty_bucket` for nonempty deletion.
- `(*b2.Bucket).Compose` documentation distinguishes locally validated sizes from final whole-file copies, whose size limits are enforced by B2 after the large file starts.
- `base.AuthData.AllowedCapabilities` now preserves storage capabilities from `apiInfo.storageApi.allowed.capabilities`, including in saved and restored `b2.AccountInfo` sessions. The in-process `x/b2sim` simulator uses the same documented nesting.

### Security

- Writer retry logs for upload parts retain the error type and HTTP status while omitting server error messages (#199).

[0.10.0]: https://github.com/Backblaze/blazer/compare/v0.9.0...v0.10.0

## [0.9.0] - 2026-10-22

### Added

- `(*b2.Bucket).NotificationRules` and `SetNotificationRules` read and replace bucket event notification rules, including webhook targets.
- `(*b2.Bucket).GetFileInfoByName` looks up metadata for the current visible file with an exact name without downloading its contents. `(*base.Bucket).GetFileInfoByName` is the low-level call.
- `ReplicationConfiguration.AsReplicationDestination` exposes the replication destination's source-to-destination key mapping.
- `(*base.B2).CreateKeyMultiBucket` and the `b2.BucketIDs` `KeyOption` for creating Multi-Bucket Application Keys via `(*b2.Client).CreateKey`.
- `(*b2.Bucket).ID` returns the bucket's B2 ID, which is needed to fill in `ReplicationRules.DestinationBucketID`.
- `(*b2.Bucket).Copy` copies a file server-side with `b2_copy_file`, within a bucket or to another bucket of the account (`CopyToBucket`), optionally a byte range (`CopyRange`) and with replaced metadata (`CopyWithMetadata`). `(*base.Bucket).CopyFile` is the low-level call. A single copy must be under 5 GB; the per-copy Object Lock and encryption parameters are not exposed.
- `b2.BypassGovernance()`, a `DeleteOption` accepted by `(*b2.Object).Delete`, deletes a file version under governance-mode Object Lock retention. The application key needs the `bypassGovernance` capability; compliance-mode retention is never bypassed. `(*base.File).DeleteFileVersion` takes an optional `bypassGovernance` argument for the same purpose.
- Per-file Object Lock. `b2.WithFileRetention` and `b2.WithLegalHold` set retention and legal hold when a `Writer` uploads a file, `(*b2.Object).UpdateFileRetention` and `UpdateFileLegalHold` change them afterwards, and `Attrs.Retention` and `Attrs.LegalHold` report them. Passing a nil `*FileRetention` to `UpdateFileRetention` removes retention (governance mode needs `bypassGovernance`). `FileRetention` takes a `RetentionMode` and a `RetainUntil` time, and is separate from the bucket default `Retention`. `WithAttrsOption` does not copy `Attrs.Retention` or `Attrs.LegalHold`. A `Writer` with `Resume` set refuses to resume an unfinished upload whose retention or legal hold differs from the one requested. `base.UploadFile` and `base.StartLargeFile` accept an optional `base.FileLock`.

### Changed

- The minimum required Go version is now 1.26.10.
- `b2_authorize_account` and the other general API calls now target the B2 Native API v4. `(*base.B2).CreateKey` and `(*b2.Bucket).CreateKey` continue to target the v3 `b2_create_key` endpoint and produce legacy single-bucket keys.
- `base.CreateBucket` takes the bucket's CORS rules and whether Object Lock is enabled as additional arguments.
- A connection error on an upload now names the request, so its message reads `b2_upload_file: 0: connection reset` instead of `b2 error: connection reset`. Connection errors on every other request are unchanged.

### Fixed

- Bucket creation, listing and updates preserve the replication destination configuration as well as the source configuration.
- Failed uploads no longer return their upload URL to the pool for reuse by another writer. Server-supplied `Retry-After` delays are capped at 30 seconds.
- Certificate verification failures, including rejection of compromised CAs, now stop retries and preserve the original error instead of being treated as retryable connection errors or prompting a new upload URL.
- `Client.NewBucket` sends `BucketAttrs.CORSRules` and `BucketAttrs.FileLockEnabled` in `b2_create_bucket`; they were dropped.
- `Bucket.Attrs` and the buckets returned by `Client.Bucket` and `Client.ListBuckets` report `CORSRules` and `FileLockEnabled`.
- `Bucket.Update` sends `CORSRule.Name` as `corsRuleName`; it was dropped.
- `Bucket.Update` sends `BucketAttrs.ReplicationConfig` when it is set, and no longer panics when it is nil on a bucket that already has a replication configuration. The check used the bucket's cached configuration instead of the caller's.
- `Writer` no longer hangs when a part of a large upload fails permanently. `sendChunk` held a read lock while blocked handing the next chunk to a worker, so the failing worker could never record the error and cancel the upload. `Write` also no longer takes its read lock recursively, which could deadlock against a waiting `Close`.
- `Writer` upload workers exit when the writer's context is cancelled. With `ConcurrentUploads` of 2 or more, a failed part cancelled the context, `Close` then returned before signalling the remaining workers, and an idle worker goroutine stayed parked forever.
- `ObjectIterator` no longer ends a listing early, with no error, when a page is empty or all its entries are filtered out (hidden listings skip unfinished uploads) but B2 returned a cursor to the next page. This affected `List`, `ListHidden` and `ListUnfinished`. If a backend keeps returning an empty page with the same cursor, the iterator now stops with an error instead of repeating the request.
- `Bucket.Attrs`, and the buckets returned by `Client.Bucket`, `Client.NewBucket` and `Client.ListBuckets`, report `DefaultRetention`. It was decoded only by `Update` and never mapped to `BucketAttrs`.
- `Bucket.Update` sends the default retention, and `fileLockEnabled`, only when the caller changes them. The cached values are what go on the wire, and setting either needs the `writeBucketRetentions` capability, so an update of unrelated settings on an Object Lock bucket would otherwise be refused for keys without that capability. `Update` also no longer panics when B2 reports a default retention without a period unit.
- A file made by `Copy` has the action `copy`; `(*b2.Object).Delete`, `Attrs` and `List` with `ListHidden` now treat it like an uploaded file.
- `Bucket.Update` clears the bucket's info when `BucketAttrs.Info` is an empty, non-nil map, as its documentation says. The empty map was dropped from the request, so the existing info was kept and the call reported success. A nil `Info` still leaves the info unchanged.
- `Bucket.Update` no longer fails with `Unsupported bucket default server-side encryption mode` on a bucket that has no default encryption. B2 reports that case as a null mode, which was cached as an empty setting and re-sent on every update. It is now treated as no default, so `Attrs` reports none and updates leave the field out.
- `Bucket.Update` removes every lifecycle rule when `BucketAttrs.LifecycleRules` is an empty, non-nil slice, as its documentation says. The empty list was dropped from the request, so the rules stayed and the call reported success.
- `Bucket.Update` sends lifecycle rules only when `BucketAttrs.LifecycleRules` is set. It used to re-send the cached rules on every update, which replaced rules changed elsewhere, such as through the S3-compatible API, since B2 replaces them wholesale. A nil `LifecycleRules` now leaves them untouched.
- Uploads fetch a new upload URL after a connection-level failure (a reset or a stall), as B2's Integration Checklist asks, instead of retrying the broken one. An HTTP response such as 429 or 503 with `Retry-After` still waits as told and retries the same URL.
- A `Reader` gives up on a chunk that keeps arriving truncated after 21 attempts (the first plus the 20 retries allowed for any download), with an error that wraps `io.ErrUnexpectedEOF`. It used to retry forever, until the caller's context ended. The wait between attempts grows from 1 ms to a cap of about 16 s, so an interruption of around two minutes is still survived.

[0.9.0]: https://github.com/Backblaze/blazer/compare/v0.8.0...v0.9.0

## [0.8.0] - 2026-09-15

### Changed

- B2 now applies SSE-B2 (AES256) as the default server-side encryption to every bucket. A nil `BucketAttrs.DefaultServerSideEncryption` passed to `Client.NewBucket` or `Bucket.Update` leaves the default to the server; any other setting than SSE-B2 with AES256 is rejected before the request is sent.
- `base.CreateBucket` takes the bucket's default server-side encryption as an additional argument.

### Fixed

- `base.Bucket.DefaultServerSideEncryption` is decoded from the `{isClientAuthorizedToRead, value}` object B2 returns. It was read as a flat object, so `Bucket.Update` sent an empty encryption mode and failed.

### Deprecated

- `b2.DefaultServerSideEncryption()`, use `b2.SSEB2WithAES256()` instead.

[0.8.0]: https://github.com/Backblaze/blazer/compare/v0.7.2...v0.8.0

## [0.7.2] - 2025-01-23

### Changed

- Removed unused `bonfire` and `pyre` code, greatly reducing the number of dependencies and the amount of work required to keep them up to date.
- Restructured the repository as a [Go workspace](https://go.dev/ref/mod#workspaces) and moved the sole remaining third-party dependency into `bin/b2keys`.

[0.7.2]: https://github.com/Backblaze/blazer/compare/v0.7.1...v0.7.2

## [0.7.1] - 2024-10-07

### Fixed

- The `cleanup` utility now deletes the `replication-target` test bucket

### Changed

- Bumped dependencies in response to dependabot alerts

[0.7.1]: https://github.com/Backblaze/blazer/compare/v0.7.0...v0.7.1

## [0.7.0] - 2024-10-04

### Added

- Can now specify bucket type when listing buckets
- Can now get and set default encryption configuration, object lock, CORS rules, etc on bucket ([djenriquez](https://github.com/djenriquez))
- Can now get the S3 API URL from a bucket ([celskeggs](https://github.com/celskeggs))

### Fixed

- The `cleanup` utility now successfully deletes test files and buckets after an interrupted test run

### Changed

- Migrated to Backblaze B2 Native API v3 

[0.7.0]: https://github.com/Backblaze/blazer/compare/v0.6.1...v0.7.0

## [0.6.1] - 2023-10-16

### Added

- `go.mod` file, license report ([tzeejay](https://github.com/tzeejay))

### Fixed

- Resolve import errors ([tzeejay](https://github.com/tzeejay))

### Changed

- Reference license report from README ([tzeejay](https://github.com/tzeejay))

[0.6.1]: https://github.com/Backblaze/blazer/compare/v0.6.0...v0.6.1

## [0.6.0] - 2023-09-26

Tagged initial version at https://github.com/Backblaze/blazer

[0.6.0]: https://github.com/Backblaze/blazer/compare/v0.5.3...v0.6.0
