# Blazer

[![CI](https://github.com/Backblaze/blazer/actions/workflows/ci.yml/badge.svg)](https://github.com/Backblaze/blazer/actions/workflows/ci.yml)
[![CodeQL](https://github.com/Backblaze/blazer/actions/workflows/codeql.yml/badge.svg)](https://github.com/Backblaze/blazer/actions/workflows/codeql.yml)
[![Security checks](https://github.com/Backblaze/blazer/actions/workflows/security.yml/badge.svg)](https://github.com/Backblaze/blazer/actions/workflows/security.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/Backblaze/blazer/b2.svg)](https://pkg.go.dev/github.com/Backblaze/blazer/b2)
[![Coverage](https://img.shields.io/endpoint?url=https%3A%2F%2Fraw.githubusercontent.com%2FBackblaze%2Fblazer%2Fbadges%2Fcoverage.json)](https://github.com/Backblaze/blazer/actions/workflows/ci.yml?query=branch%3Amain)
[![Latest release](https://img.shields.io/github/v/release/Backblaze/blazer?sort=semver&display_name=tag&color=blue)](https://github.com/Backblaze/blazer/releases/latest)
[![Go version](https://img.shields.io/github/go-mod/go-version/Backblaze/blazer?logo=go&logoColor=white)](go.mod)
[![License: Apache 2.0](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Dependencies](https://img.shields.io/badge/dependencies-stdlib%20only-brightgreen)](b2/licenses.csv)

Blazer is a Go client library for [Backblaze B2 Cloud Storage](https://www.backblaze.com/cloud-storage),
maintained by Backblaze.

```go
import "github.com/Backblaze/blazer/b2"
```

Blazer targets the B2 Native API. Unless you specifically need the Native API, you should use the
[MinIO Go Client SDK](https://github.com/minio/minio-go) with B2's
[S3 Compatible API](https://www.backblaze.com/docs/cloud-storage-s3-compatible-api).

_Many thanks to Toby Burress ([kurin](https://github.com/kurin)) for creating and maintaining Blazer for its
first six years._

**Contents**

- [Features](#features)
- [Install](#install)
- [Packages](#packages)
- [Examples](#examples)
- [Testing resilience with B2 test mode](#testing-resilience-with-b2-test-mode)
- [Contributing](#contributing)
- [Security](#security)
- [Licenses](#licenses)
- [Backblaze ecosystem](#backblaze-ecosystem)

## Features

- **Streaming I/O.** `Writer` and `Reader` implement `io.Writer` and `io.Reader`; large files are split
  into parts automatically, with concurrent part uploads and ranged, concurrent downloads.
- **Large files.** Resumable uploads, server-side copies, and `Bucket.Compose` to build a file from
  uploaded and copied parts.
- **Encryption.** Per-request SSE-B2 and SSE-C (customer-supplied keys) for uploads, downloads, metadata
  lookups and copies.
- **Object Lock.** File retention and legal hold on upload.
- **Buckets and keys.** Create, update and delete buckets; create and list application keys; issue
  download authorization tokens for a prefix.
- **Replication and notifications.** `SetupReplication` and `ReplicationStatus`; event-notification rules,
  plus `VerifySignature` and `ParseEvent` for inbound webhooks.
- **Sync.** `b2/sync` pushes a local directory tree into a B2 prefix, with dry runs and per-file results.
- **Progress and status.** Byte-level progress callbacks for uploads and downloads, and a `Client.Status`
  snapshot you can serve over HTTP.
- **Reliable by default.** Retries with backoff, `Retry-After` support and automatic reauthorization;
  optional session persistence with `WithAccountInfo`.
- **No third-party dependencies.** The `b2` package imports only the Go standard library.

## Install

```bash
go get github.com/Backblaze/blazer/b2
```

Blazer supports the Go version in [`go.mod`](go.mod) and newer; CI tests it on the current stable Go
release as well. Releases follow [semantic versioning](https://semver.org) and are listed in the
[changelog](CHANGELOG.md).

You need a B2 [application key](https://www.backblaze.com/docs/cloud-storage-application-keys). Give it
only the capabilities and buckets your program uses.

## Packages

| Package | Purpose |
| --- | --- |
| [`b2`](https://pkg.go.dev/github.com/Backblaze/blazer/b2) | The client library. Start here. |
| [`b2/sync`](https://pkg.go.dev/github.com/Backblaze/blazer/b2/sync) | Sync a local directory tree into a B2 prefix. |
| [`base`](https://pkg.go.dev/github.com/Backblaze/blazer/base) | A thin, low-level mapping of the B2 Native API calls that `b2` is built on. |
| [`x/...`](https://pkg.go.dev/github.com/Backblaze/blazer/x) | Experimental packages; their API may change between minor releases. |
| [`bin/b2keys`](bin/b2keys) | A small command-line tool for creating application keys (a separate module). |

## Examples

### Getting started

```go
import "os"

id := os.Getenv("B2_APPLICATION_KEY_ID")
key := os.Getenv("B2_APPLICATION_KEY")

ctx := context.Background()

// b2_authorize_account
b2, err := b2.NewClient(ctx, id, key)
if err != nil {
	log.Fatalln(err)
}

buckets, err := b2.ListBuckets(ctx)
if err != nil {
	log.Fatalln(err)
}
```

### Copy a file into B2

```go
func copyFile(ctx context.Context, bucket *b2.Bucket, src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	obj := bucket.Object(dst)
	w := obj.NewWriter(ctx)
	if _, err := io.Copy(w, f); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}
```

If the file is smaller than the writer's `ChunkSize` (100 MB by default), Blazer buffers it and sends it
with `b2_upload_file`. Larger files are uploaded in `ChunkSize` parts with B2's large file API.

### Copy a file into B2, with multiple concurrent uploads

Uploading a large file with multiple HTTP connections is simple:

```go
func copyFile(ctx context.Context, bucket *b2.Bucket, writers int, src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bucket.Object(dst).NewWriter(ctx)
	w.ConcurrentUploads = writers
	if _, err := io.Copy(w, f); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}
```

This uploads up to `writers` parts at a time. `ChunkSize` sets the part size: 100 MB by default, at least
5 MB, at most 5 GB.

### Download a file from B2

Downloading is as simple as uploading:

```go
func downloadFile(ctx context.Context, bucket *b2.Bucket, downloads int, src, dst string) error {
	r := bucket.Object(src).NewReader(ctx)
	defer r.Close()

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	r.ConcurrentDownloads = downloads
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
```

### List all objects in a bucket

```go
func printObjects(ctx context.Context, bucket *b2.Bucket) error {
	iterator := bucket.List(ctx)
	for iterator.Next() {
		fmt.Println(iterator.Object())
	}
	return iterator.Err()
}
```

### Report upload progress

```go
w := bucket.Object(dst).NewWriter(ctx, b2.WithProgress(func(done, total int64) {
	log.Printf("%d of %d bytes", done, total)
}))
```

### Encrypt with a customer-supplied key (SSE-C)

```go
key, err := b2.NewSSECKey(secret) // 32 bytes; keep it safe, B2 does not store it
if err != nil {
	return err
}
w := bucket.Object("private.bin").NewWriter(ctx, b2.WithSSECKey(key))
// ... write and close w, then read it back with the same key:
r := bucket.Object("private.bin").NewReader(ctx, key)
```

Use `b2.WithSSEB2("AES256")` instead for B2-managed encryption on a single upload.

### Sync a local directory

```go
import "github.com/Backblaze/blazer/b2/sync"

res, err := sync.Sync(ctx, bucket, "backups/", "./data", sync.WithConcurrency(4))
if err != nil {
	return err
}
log.Printf("uploaded %d, skipped %d, failed %d", len(res.Uploaded), len(res.Skipped), len(res.Failed))
```

Add `sync.WithDryRun()` to see what would be uploaded without writing anything.

### Grant temporary auth to a file

Say you have a number of files in a private bucket, and you want to allow other
people to download some files.  This is possible to do by issuing a temporary
authorization token for the prefix of the files you want to share.

```go
token, err := bucket.AuthToken(ctx, "photos", time.Hour)
```

If successful, `token` is then an authorization token valid for one hour, which
can be set in HTTP GET requests.

The hostname to use when downloading files via HTTP is account-specific and can
be found via the BaseURL method:

```go
base := bucket.BaseURL()
```

More runnable code lives in [`examples/`](examples), and every exported identifier is documented on
[pkg.go.dev](https://pkg.go.dev/github.com/Backblaze/blazer/b2).

## Testing resilience with B2 test mode

B2 provides documented "test mode" hooks (sent as the `X-Bz-Test-Mode` request
header) that ask the service to inject controlled failures, so you can exercise
your retry and error-handling paths without waiting for real faults. blazer
exposes each one as a client option:

```go
// Ask B2 to fail some uploads so you can verify your upload retries.
client, err := b2.NewClient(ctx, id, key, b2.FailSomeUploads())
```

| Option | `X-Bz-Test-Mode` value | Effect |
| --- | --- | --- |
| `b2.FailSomeUploads()` | `fail_some_uploads` | B2 fails some uploads, exercising upload retries. |
| `b2.ExpireSomeAuthTokens()` | `expire_some_account_authorization_tokens` | B2 expires account authorization tokens frequently, exercising re-authentication. |
| `b2.ForceCapExceeded()` | `force_cap_exceeded` | B2 behaves as if the account storage cap were exceeded. Backblaze documents it for upload- and download-related calls. |

Backblaze documents each value on its own and does not say whether several can be
combined. Enabling more than one option sends one `X-Bz-Test-Mode` header line per
option. blazer's live integration tests combine `b2.FailSomeUploads()` and
`b2.ExpireSomeAuthTokens()`, but that does not establish that every combination is
supported by the B2 service, so prefer one option per client unless you have
verified the combination you need.

The header is set when the client is created and sent with every request that
client makes, including authorization and downloads. There is no way to turn it
off afterwards, so create a separate client for tests and leave these options off
in production. See Backblaze's
[Integration Checklist](https://www.backblaze.com/docs/cloud-storage-integration-checklist)
for the authoritative behavior.

## Contributing

Issues and pull requests are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a PR: it covers
`make ci` (the same checks CI runs), changelog entries with [changie](https://changie.dev), and the review
process in [REVIEW.md](REVIEW.md). Contributor references, including the B2 behaviors Blazer must get right,
are in [`docs/`](docs). Coding agents start from [AGENTS.md](AGENTS.md). Releases follow
[RELEASE.md](RELEASE.md).

Please follow the [Code of Conduct](CODE_OF_CONDUCT.md).

### Contributors

Thanks to everyone who has contributed to Blazer.

[![Contributors](https://contrib.rocks/image?repo=Backblaze/blazer)](https://github.com/Backblaze/blazer/graphs/contributors)

## Security

Do not open public issues for vulnerabilities. See [SECURITY.md](SECURITY.md) for how to report one.

## Licenses

Blazer is licensed under the [Apache License 2.0](LICENSE).

The b2 package does not consume any third-party packages; it depends only on the Go standard library and
code within the `blazer` repository. A report of used licenses is in [`b2/licenses.csv`](b2/licenses.csv),
generated with [go-licenses](https://github.com/google/go-licenses). Double-check it yourself if this is a
concern: it can become stale as the code changes.

## Backblaze ecosystem

- **[b2-sdk-python](https://github.com/Backblaze/b2-sdk-python)** — the Python SDK for B2, and the
  reference implementation Blazer is checked against.
- **[B2 Command Line Tool](https://github.com/Backblaze/B2_Command_Line_Tool)** — the `b2` CLI.
- **[b2-sdk-typescript](https://github.com/backblaze-labs/b2-sdk-typescript)** — a TypeScript and
  JavaScript SDK for B2, incubating in Backblaze Labs.
- **[b2-mcp](https://github.com/backblaze-labs/b2-mcp)** — a Model Context Protocol server for B2.
- **[b2-action](https://github.com/backblaze-labs/b2-action)** — a GitHub Action for B2.
