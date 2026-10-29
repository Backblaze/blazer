# Spec: a conformance check for unbound (unknown-length) uploads
From: intent.md (Felipe Fumero). Issue: #213. Status: ready for review.
Skills applied: none beyond `AGENTS.md`, `REVIEW.md` and `docs/testing.md`.

Sources of truth (cited in the check's DOCS header, as `large.multipart` does):
- https://www.backblaze.com/docs/cloud-storage-large-files: "Each large file must consist of at least two
  parts, and all of the parts except the last one must be at least 5 MB."
- https://www.backblaze.com/apidocs/b2-finish-large-file: a large file's `contentSha1` is `"none"`.

## Requirements

- **R1. The check.** A new executable `.sdkharness/tests/conformance/large.unbound_incremental`, built on the
  `large.multipart` skeleton (same guards, offline build, verdict format). Its Go program:
  - generates a 12,000,000-byte random payload and its SHA-1;
  - writes it through `io.Copy(w, r)` where `r` is the read side of an `io.Pipe` fed by a goroutine, so the
    Writer cannot learn the size; `w.ChunkSize = 5000000`;
  - wraps `http.DefaultTransport` with `b2.Transport` and asserts one `b2_start_large_file`, at least two
    `b2_upload_part`, and one `b2_finish_large_file` request after `Close`;
  - after `Close`, reads `Attrs` and asserts `Size == 12000000` and `SHA1 == "none"` (a large file);
  - downloads the object and asserts its bytes equal the payload (compare SHA-1 and length).
  Any mismatch reports `FAIL (<area> -- <one line>)`; success reports `PASS`.
- **R2. Unknown length is real.** The program must not pass a size, `Attrs.Size` or a `ReadSeeker`/`Len()`
  that would let the Writer take a sized path. The reader type is `*io.PipeReader`.
- **R3. The contract row.** `.sdkharness/tests.tsv` gains
  `conformance	large.unbound_incremental	simulator	./.sdkharness/tests/run-conformance`, placed with the other
  `large.*` rows.
- **R4. The contract test follows.** `b2/sdkharness_contract_test.go` expects 51 lines and 34 conformance rows
  (from 50 and 33).
- **R5. Test-only.** Only `.sdkharness/`, `b2/sdkharness_contract_test.go` and
  `intent/unbound-incremental-conformance/` change. The check is executable (`0755`).

## Design

Copy `large.multipart`, rename the capability, replace the payload/upload section with the pipe-based
upload, count the large-file request paths through a `b2.Transport` wrapper, and keep the shared helpers it
already has (verdict printing, `tidy_detail`, build and loopback guard). Drop the `large_file_sha1`
assertions: with an unknown length the whole-file digest is not known when `b2_start_large_file` is sent, so
B2's recommendation cannot apply.

## How each constraint is met

| Constraint | Met by |
| --- | --- |
| Test-only | R5 |
| Existing check contract | R1 built on `large.multipart`; `tests/selftest` still passes |
| Expected values from the docs and the payload | R1 assertions; `"none"` from `b2_finish_large_file` |

## Areas of concern

1. **Harness wiring.** The harness repository must allow the new scenario and record its adoption before the
   capability can be claimed. That work is outside this repository. Owner: @ffumero2003.
2. **Small final part.** 12 MB at 5 MB chunks gives parts of 5, 5 and 2 MB, matching `large.multipart`. Owner:
   @ffumero2003.
