# Plan: assert the listing prefix and cursor on every page, in b2 and sync (from intent.md and spec.md)

Branch: `test-issue-196-listing-prefix-cursor`, stacked on `test-issue-191-shared-recording-server`. One
PR for issue #196 carries this folder and the tests. Apply the lean-code ladder from `AGENTS.md`: reuse
`internal/b2wiretest` and existing fixtures, add only what the tests need.

## Files that change

```
b2/listing_wire_test.go                    (new)   R1-R6
b2/sync/sync_prefix_wire_test.go           (new)   R7
b2/sync/sync_test.go                       (edit)  R8: the b2_list_file_names case of the existing fake only
intent/listing-prefix-cursor/plan.md       (edit only if the implementation departs from this plan)
```

No production file changes (R9).

## Order of work

0. **Get #191's code.** Run `git merge test-issue-191-shared-recording-server` (merge, never rebase). If
   `internal/b2wiretest/server.go` is still missing, stop: blocked on #191. Read
   `intent/b2wiretest/spec.md` for the helper API.
1. **Paging handler.** A small local function that builds a `b2wiretest` handler from two literal pages
   and the expected cursor field(s):
   - no cursor in the request: page 1;
   - the exact expected cursor: page 2;
   - anything else: status 400 with a B2 error body.
2. **Names, versions, unfinished** (R1, R2, R3, R6). `TestListSendsPrefixAndCursorOnEveryPage`, a table
   with three rows (current, hidden, unfinished). For each row:
   - iterate to the end;
   - assert the names yielded and `Err() == nil`;
   - assert `prefix`/`delimiter` on both requests (not for unfinished) and the cursor on request 2,
     using `AssertJSON` on `srv.Calls(<call>)[0]` and `[1]`.
3. **Prefix source** (R4, R5). `TestListPrefixFromKeyOrCaller`: rows for {restricted key, no caller
   prefix} and {unrestricted key, caller prefix}, each for names and versions; one page is enough.
4. **Sync** (R7). `TestSyncListsOnlyTheDestinationPrefix` in package `sync`:
   - create a temp dir with `a.txt`;
   - script a listing handler that returns a root `a.txt` only when `prefix` is empty, and nothing for
     `"p/"`, plus upload URLs (built in) and `upload_file` (a file JSON);
   - assert the list request's `prefix` is `"p/"` and one `upload_file` was recorded for `p/a.txt`
     (`X-Bz-File-Name`, URL-escaped as B2 requires).
5. **Sync fake** (R8). In `b2/sync/sync_test.go`, change only the `b2_list_file_names` case so it:
   - decodes the request;
   - filters by `prefix`;
   - starts at `startFileName`;
   - caps at `maxFileCount`;
   - sets `nextFileName`.

   Run `go test ./b2/sync`. If an existing test now fails, stop and report it (spec concern 2).
6. **Check and self-review.** `make ci`; then a fresh-context review of the diff per `AGENTS.md`, answering
   each finding.

Steps 2, 3 and 4 are independent once step 1 exists. Step 5 is independent of 2 to 4.

## Risks

- **A new test fails on current code.** Then it found a product bug. Stop and report; do not fix it here.
- **Infinite listing.** A broken cursor must not loop forever. The 400 in step 1 ends the iterator; also
  wrap each test in a context with a 10-second timeout.
- **Restricted-key auth shape.** If `allowed` is shaped wrong, blazer may ignore it. Take the shape from
  `base/base_v4_test.go`'s `v4AuthJSON`.
- **Changelog.** The #196 diff is test-only, but the default `origin/main` baseline also includes #191's
  non-test helper files. The requested `make ci` ran and passed every preceding check, then failed with
  `No changelog entry: run 'make changelog-new' (or label the PR skip-changelog).` Keep that failure
  visible; changing #191's changelog policy is out of scope. Also run
  `make ci BASE_REF=test-issue-191-shared-recording-server` to check this stacked change against its
  dependency. This is a verification adjustment only; the implementation files and requirements are
  unchanged.

## Proof

Apply each break, run `go test -count=1 ./b2 ./b2/sync -run 'ListSends|ListPrefixFrom|SyncListsOnly'`,
record the failing output, revert, record the passing output.

| Req | Break to apply | Test that must fail |
| --- | --- | --- |
| R5 | `base/base.go:1643`: `prefix == ""` to `prefix != ""` | `TestListPrefixFromKeyOrCaller` (names) |
| R5 | `base/base.go:1692`: the same in `ListFileVersions` | `TestListPrefixFromKeyOrCaller` (versions) |
| R1, R6 | `b2/iterator.go:292`: `c == nil` to `c != nil` in `listCurrentObjects` | `TestListSendsPrefixAndCursorOnEveryPage` (current) |
| R2, R6 | `b2/iterator.go:254`: the same in `listObjects` | `TestListSendsPrefixAndCursorOnEveryPage` (hidden) |
| R3, R6 | `b2/iterator.go:325`: the same in `listUnfinishedLargeFiles` | `TestListSendsPrefixAndCursorOnEveryPage` (unfinished) |
| R7 | `b2/sync/sync.go:178`: `bucket.List(ctx)` without `ListPrefix(prefix)` | `TestSyncListsOnlyTheDestinationPrefix` |

Also record `go test -race -count=5 -shuffle=on ./b2 ./b2/sync` passing with `B2_APPLICATION_KEY_ID`,
`B2_APPLICATION_KEY`, `B2_ACCOUNT_ID` and `B2_ACCOUNT_KEY` unset, and `make ci` passing.
