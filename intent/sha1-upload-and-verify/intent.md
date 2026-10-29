# Intent: pin the SHA1 on upload parts and make Reader.Verify catch a bad hash
Author: Felipe Fumero. Issue: #195. Parent: #97. Depends on: #191. Status: draft.

## Problem

SHA1 checksums are how B2 and blazer detect corrupted data in transit. Two paths have no test that would
notice them breaking.

**Upload.** Each large-file part is sent with `X-Bz-Content-Sha1` (`base/base.go:1534`), and B2 rejects a
part whose hash does not match. `TestComposeAllSourceMixesPreservePartHashOrder`
(`b2/compose_extended_wire_test.go:374`) only checks that the header is not empty. Sending 40 zeros for
every part leaves `go test ./base ./b2` green at a5e0d13. Against real B2 every large upload would then
fail.

**Download.** `(*Reader).Verify` (`b2/reader.go:357-370`) compares the bytes read with the SHA1 that B2
sent in `X-Bz-Content-Sha1`. The reader records that header at `b2/reader.go:185`. Both of these changes
leave the tests green, because the only test of `Verify` (`b2/integration_test.go:1025`) is live and
skips without credentials:

- not recording the header at `reader.go:185`;
- inverting the guards at `reader.go:367`.

`Verify` then reports a corrupted download as "could not verify" (`nil, false`) instead of an error, and a
caller that relies on it accepts bad data.

## Proposed outcome

Credential-free tests through the public API that fail on each change above:

- every part of a large upload carries the SHA1 of exactly the bytes in that part;
- `Verify` returns an error for a download whose bytes do not match `X-Bz-Content-Sha1`, `(nil, true)`
  when they match, and `(nil, false)` when verification is not possible (a ranged read, or a large file
  reported with `none`).

## Affected users and APIs

- `(*Object).NewWriter` (large uploads), `(*Object).NewReader`, `(*Object).NewRangeReader` and
  `(*Reader).Verify`, as observed on the wire. Behavior does not change.
- Test code only.

## Constraints

- **Test-only.** No production code changes. If a new test fails on current code, it found a product bug:
  stop and open a separate fix issue.
- Expected hashes are computed in the test with `crypto/sha1` over fixed literal bytes. That computation is
  independent of blazer. Header names and the `none` value come from the B2 docs for `b2_upload_part` and
  `b2_download_file_by_name`.
- Credential-free, offline, safe under `go test -race -count=5 -shuffle=on`.
- Built on the shared recording server from #191; no new private fake.

## Open questions

1. The `X-Bz-Info-Large_file_sha1` fallback for large-file downloads (`base/base.go:1875`) is a separate
   gap, tracked in #202. This intent leaves it out.
2. `Verify` has a known gap noted in its own comment: if exactly the file length is requested and the hash is
   bad, it returns `(nil, false)`. This intent pins today's behavior for full reads only, and leaves that
   case alone.
