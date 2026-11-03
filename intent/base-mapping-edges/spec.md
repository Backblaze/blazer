# Spec: cover the remaining base request and response mapping edges
From: intent.md (Felipe Fumero). Issue: #202. Status: ready for review.
Skills applied: none beyond `AGENTS.md`, `REVIEW.md` and `docs/testing.md`.

Sources of truth (each test links the page):
- B2 `b2_list_file_names` / file info: `uploadTimestamp` is milliseconds since midnight, January 1, 1970 UTC.
- B2 `b2_download_file_by_name` / `_by_id`: response header `X-Bz-Content-Sha1` is `none` for large files;
  `X-Bz-Info-*` headers carry file info, including `large_file_sha1` when it was set at start.
- HTTP `User-Agent` (RFC 9110 §10.1.5): product tokens separated by spaces, most significant first.

## Requirements

- **R1. Custom user agent reaches the wire.** With `AuthorizeAccount(..., UserAgent("acme-app/1.2"))` against
  an `httptest` server, the `b2_authorize_account` request's `User-Agent` starts with `"acme-app/1.2 "` and
  is longer than that prefix (the default agent follows). Without the option, the header does not contain
  `"acme-app"`.
- **R2. Millisecond timestamps.** A table test of `millitime` with literals:
  `0` → `time.Unix(0, 0)`; `999` → `time.Unix(0, 999000000)`; `1000` → `time.Unix(1, 0)`;
  `1700000000123` → `time.Unix(1700000000, 123000000)`. Compare with `time.Time.Equal`.
- **R3. Large-file SHA-1 on download.** A download (by name and by ID, as in `download_gzip_test.go`) whose
  response has `X-Bz-Content-Sha1: none` and `X-Bz-Info-large_file_sha1: <40 hex>` yields
  `FileReader.SHA1 == <40 hex>`. With a real `X-Bz-Content-Sha1` and a different `large_file_sha1`, `SHA1` is
  the content SHA-1. With `none` and no info header, `SHA1 == "none"`.
- **R4. Test-only.** Only `base/*_test.go` and `intent/base-mapping-edges/` change.

## Design

One new file, `base/mapping_edges_test.go` (package `base`), with `TestUserAgentOptionReachesTheWire`,
`TestMillitime` and `TestDownloadUsesLargeFileSHA1WhenContentSHA1IsNone`. R1 and R3 use `httptest.NewServer`
as the existing base tests do.

## How each constraint is met

| Constraint | Met by |
| --- | --- |
| Test-only | R4 |
| Expected values from the docs | R2 literals from the millisecond definition; R3 header names from the download docs |
| Credential-free, race-safe | `httptest`; `-race -count=5 -shuffle=on` |

## Areas of concern

1. **Header canonicalization in R3.** Go canonicalizes `X-Bz-Info-large_file_sha1` to
   `X-Bz-Info-Large_file_sha1`; the info key blazer reads is `Large_file_sha1`. The test sends the lower-case
   form B2 documents and asserts the result, not the internal key. Owner: @ffumero2003.
