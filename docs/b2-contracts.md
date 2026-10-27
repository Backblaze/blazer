# B2 contracts

Facts about B2 behavior and SDK conventions that blazer has to get right, with the evidence for each.
Read the section for the area you are changing before you start; add to it when a review or a live run
teaches something new. [AGENTS.md](../AGENTS.md) holds the short rules; this file holds the detail.

Each entry says what is true, where it comes from, and the test that pins it in blazer. Evidence kinds:
**docs** (B2 API documentation), **reference** (`b2-sdk-python` or the `b2` CLI, pinned to a commit),
**live** (observed against B2, with the date), **simulator** ([`backblaze-labs/b2-simulator`](https://github.com/backblaze-labs/b2-simulator), pinned by the harness and used by
`.sdkharness/`). When two sources disagree, keep both and say which one blazer follows and why.

## SSE-C keys and identity

- The key bytes are secret; the key MD5 sent on the wire is a fingerprint of the key and is also kept out
  of logs and errors. Redact both in request and response headers and in JSON bodies, matching field names
  after JSON decoding (casing, nesting and escaped names), not by raw text. (#122 review, #159)
- `sse_c_key_id` in file info is an optional label chosen by the caller, separate from the key and its
  MD5. Never derive a stored identifier from the key. reference: [b2sdk `setting.py`][PyEncryption];
  test: `TestSSECResumeRejectsDifferentOrMissingKeyIdentifier`.
- A copy decrypts the source with the source key and encrypts the destination with the destination key.
  When the destination key has a different ID, the destination file info must carry the destination's
  `sse_c_key_id` (b2sdk clones the source info, swaps the ID and uses `REPLACE`). reference:
  [b2sdk `copy_manager.py`][PyCopy]; #159 item 6.
- An explicit `nil` key option is an error, not "no option", and a zero-value `SSECKey` is not a key.
  Both must fail before any request. (#122 review, #159)

## Encryption defaults and resume

- An omitted encryption setting means "use the bucket default"; it is not "no encryption". reference:
  [CLI `console_tool.py`][CLIEncryption].
- Before a resumed large file reuses existing parts, verify its encryption mode, algorithm and key
  identity; when that cannot be verified, refuse to resume. A matching name or matching part hashes is
  not enough. reference: [b2sdk `executor.py`][PyResume]; tests:
  `TestSSEB2ResumeRejectsWrongOrUnverifiableEncryption`, `TestOverriddenSSECResumeUsesEffectiveEncryption`.

## File info written by SDKs

- `large_file_sha1` is SDK-managed file info that must match the uploaded content; B2 does not compute a
  SHA1 for large files. blazer exposes it in `Attrs.Info` as well as in the normalized SHA1. reference:
  [b2sdk `emerger.py`][PySHA]; tests: `TestAttrsPublicMetadataPathsKeepFileInfoIndependent`,
  `TestSyncSHA1LargeFileRoundTrip`.
- `src_last_modified_millis` is the source file's modification time in milliseconds, set by uploads and
  used by sync. The upload timestamp is a different clock. reference: [b2sdk `file_version.py`][PyFileVersion].
- Values in file info are caller data and may be malformed (for example a non-numeric
  `src_last_modified_millis` written by another tool). A bad value fails only the operation on that file.

## Buckets

- `b2_create_bucket` accepts `replicationConfiguration`, and `NewBucket` applies every field of
  `BucketAttrs`, as its godoc says. docs: [b2_create_bucket][CreateBucket]; reference: b2sdk
  `create_bucket(replication=...)`, `b2 bucket create --replication`; test:
  `TestNewBucketReplicationConfigurationWireRoundTrip`.
- A configuration the key is not allowed to read comes back with `isClientAuthorizedToRead: false` and no
  value. That is hidden, not absent: never build a replacement from it. tests:
  `TestNewBucketReplicationConfigurationUnreadable`, `TestSetupReplicationFailsClosedOnUnreadableConfig`.
- The replication helper rejects a rule that would exceed the source bucket's rule limit before it writes
  anything (#137).

## Downloads

- Ask for stored bytes (`Accept-Encoding: identity`). Go decompresses `gzip` transparently, which would
  make the returned bytes, `Content-Length` and SHA1 disagree with what B2 stores. reference: b2sdk
  disables decompression too ([`b2http.py`][PyHTTP]); test: `TestDownloadGzipReturnsStoredBytes`.
- `NewRangeReader(ctx, offset, 0)` reads zero bytes; a negative length reads to the end of the file.

## Retries and authorization

- `Retry-After` is honored as sent by the server, with a bounded number of attempts. Test it as received
  on the wire, not as a mocked duration. reference: [b2sdk `b2http.py`][PyHTTP].
- A failed part upload gets a fresh upload URL, and later parts keep using the new one.
- An expired or invalid token (401) triggers reauthorization; a permission error (403) does not.
  Simultaneous 401s should share one reauthorization (#144). reference: [b2sdk `session.py`][PySession], [`exception.py`][PyErrors].

## Large files

- Parts are numbered 1 to 10000; every part except the last is at least the account's
  `absoluteMinimumPartSize` (5,000,000 bytes on B2); the recommended size comes from authorization. The
  harness simulator uses much smaller defaults for speed, so only its realistic-limits profile proves a
  limit.

## What each kind of test proves

| Surface | Proves | Does not prove |
| --- | --- | --- |
| Fake backend (`b2/b2_test.go`) | `b2` package logic | The request sent on the wire |
| Wire test (`httptest` through `NewClient`) | The exact request and how a response is decoded | That B2 answers that way |
| B2 simulator ([`backblaze-labs/b2-simulator`](https://github.com/backblaze-labs/b2-simulator), through `.sdkharness/`) | Behavior against a B2-shaped server, pinned | Behavior it does not model (encryption keys, retention, replication), or behavior not yet confirmed against live B2 |
| Live B2 | Real behavior on that date and account | Other accounts, regions or later B2 releases |

A check that did not run is **NOT RUN**, never PASS. Say in the PR which surfaces you used.

[PyEncryption]: https://github.com/Backblaze/b2-sdk-python/blob/17a6be766237899f8d807feab4102533085d0683/b2sdk/_internal/encryption/setting.py#L42-L232
[PyCopy]: https://github.com/Backblaze/b2-sdk-python/blob/17a6be766237899f8d807feab4102533085d0683/b2sdk/_internal/transfer/outbound/copy_manager.py#L199-L253
[PyResume]: https://github.com/Backblaze/b2-sdk-python/blob/17a6be766237899f8d807feab4102533085d0683/b2sdk/_internal/transfer/emerge/executor.py#L347-L410
[PySHA]: https://github.com/Backblaze/b2-sdk-python/blob/17a6be766237899f8d807feab4102533085d0683/b2sdk/_internal/transfer/emerge/emerger.py#L48-L69
[PyFileVersion]: https://github.com/Backblaze/b2-sdk-python/blob/17a6be766237899f8d807feab4102533085d0683/b2sdk/_internal/file_version.py#L84-L103
[PyHTTP]: https://github.com/Backblaze/b2-sdk-python/blob/17a6be766237899f8d807feab4102533085d0683/b2sdk/_internal/b2http.py#L575-L640
[PySession]: https://github.com/Backblaze/b2-sdk-python/blob/17a6be766237899f8d807feab4102533085d0683/b2sdk/_internal/session.py#L484-L498
[PyErrors]: https://github.com/Backblaze/b2-sdk-python/blob/17a6be766237899f8d807feab4102533085d0683/b2sdk/_internal/exception.py#L755-L776
[CLIEncryption]: https://github.com/Backblaze/B2_Command_Line_Tool/blob/685dc497f8bde0374dcaf5edcae0210d4dc9492b/b2/_internal/console_tool.py#L410-L463
[CreateBucket]: https://www.backblaze.com/apidocs/b2-create-bucket
