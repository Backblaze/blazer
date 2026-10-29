# Spec: cover the remaining b2 request-mapping edges found by mutation
From: intent.md (Felipe Fumero). Issue: #201. Status: ready for review.
Skills applied: none beyond `AGENTS.md`, `REVIEW.md` and `docs/testing.md`.

Sources of truth (each test links or quotes its source):
- B2 large files: non-final parts at least 5 MB (`absoluteMinimumPartSize`), at most 5 GB.
- B2 `b2_update_bucket`: omitted fields are left unchanged; `corsRules`, `replicationConfiguration`.
- B2 replication rules: `priority` 1 to 2147483647.
- B2 file info: at most 10 `X-Bz-Info-*` entries per file.
- B2 `b2_list_file_names`/`_versions` `maxFileCount`; `b2_list_unfinished_large_files` `maxFileCount` at most 100.
- Blazer doc comments: `ListPageSize` ("default (and maximum) is 1000 objects, except for unfinished large
  files, which is 100"); `Writer.ChunkSize` ("when determining whether to upload a file normally or when to
  split it into parts"); `Client.Status`.

## Requirements

- **R1. Compose minimum part.** A non-final copied range of exactly 5,000,000 bytes is accepted; 4,999,999 is
  rejected; a range of 5,000,000 bytes at a large offset (`bytes=9000000000-9004999999`) is accepted.
- **R2. Rules-only replication.** `Bucket.Update` with `AsReplicationSource.ReplicationRules` set and no
  `SourceApplicationKeyID` sends `replicationConfiguration.asReplicationSource.replicationRules` with those
  rules, and does not panic.
- **R3. CORS kept on unrelated update.** A bucket with one CORS rule, updated with only a lifecycle change,
  sends a `b2_update_bucket` whose `corsRules` equals the existing rule (or omits `corsRules`), never `[]`.
  A subsequent `bucket.Attrs()` still reports the original rule. Removing the CORS guard is equivalent
  under this contract: the omitted field keeps the server's CORS configuration unchanged.
- **R4. Reauth waiter survives a cancelled leader.** Two requests hit an expired token at once. The leader's
  context is cancelled while `b2_authorize_account` is in flight (the handler blocks until then). The waiter,
  whose context is live, returns success after reauthorizing itself, not `context.Canceled`. Deterministic:
  channels, no sleeps beyond a 10-second test deadline.
- **R5. Replication priority bounds.** `SetupReplication` with priority `2147483647` passes validation;
  `2147483648` (on 64-bit) and `0` return the "priority must be between 1 and 2147483647" error before any
  request.
- **R6. SSE-C key ID conflict.** A Writer with SSE-C and file info `{"sse_c_key_id":"a","SSE_C_KEY_ID":"b"}`
  fails with the conflict error and sends no upload request; equal values in both cases are accepted.
- **R7. Page size clamps.** Each request's `maxFileCount`: `ListPageSize(5000)` → `1000`;
  `ListPageSize(-1)` → `1000`; `ListPageSize(500)` → `500`; `ListUnfinished()` with `ListPageSize(500)` → `100`.
- **R8. Status cleanup.** After a Writer and a Reader on one client are closed, `Client.Status()` lists no
  writers and no readers.
- **R9. Resume reads every page of parts.** Resume over an unfinished file whose `b2_list_parts` returns
  100 parts then 3 parts (`nextPartNumber` 101, then null) treats all 103 parts as uploaded: no
  `b2_upload_part` for parts 1 to 103.
- **R10. No 11th file-info entry.** `fileInfoFromAttrs` with 10 user entries plus `SHA1` and `LastModified`
  returns exactly the 10 user entries; with 9 user entries it adds `large_file_sha1` only.
- **R11. ChunkSize neighbors.** `ReadFrom` with a seekable reader of `ChunkSize - 1` and `ChunkSize + 1`
  bytes: assert the endpoint sequence and the uploaded bytes for each. The exact-ChunkSize case exposed
  a one-part large-file upload and is split into a separate product-bug issue (number to be added), per
  the owner's direction. The `<` to `<=` break is not provable with these remaining cases.
- **R12. Test-only.** Only `b2/*_test.go` and `intent/b2-mapping-edges/` change.

## Design

Two new files in package `b2`:
- `b2/bucket_mapping_edges_test.go`: R1, R2, R3, R4, R5.
- `b2/writer_listing_edges_test.go`: R6, R7, R8, R9, R10, R11.
Use `testRoot`/`testFile` where they model the call (for example R9 through `testFile.listParts`), else
`internal/b2wiretest`. R10 calls `fileInfoFromAttrs` directly.

## How each constraint is met

| Constraint | Met by |
| --- | --- |
| Test-only | R12 |
| Expected values from docs or doc comments | Sources above; every number is a literal |
| Credential-free, race-safe | fakes and `b2wiretest`; `-race -count=5 -shuffle=on` |

## Areas of concern

1. **R3 clearing CORS.** Keeping CORS on an empty `CORSRules` means a caller cannot clear rules through
   `Update`. That is today's behavior and this spec pins it; changing it is a separate issue. Owner: @ffumero2003.
2. **R10 and #169.** See the intent's open question. Owner: @ffumero2003.
3. **R4 determinism.** If the reauth path cannot be made deterministic with the available seams, stop and
   report rather than adding sleeps. Owner: @ffumero2003.
