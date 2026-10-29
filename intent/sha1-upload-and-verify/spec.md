# Spec: pin the SHA1 on upload parts and make Reader.Verify catch a bad hash
From: intent.md (Felipe Fumero). Issue: #195. Status: ready for review.
Skills applied: none beyond `AGENTS.md`, `REVIEW.md` and `docs/testing.md`.

Sources of truth (each test links the page):
- B2 `b2_upload_part`: request header `X-Bz-Content-Sha1`, the hex SHA1 of the part's bytes.
- B2 `b2_download_file_by_name`: response header `X-Bz-Content-Sha1`, the hex SHA1 of the whole file, or
  `none` for large files uploaded without one.

Expected hashes are `fmt.Sprintf("%x", sha1.Sum(part))` over fixed literal bytes in the test.

## Requirements

- **R1. Part hashes.** Writing 25 fixed bytes with `ChunkSize` 10 sends three `upload_part` requests. For
  each one, `X-Bz-Content-Sha1` equals the SHA1 of exactly the bytes in that request's body, and
  `X-Bz-Part-Number` matches its position. Parts may arrive in any order, so the test matches by part
  number. (Kills `base/base.go:1534`.)
- **R2. Verify succeeds on a match.** `NewReader` on an object whose download response body is fixed bytes
  `B` with `X-Bz-Content-Sha1` = SHA1(`B`), read to EOF, gives `Verify() == (nil, true)`.
- **R3. Verify fails on a mismatch.** The same, with the body changed by one byte but the header still
  SHA1(`B`), gives a non-nil error and `true`. (Kills `b2/reader.go:185` and `:367`.)
- **R4. No verification on a ranged read.** `NewRangeReader` with offset > 0 gives `(nil, false)`.
- **R5. No verification for `none`.** A full read with `X-Bz-Content-Sha1: none` gives `(nil, false)`.
- **R6. Test-only.** No file outside `*_test.go` and `intent/sha1-upload-and-verify/` changes.

## Design

One new file, `b2/sha1_wire_test.go` (package `b2`), using `internal/b2wiretest`.

- **Upload (R1):** script the authorization, a bucket, `b2_start_large_file`, `upload_part` (built in) and
  `b2_finish_large_file`. After `Close`, read `srv.Calls("upload_part")`, and for each record compute
  SHA1 over its `Body`, then compare with its `X-Bz-Content-Sha1`. Also compare the set of bodies with
  the three slices of the fixed input, so a reordering or wrong slicing is caught.
- **Download (R2–R5):** the helper names a request by the last segment of its path. A download by name
  goes to `<downloadUrl>/file/<bucket>/<object>`, so it is recorded and scripted under the object name.
  Use object names without `/` (for example `"verify-ok"`, `"verify-bad"`). Each response sets
  `Content-Length`, `Content-Type`, `X-Bz-File-Id`, `X-Bz-File-Name` and `X-Bz-Content-Sha1`, the
  headers `base.downloadFile` reads (`base/base.go:1804-1890`). Read with `io.ReadAll`, which reads past
  the end, then call `Verify`.
- See `b2/progress_wire_test.go` for an existing fake download response that today's reader accepts.

## How each constraint in the intent is met

| Constraint | Met by |
| --- | --- |
| Test-only | R6 |
| Independent expected values | Hashes from `crypto/sha1` over literals; header names from the B2 docs |
| Credential-free, race-safe | `b2wiretest`; `-race -count=5 -shuffle=on` |
| Built on #191 | Only `internal/b2wiretest` |

## Areas of concern

1. **Stacked on #191.** This branch starts from #191's branch; #191 merges first. Owner: @ffumero2003.
2. **Small `ChunkSize`.** B2 requires parts of at least 5 MB except the last, and #182 may add that check.
   If it does, R1 must use larger data. Owner: @ffumero2003, or whoever lands #182 second.
3. **Range handling.** The reader may send a `Range` header. The fake answers with the whole body for R2,
   R3 and R5, and with the requested slice for R4, following `b2/progress_wire_test.go`. Validating
   `Content-Range` is #175 and #179, not this change.
