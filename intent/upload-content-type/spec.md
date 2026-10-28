# Spec: assert the Content-Type sent on every upload
From: intent.md (Felipe Fumero). Issue: #197. Status: ready for review.
Skills applied: none beyond `AGENTS.md`, `REVIEW.md` and `docs/testing.md`.

Sources of truth (each test links the page):
- B2 `b2_upload_file`: request header `Content-Type`.
- B2 `b2_start_large_file`: request body field `contentType`.
- blazer's `Writer` documentation for the default type when none is set (`application/octet-stream`).

Fixed test value: `text/html; charset=utf-8`. It has a parameter on purpose, to catch any truncation.

## Requirements

- **R1. Explicit type, small upload.** `NewWriter(ctx, WithAttrsOption(&Attrs{ContentType: "text/html; charset=utf-8"}))`,
  writing fewer bytes than `ChunkSize`, sends one `upload_file` request whose `Content-Type` header is
  `text/html; charset=utf-8`. (Kills `b2/writer.go:355`.)
- **R2. Explicit type, large upload.** The same with `ChunkSize` below the data size sends one
  `b2_start_large_file` whose body `contentType` is `text/html; charset=utf-8`. (Kills `b2/writer.go:414`.)
- **R3. Default type.** Without `WithAttrsOption`, the small upload sends `Content-Type:
  application/octet-stream`, and the large upload sends `contentType` `application/octet-stream`.
- **R4. Test-only.** No file outside `*_test.go` and `intent/upload-content-type/` changes. SSE-B2 header
  tests are already covered (`b2/sse_b2_wire_test.go`) and are not duplicated.

## Design

One new file, `b2/upload_content_type_wire_test.go` (package `b2`), using `internal/b2wiretest`:

- Fixture: the authorization (the `updateAuthJSON` shape), one bucket, `upload_file` (a file JSON),
  `b2_start_large_file` (`{"fileId":"large-1"}`), `upload_part` (built in) and `b2_finish_large_file`
  (a file JSON). The upload URLs are built in.
- `TestWriterSendsContentType`: a table over {small, large} × {explicit, default}. Small rows use
  `AssertHeader` on `Last(t, "upload_file")`; large rows use `AssertJSON` on
  `Last(t, "b2_start_large_file")` for `contentType`.
- "Large" sets `ChunkSize` below the data size, as in #193 and #195. If today's code rejects a small
  `ChunkSize`, use the smallest it accepts.

## How each constraint in the intent is met

| Constraint | Met by |
| --- | --- |
| Test-only | R4 |
| Expected values | The literal type above; header and field names from the B2 docs |
| Credential-free, race-safe | `b2wiretest`; `-race -count=5 -shuffle=on` |
| Built on #191 | Only `internal/b2wiretest` |

## Areas of concern

1. **Stacked on #191.** This branch starts from #191's branch; #191 merges first. Owner: Felipe.
2. **Shared fixture with #193.** #193's object-lock tests need the same upload fixture. If both land, the
   second PR should reuse the first one's fixture rather than keep two copies. Owner: whoever merges second.
3. **The issue text** lists SSE-B2 as untested. Update the issue to match this scope before the PR opens.
   Owner: Felipe.
