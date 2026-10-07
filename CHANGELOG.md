# Changelog

Going forward from v0.6.0 (the first new version after the move to https://github.com/Backblaze/blazer), all notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `(*base.B2).CreateKeyMultiBucket` and the `b2.BucketIDs` `KeyOption` for creating Multi-Bucket Application Keys via `(*b2.Client).CreateKey`.
- `(*b2.Bucket).ID` returns the bucket's B2 ID, which is needed to fill in `ReplicationRules.DestinationBucketID`.
- `b2.BypassGovernance()`, a `DeleteOption` accepted by `(*b2.Object).Delete`, deletes a file version under governance-mode Object Lock retention. The application key needs the `bypassGovernance` capability; compliance-mode retention is never bypassed. `(*base.File).DeleteFileVersion` takes an optional `bypassGovernance` argument for the same purpose.

### Changed

- `b2_authorize_account` and the other general API calls now target the B2 Native API v4. `(*base.B2).CreateKey` and `(*b2.Bucket).CreateKey` continue to target the v3 `b2_create_key` endpoint and produce legacy single-bucket keys.
- `base.CreateBucket` takes the bucket's CORS rules and whether Object Lock is enabled as additional arguments.

### Fixed

- `Client.NewBucket` sends `BucketAttrs.CORSRules` and `BucketAttrs.FileLockEnabled` in `b2_create_bucket`; they were dropped.
- `Bucket.Attrs` and the buckets returned by `Client.Bucket` and `Client.ListBuckets` report `CORSRules` and `FileLockEnabled`.
- `Bucket.Update` sends `CORSRule.Name` as `corsRuleName`; it was dropped.
- `Bucket.Update` sends `BucketAttrs.ReplicationConfig` when it is set, and no longer panics when it is nil on a bucket that already has a replication configuration. The check used the bucket's cached configuration instead of the caller's.
- `Writer` no longer hangs when a part of a large upload fails permanently. `sendChunk` held a read lock while blocked handing the next chunk to a worker, so the failing worker could never record the error and cancel the upload. `Write` also no longer takes its read lock recursively, which could deadlock against a waiting `Close`.
- `Writer` upload workers exit when the writer's context is cancelled. With `ConcurrentUploads` of 2 or more, a failed part cancelled the context, `Close` then returned before signalling the remaining workers, and an idle worker goroutine stayed parked forever.
- `ObjectIterator` no longer ends a listing early, with no error, when a page is empty or all its entries are filtered out (hidden listings skip unfinished uploads) but B2 returned a cursor to the next page. This affected `List`, `ListHidden` and `ListUnfinished`. If a backend keeps returning an empty page with the same cursor, the iterator now stops with an error instead of repeating the request.
- `Bucket.Update` removes every lifecycle rule when `BucketAttrs.LifecycleRules` is an empty, non-nil slice, as its documentation says. The empty list was dropped from the request, so the rules stayed and the call reported success.
- `Bucket.Update` sends lifecycle rules only when `BucketAttrs.LifecycleRules` is set. It used to re-send the cached rules on every update, which replaced rules changed elsewhere, such as through the S3-compatible API, since B2 replaces them wholesale. A nil `LifecycleRules` now leaves them untouched.

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

[unreleased]: https://github.com/Backblaze/blazer/compare/v0.6.1...HEAD
[0.6.1]: https://github.com/Backblaze/blazer/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/Backblaze/blazer/compare/v0.5.3...v0.6.0
