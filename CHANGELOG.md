# Changelog

Going forward from v0.6.0 (the first new version after the move to https://github.com/Backblaze/blazer), all notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `b2.SetupReplication` merges caller-owned source and destination key IDs into bucket replication configurations, and `b2.ReplicationStatus` reads configured rules. Both reject unreadable existing configuration; setup rejects conflicting options and key mappings.
- `b2/sync.Sync` pushes a local directory tree into a B2 prefix with configurable comparison, concurrency, dry-run reporting, and per-file failure results. It follows a symlinked root but not symlinks below it; changed large files are re-uploaded rather than resumed.
- `b2.WithAccountInfo` allows a client to reuse an authorized session through the default in-memory store or an opt-in `AccountInfoFile`. The file backend stores tokens and endpoints atomically with private POSIX permissions and never stores the application key. On Windows, session privacy depends on the directory ACLs; place the file under the user's profile directory, such as `os.UserConfigDir()`. A missing, corrupt or oversized cached session triggers normal authorization and replacement; oversized reads remain bounded.
- Per-request SSE-C keys for uploads, downloads, metadata lookup, and server-side copies. Large-file uploads send the key on the start request and every part; keys cannot be configured as client writer defaults. `NewSSECKeyWithID` accepts an optional caller-chosen key identifier, stored as `sse_c_key_id` in large-file info for resume. Blazer adds no key-derived fingerprint to metadata. Resume rejects missing or different IDs, and an unkeyed writer cannot resume an SSE-C upload. Callers must not reuse an ID for another key: B2 rejects wrong keys on uploaded parts, but cannot check a key when every part is skipped.
- `b2.VerifySignature` checks the HMAC-SHA256 signature of a raw B2 event-notification body, and `b2.ParseEvent` decodes its events. The helpers reject missing or malformed signatures, empty signing secrets, and bodies larger than 1 MiB; callers must cap HTTP reads before passing a body to them and deduplicate at-least-once deliveries by event ID.
- `b2.WithProgress` reports byte-level upload progress, and `b2.Reader.Progress` reports byte-level download progress. Callbacks are serialized off transfer locks and finish with a terminal value; known upload sizes and finite download ranges report their actual totals.
- `(*b2.Bucket).Compose` builds a large file from ordered uploaded and server-side copied parts, using `b2_copy_part`. It validates part sizes and copy ranges before starting, rejects overlapping ranges from the same source, and cancels an unfinished file if a part fails, even after caller cancellation.
- `(*b2.Bucket).DownloadFileByID` downloads the exact file version identified by its B2 file ID, including historical versions and optional per-request SSE-C keys; `(*base.Bucket).DownloadFileByID` provides low-level GET, HEAD, and range requests.
- `b2.WithSSEB2("AES256")` requests SSE-B2 for one upload without changing the bucket default. Resumed uploads validate the existing encryption mode and algorithm; SSE-B2 and SSE-C writer options cannot be combined.
- `(*b2.Client).NewBucket` can set a replication configuration at bucket creation; `(*base.B2).CreateBucket` accepts the replication configuration as a new parameter.

### Changed

- Simple SSE-C uploads made with `NewSSECKeyWithID` now store the caller ID as `sse_c_key_id` in file info. This adds one file-info entry and counts toward B2's 10-entry limit. They now fail before any request when caller file info conflicts with the key ID, sets an ID for a key without one, or would exceed that limit. Previously those file-info combinations were sent to B2 as-is.

### Fixed

- A rejected bucket replication update no longer leaves the attempted configuration in the local bucket cache, where a later unrelated update could resend it.
- `b2/sync` treats an unavailable destination SHA1 as changed, stops hashing after context cancellation, rejects files swapped for symlinks after walking, cancels an upload if the streamed bytes differ from the walk-time SHA1, and reports a destination object whose attributes cannot be read only as a failure of the local file that maps to it.
- `AccountInfoFile` rejects cached sessions read from non-private POSIX paths, incomplete sessions reauthorize and refresh the cache, and verbose authorization-response logging omits the response body entirely.
- Verbose B2 wire logging redacts authorization tokens, newly created application keys, and SSE-C key fields; logs header names without values and omits malformed or failed response bodies and untrusted transport error text. This fixes credential exposure in logs (#149).
- SSE-C large-file starts reject a caller key ID that would exceed B2's 10-entry file-info limit before sending the request.
- Concurrent bounded reads stop allocating download chunks when the requested range is exhausted, rather than requesting data past its end. `NewRangeReader(ctx, offset, 0)` now returns zero bytes instead of reading the rest of the object.
- Large-file Writer uploads retain a replacement upload-part endpoint across later chunks; copied and uploaded part replacement safely retain the verified prior size across ambiguous responses. `File.AsLargeFile().CopyPart` no longer panics, and Compose stops buffering promptly when its context is canceled.
- Download response lengths that cannot fit in the platform's `int` are rejected before conversion, including on 32-bit systems. Download-by-ID logging and free-form error details redact file IDs; HTTP status and B2 error codes remain available.
- Downloads by ID and by name request stored bytes without HTTP gzip decompression, preserving the file's content length and SHA1 for gzip-encoded objects.
- Repeated `(*b2.Object).Attrs` calls preserve cached file metadata, including `LastModified`, without mutating the cache. Each returned `Attrs.Info` is independent; `large_file_sha1` remains in the public map, while `src_last_modified_millis` remains excluded.
- Progress status no longer waits on stalled transfer reads; cancellation closes a blocked download body before waiting for its copy. Reader shutdown now waits for initialization, zero-length ranges can be verified, and resumed uploads tolerate non-seekable inputs while clamping known totals to zero.
- Exact-boundary ranged reads now mark a chunk whose size equals the remaining requested length as final. The shared remaining-length accounting is kept under the reader mutex.

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

## [0.8.0] - 2026-09-15

### Changed

- B2 now applies SSE-B2 (AES256) as the default server-side encryption to every bucket. A nil `BucketAttrs.DefaultServerSideEncryption` passed to `Client.NewBucket` or `Bucket.Update` leaves the default to the server; any other setting than SSE-B2 with AES256 is rejected before the request is sent.
- `base.CreateBucket` takes the bucket's default server-side encryption as an additional argument.

### Fixed

- `base.Bucket.DefaultServerSideEncryption` is decoded from the `{isClientAuthorizedToRead, value}` object B2 returns. It was read as a flat object, so `Bucket.Update` sent an empty encryption mode and failed.

### Deprecated

- `b2.DefaultServerSideEncryption()`, use `b2.SSEB2WithAES256()` instead.

## [0.7.2] - 2025-01-23

### Changed

- Removed unused `bonfire` and `pyre` code, greatly reducing the number of dependencies and the amount of work required to keep them up to date.
- Restructured the repository as a [Go workspace](https://go.dev/ref/mod#workspaces) and moved the sole remaining third-party dependency into `bin/b2keys`.

## [0.7.1] - 2024-10-07

### Fixed

- The `cleanup` utility now deletes the `replication-target` test bucket

### Changed

- Bumped dependencies in response to dependabot alerts

## [0.7.0] - 2024-10-04

### Added

- Can now specify bucket type when listing buckets
- Can now get and set default encryption configuration, object lock, CORS rules, etc on bucket ([djenriquez](https://github.com/djenriquez))
- Can now get the S3 API URL from a bucket ([celskeggs](https://github.com/celskeggs))

### Fixed

- The `cleanup` utility now successfully deletes test files and buckets after an interrupted test run

### Changed

- Migrated to Backblaze B2 Native API v3 

## [0.6.1] - 2023-10-16

### Added

- `go.mod` file, license report ([tzeejay](https://github.com/tzeejay))

### Fixed

- Resolve import errors ([tzeejay](https://github.com/tzeejay))

### Changed

- Reference license report from README ([tzeejay](https://github.com/tzeejay))

## [0.6.0] - 2023-09-26

Tagged initial version at https://github.com/Backblaze/blazer

[unreleased]: https://github.com/Backblaze/blazer/compare/v0.9.0...HEAD
[0.9.0]: https://github.com/Backblaze/blazer/compare/v0.8.0...v0.9.0
[0.6.1]: https://github.com/Backblaze/blazer/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/Backblaze/blazer/compare/v0.5.3...v0.6.0
