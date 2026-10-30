# Intent: assert the listing prefix and cursor on every page, in b2 and sync
Author: Felipe Fumero. Issue: #196. Parent: #97. Depends on: #191. Related: #184. Status: draft.

## Problem

Listing is how callers find what to read, overwrite or delete, so a listing that silently covers the wrong
objects is dangerous. These changes each leave `go test ./...` green at a5e0d13:

- `base/base.go:1643` and `:1692`: in `ListFileNames` and `ListFileVersions`, `prefix == ""` becomes
  `prefix != ""`. A caller's prefix is replaced by the key's restriction prefix, which is empty for an
  unrestricted key, so the whole bucket is listed.
- `b2/iterator.go:254`, `:292` and `:325`: `c == nil` becomes `c != nil`. The cursor is then reset on every
  page, and its prefix, delimiter and continuation are lost. The listing fake in
  `b2/iterator_advance_test.go:34-41` serves pages by call number, not by cursor, so it cannot tell.
- `b2/sync/sync.go:178`: `ListPrefix(prefix)` dropped from the destination listing. The sync test fake
  (`b2/sync/sync_test.go:145-153`) returns every stored file and ignores `prefix`, `startFileName` and
  `maxFileCount`. A bucket-root `a.txt` can then be taken for the destination of a local `a.txt` synced
  under `p/`, and the upload skipped.

In a delete loop or a sync, any of these touches objects outside the prefix the caller asked for.

## Proposed outcome

Credential-free tests through the public API that fail on each change above:

- every `b2_list_file_names` and `b2_list_file_versions` request carries the prefix and delimiter the
  caller asked for, on the first page and on every later page, together with the right start cursor;
- with a key restricted to a prefix and no caller prefix, the key's prefix is sent;
- unfinished-file listing sends the right continuation on later pages;
- `sync.Sync` lists only the destination prefix, and the sync fake behaves like B2 for prefix and paging.

## Affected users and APIs

- `(*Bucket).List` with `ListPrefix`, `ListDelimiter`, `ListHidden`, `ListUnfinished`; `sync.Sync`.
  Behavior does not change.
- Test code only, including the shared sync test fake.

## Constraints

- **Test-only.** No production code changes. If a new test fails on current code, it found a product bug:
  stop and open a separate fix issue.
- **Out of scope: prefix on unfinished-file listing.** `ListPrefix` with `ListUnfinished` is not sent today
  (`base.ListUnfinishedLargeFiles` has no prefix parameter). That is product bug #184, so it is not
  asserted here.
- Expected values come from the B2 docs for `b2_list_file_names`, `b2_list_file_versions` and
  `b2_list_unfinished_large_files`, never from blazer's output.
- Credential-free, offline, safe under `go test -race -count=5 -shuffle=on`.
- Built on the shared recording server from #191. The existing sync fake is corrected rather than replaced.

## Open questions

1. Whether to replace `b2/iterator_advance_test.go`'s call-number fake with the new cursor-aware tests,
   or keep both. This intent keeps it. (@goanpeca)
