# Spec: assert the listing prefix and cursor on every page, in b2 and sync
From: intent.md (Felipe Fumero). Issue: #196. Status: ready for review.
Skills applied: none beyond `AGENTS.md`, `REVIEW.md` and `docs/testing.md`.

Sources of truth (each test links the page):
- B2 `b2_list_file_names`: request `prefix`, `delimiter`, `startFileName`, `maxFileCount`; response
  `files`, `nextFileName`.
- B2 `b2_list_file_versions`: request `prefix`, `delimiter`, `startFileName`, `startFileId`,
  `maxFileCount`; response `files`, `nextFileName`, `nextFileId`.
- B2 `b2_list_unfinished_large_files`: request `startFileId`, `maxFileCount`; response `files`,
  `nextFileId`.
- B2 `b2_authorize_account`: `allowed.namePrefix` restricts a key to names under that prefix.

## Requirements

- **R1. Current objects, two pages.** `bucket.List(ctx, ListPrefix("logs/"), ListDelimiter("/"),
  ListPageSize(2))` over a scripted listing with three objects:
  - the first `b2_list_file_names` request has `prefix` `"logs/"`, `delimiter` `"/"`, no `startFileName`;
  - the second has the same `prefix` and `delimiter`, and `startFileName` equal to the `nextFileName` of the
    first response;
  - the iterator yields the three names in order, then `Err() == nil`.
- **R2. All versions, two pages.** The same with `ListHidden()` against `b2_list_file_versions`: the second
  request carries `startFileName` and `startFileId` from the first response's `nextFileName` and
  `nextFileId`, plus the same `prefix` and `delimiter`.
- **R3. Unfinished files, two pages.** `ListUnfinished()` against `b2_list_unfinished_large_files`: the
  second request carries `startFileId` equal to the first response's `nextFileId`. The prefix is not
  asserted (#184).
- **R4. Restricted key.** With an authorization whose `allowed` has `bucketId` `"bid"` and `namePrefix`
  `"restricted/"`, `List` with no `ListPrefix` sends `prefix` `"restricted/"` for names and for versions.
- **R5. Caller prefix wins on an unrestricted key.** With `allowed.namePrefix` null, `ListPrefix("logs/")`
  sends `prefix` `"logs/"` for names and for versions. (Kills `base/base.go:1643,1692`.)
- **R6. A wrong cursor fails the test, not the process.** The scripted listing returns a 400 error when a
  request's cursor is not the expected one, so a broken cursor ends the iteration with an error instead of
  looping. (Kills `b2/iterator.go:254,292,325`.)
- **R7. Sync lists only the destination prefix.** `sync.Sync(ctx, bucket, "p/", dir)`, with a local
  `a.txt`, sends `b2_list_file_names` with `prefix` `"p/"`. When the bucket holds a root `a.txt` and no
  `p/a.txt`, the local file is uploaded as `p/a.txt`. (Kills `b2/sync/sync.go:178`.)
- **R8. The sync fake behaves like B2.** The `b2_list_file_names` handler in `b2/sync/sync_test.go`
  returns only files whose name starts with the request `prefix`, starting at `startFileName`, at most
  `maxFileCount` per page, and sets `nextFileName` when more remain. Every existing test in `b2/sync`
  still passes unchanged.
- **R9. Test-only.** Outside `intent/listing-prefix-cursor/`, only `*_test.go` files change.

## Design

- `b2/listing_wire_test.go` (new, package `b2`) holds R1–R6 and uses `internal/b2wiretest`. A listing
  handler (`Handle`) serves page 1 when the request has no cursor, page 2 when it has exactly the expected
  cursor, and a 400 error otherwise (R6). Page contents and cursors are literals.
- R4 scripts `b2_authorize_account` with the restricted `allowed` block. Copy the auth JSON shape from
  `b2/update_cors_name_test.go` and set `"allowed":{"buckets":[{"id":"bid","name":"bucket"}],"namePrefix":"restricted/",...}`.
  Check the exact `allowed` shape against `base/base_v4_test.go`'s `v4AuthJSON`.
- `b2/sync/sync_prefix_wire_test.go` (new, package `sync`) holds R7, using `internal/b2wiretest` with a
  listing handler that returns the root `a.txt` only for an empty prefix.
- `b2/sync/sync_test.go` gets the R8 change to its existing `b2_list_file_names` case only.

## How each constraint in the intent is met

| Constraint | Met by |
| --- | --- |
| Test-only | R9 |
| Unfinished prefix out of scope | R3 asserts the cursor only |
| Expected values from the B2 docs | Literal pages and cursors; field names from the docs |
| Credential-free, race-safe | `b2wiretest`; `-race -count=5 -shuffle=on` |
| Fake corrected, not replaced | R8 |

## Areas of concern

1. **Stacked on #191.** This branch starts from #191's branch; #191 merges first. Owner: @ffumero2003.
2. **R8 may expose an existing sync test that relied on the fake ignoring the prefix.** If one fails, it
   was not testing what it claimed: stop and report it rather than loosening the fake. Owner: @ffumero2003.
3. **Conflict with #184.** When #184 adds the prefix to unfinished listing, R3 should gain a prefix check.
   Owner: @ffumero2003, or whoever lands #184.
