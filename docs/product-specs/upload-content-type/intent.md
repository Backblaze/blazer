# Intent: assert the Content-Type sent on every upload
Author: Felipe Fumero. Issue: #197. Parent: #97. Depends on: #191. Status: draft.

## Problem

A `Writer` uploads with the content type the caller set through `WithAttrsOption(&Attrs{ContentType: ...})`,
or `application/octet-stream` when none was set. The choice is made twice, once for each upload path:

- `b2/writer.go:355`, simple upload (`b2_upload_file`, `Content-Type` header);
- `b2/writer.go:414`, large upload (`b2_start_large_file`, `contentType` body field).

Inverting either check (`ctype == ""` to `ctype != ""`) overwrites the caller's content type with
`application/octet-stream` and leaves `go test ./base ./b2/...` green at a5e0d13. Content type decides how
browsers and CDNs serve a file, so this would break every public download of an uploaded web asset.

### Scope correction

Issue #197 also listed the per-request SSE-B2 header (`base/base.go:1194`) as untested. That was a
`base`-only finding. Re-checked against the whole module, breaking that header fails
`TestWithSSEB2WireAndRoundTrip` (`b2/sse_b2_wire_test.go:182`) and two more `b2` tests. It is already
covered, so this change only adds the Content-Type tests.

## Proposed outcome

Credential-free tests through `(*Object).NewWriter` that fail on either change above:

- an explicit content type reaches B2 unchanged on a small and on a large upload;
- with no content type set, `application/octet-stream` is sent on both.

## Affected users and APIs

- `(*Object).NewWriter` with `WithAttrsOption`, as observed on the wire. Behavior does not change.
- Test code only.

## Constraints

- **Test-only.** No production code changes. If a new test fails on current code, it found a product bug:
  stop and open a separate fix issue.
- Expected values come from the B2 docs for `b2_upload_file` (`Content-Type` header) and
  `b2_start_large_file` (`contentType` field), where `b2/x-auto` and explicit MIME types are described.
  blazer's default `application/octet-stream` is asserted as blazer's documented behavior, not a B2 rule.
- Credential-free, offline, safe under `go test -race -count=5 -shuffle=on`.
- Built on the shared recording server from #191; no new private fake.

## Open questions

None.
