# Intent: cover the remaining base request and response mapping edges
Author: Felipe Fumero. Issue: #202. Parent: #97. Status: draft.

## Problem

Three changes to `base` leave the whole module's tests green (`go test ./...`, re-checked 2026-10-29 at a5e0d13):

- `base/base.go:401`: `getUserAgent` returns only `DefaultUserAgent`, dropping the caller's `UserAgent(...)`
  option. Blazer's own README and the B2 integration guidance ask integrations to identify themselves; a
  dropped agent is invisible until someone reads server logs.
- `base/base.go:368`: `millitime` converts B2's millisecond timestamps. Changing the remainder (`t%1000` to
  `t%100`) corrupts every sub-second part of `uploadTimestamp` with no test failing.
- `base/base.go:1875`: a download of a large file reports `X-Bz-Content-Sha1: none`; blazer falls back to
  `X-Bz-Info-large_file_sha1`. Removing the fallback makes large-file downloads report `"none"` and lose
  integrity checking, with no test failing.

Two other items listed on the issue are already covered (struck on the issue).

## Proposed outcome

A credential-free test kills each of the three changes, with expected values from the B2 docs.

## Affected users and APIs

Test code only.

## Constraints

- Test-only. If a new test fails on current code, it found a product bug: stop and open a fix issue.
- Expected values are literals from the B2 docs, never from blazer's output.
- Credential-free and safe under `go test -race -count=5 -shuffle=on`. Reuse the `httptest` patterns already in
  `base/*_test.go` (for example `base/download_gzip_test.go`); no new helper package.

## Open questions

None.
