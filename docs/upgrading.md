# Upgrading Blazer

Blazer follows semantic versioning. Before 1.0, a minor release may include breaking
changes. This guide lists **only what callers must change** for each release, with
before/after code. [`CHANGELOG.md`](../CHANGELOG.md) remains the full record; every item
here corresponds to a `**Breaking:**` changelog entry.

Covered releases: v0.10.0 and v0.9.0.

## Upgrading to v0.10.0

v0.10.0 adds the parity capabilities and includes twelve breaking changes. **Two require a
source change** (they cause compile errors); the rest change runtime behavior for specific
call patterns — mostly turning a previously silent, risky behavior into an explicit error
("fail closed"). If you do not use the call patterns below, nothing changes for you.

### Source changes required

#### `WriterOption` is now an interface

`WriterOption` changed from `func(*Writer)` to an interface so client defaults can be
validated without executing callbacks. Existing `With…` calls are unchanged; only a custom
bare-function option needs wrapping, and option values are no longer directly callable.

```go
// Before
opt := func(w *b2.Writer) { w.ChunkSize = 10 << 20 }
w := obj.NewWriter(ctx, opt)

// After
opt := b2.WriterOptionFunc(func(w *b2.Writer) { w.ChunkSize = 10 << 20 })
w := obj.NewWriter(ctx, opt)
```

Compose options in a `[]b2.WriterOption` passed to `NewWriter` or `DefaultWriterOptions`
rather than calling an option value yourself.

#### `Object.NewReader`, `NewRangeReader`, and `Attrs` take variadic SSE-C keys

These methods gained a trailing `keys ...*SSECKey` parameter. **Direct calls compile
unchanged.** Only uses as a function value with the old signature break.

```go
// Before — taking the method as a function value
var read func(context.Context) *b2.Reader = obj.NewReader

// After — wrap in a closure, or pass keys through
read := func(ctx context.Context) *b2.Reader { return obj.NewReader(ctx) }
```

### Behavior changes (fail closed)

These keep the same signatures; a call that used to succeed silently can now return an
error. In each case the old behavior was the surprising one.

#### `CopyToBucket(nil)` now errors

```go
// Before: a nil destination silently copied into the receiver bucket.
// Now: Copy returns an error before sending any request.
_, err := bucket.Copy(ctx, srcID, dstName, b2.CopyToBucket(dst)) // dst must be non-nil
// To copy into the receiver bucket, omit CopyToBucket entirely.
```

#### Empty `BucketIDs` is rejected

```go
// Before: CreateKey(..., BucketIDs()) silently created an account-wide key.
// Now: Client.CreateKey returns b2.ErrInvalidBucketIDs; Bucket.CreateKey returns
//      b2.ErrBucketIDsOnBucket. No request is sent.
key, err := client.CreateKey(ctx, name, b2.BucketIDs(id1, id2)) // non-empty IDs
// For an intentional account-wide key, omit BucketIDs.
```

#### Invalid key and download-authorization lifetimes are rejected

`base.ErrInvalidDuration` is now returned for negative lifetimes, positive lifetimes below
one second, and lifetimes of 1000 days or more. This covers `b2.Lifetime`, `b2.Deadline`,
`Bucket.AuthToken`, `Object.AuthURL`, and the `base` key/authorization calls.

```go
// Use 0 for a non-expiring key, or ≥ 1s and < 1000 days. Download authorization
// requires 1 to 604800 seconds.
key, err := client.CreateKey(ctx, name, b2.Lifetime(24*time.Hour))
```

#### Ranged downloads validate the response

Ranged `DownloadFile*` calls now return `base.ErrInvalidRangeResponse` for a non-conforming
response (HTTP 200 at a nonzero offset, an overlong 200/206 body, a chunked response without
`Content-Length`, or a missing/invalid `Content-Range` on 206). This usually means a proxy
or server is not honoring `Range`. Handle the error and ensure your transport passes `Range`
requests through unchanged.

#### Replication on bucket create and update

- `(*b2.Client).NewBucket` now applies `attrs.ReplicationConfig` when it creates a bucket,
  as documented. If you build `BucketAttrs` from another bucket's `Attrs()`, clear
  `ReplicationConfig` when you do not want its rules copied.
- `(*b2.Bucket).Update` omits replication settings when `BucketAttrs.ReplicationConfig` is
  nil (previously it could resend the cached configuration). Use nil to leave replication
  unchanged; supply role data with normal bucket-update and `writeBucketReplications`
  permissions to change it. Partial updates that preserve an unspecified role also need
  `readBucketReplications`.
- `(*base.B2).CreateBucket` takes the replication configuration as a new parameter; pass
  `nil` to keep the previous behavior.

```go
// Copying attrs from another bucket but not its replication:
attrs, err := src.Attrs(ctx)
if err != nil {
	return err
}
attrs.ReplicationConfig = nil
_, err = client.NewBucket(ctx, "new-bucket", attrs)
```

#### `sync`: default comparison and platform support

- The default comparison and `CompareModTime` now use modification times alone, at
  millisecond resolution. To detect size or content changes when times are unchanged, pass
  `WithCompare(CompareSize)` or `WithCompare(CompareSHA1)`.
- `Sync` supports Linux, macOS, and Windows. On other targets it returns an error wrapping
  `errors.ErrUnsupported` (with `GOOS/GOARCH`) before touching the filesystem or B2; those
  targets still build.

```go
// Before: the default comparison also considered size.
// After: ask for size or content detection explicitly.
res, err := b2sync.Sync(ctx, bucket, "prefix/", "/local/root",
    b2sync.WithCompare(b2sync.CompareSHA1))
```

#### `NewRangeReader` with length 0 reads nothing

```go
// Before: length 0 read the rest of the object.
// After: length 0 returns no bytes and makes no request. Use -1 for the remainder.
r := obj.NewRangeReader(ctx, offset, -1)
```

#### Multiple `FileLock` arguments are rejected

`(*base.URL).UploadFile` and `(*base.Bucket).StartLargeFile` (and their SSE-C variants) now
reject more than one `base.FileLock` instead of silently ignoring the extras. Combine
retention and legal hold in a single `FileLock`; zero or one argument works as before.

#### `Writer.Close` waits for in-flight work

`Writer.Close` now waits for in-flight part uploads and, with `WithCancelOnError`, for
`b2_cancel_large_file` and `errf` to finish. Keep `errf` prompt and make it return without
calling `Close` or waiting on the goroutine that calls `Close` (either deadlocks). Bound the
cancellation RPC with a deadline on the context returned by `ctxf`; that deadline does not
bound `errf`.

### Logging changes

- **Upload retry log text changed.** Verbose upload-part retry logs now record the error
  type and HTTP status without the server's error message. Code matching the old log text
  should match on the status/type instead.

## Upgrading to v0.9.0

- **Go 1.26.10 floor.** v0.9.0 requires a patched Go toolchain; build with Go 1.26.10 or
  newer.
- **`(*base.B2).CreateBucket` gained arguments.** Low-level callers must pass the new
  parameters; high-level `b2` package users are unaffected.
