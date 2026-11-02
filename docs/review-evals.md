# Review evals

A seed set of past blazer bugs, to score a review agent whenever [REVIEW.md](../REVIEW.md),
[AGENTS.md](../AGENTS.md) or the model changes. It is the first step of item 6 in
`intent/review-process/intent.md`. Every row is `proposed, needs code owner confirmation`: the facts
below were checked against git history and `gh pr view`, but a code owner has not yet agreed that each one belongs in the set.

## How to score

1. Rebuild the buggy diff with the command in the last column and give only that diff to the reviewer,
   with the current `REVIEW.md` and `AGENTS.md`.
2. It is a hit when a finding names the bug class in the `Pass` column, with the `File:Line` in the
   buggy version (a few lines either way). Naming only a nearby style issue is a miss.
3. Record hits and misses per row. A miss becomes a line in [review-lessons.md](review-lessons.md) (or a
   sharper `REVIEW.md` pass) with the PR link, and the set is run again.

`Kind` says what the case is. `production bug` means the bug shipped on the default branch and a later
PR fixed it. `PR-branch bug` means the bug was only ever in a commit on an unmerged PR branch and was
fixed inside the same PR, so `main` never had it (it tests the reviewer on a feature PR, not on a
regression). `Buggy version` is the commit whose code has the bug. `Fix` is the commit that corrects it.
Line numbers refer to the buggy version. PR-branch commits need
`git fetch origin refs/pull/<n>/head:refs/remotes/origin/pr-<n>` first.

| PR | Kind | Bug (one sentence) | Introduced by | Buggy version, File:Line | Pass | Fix | Pinning test | Status |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| #78 | production bug | `b2_update_bucket` sent the cached `lifecycleRules` on every update, so an unrelated update could overwrite rules set elsewhere, and an empty list was dropped by `omitempty`, so rules could not be cleared. | Original import (`aaacf80`); the `var rules` plus `omitempty` shape is unchanged since. | `fd652a4` (parent of the fix): `base/base.go:682-689`, `internal/b2types/b2types.go:149`, `b2/baseline.go:279-289` | Wire | `20e3216` (squash of #78 on `main`) | `b2/lifecycle_update_test.go`: `TestUnrelatedUpdateDoesNotWipeRulesAddedElsewhere`, `TestUpdateWithEmptyLifecycleRulesClearsThem` | proposed, needs code owner confirmation |
| #74 | production bug | A bucket whose default encryption has an empty mode kept that value in the cached bucket, so each `Update` re-sent it and B2 answered 400 `Unsupported bucket default server-side encryption mode`. | `4fbab8f` "fix default bucket encryption param" (2026-09-03) started storing and re-sending the field raw. | `20e3216` (parent of the fix): `base/base.go:707` (sent), `:734` and `:819` (stored raw from the response) | Wire | `5c58436` (squash of #74 on `main`) | `b2/bucket_empty_sse_repeat_test.go`: `TestRepeatBucketUpdateDoesNotResendEmptyDefaultSSE`; `b2/bucket_empty_sse_test.go`: `TestBucketEmptyDefaultSSERoundTrip` | proposed, needs code owner confirmation |
| #30 | PR-branch bug | In a feature PR (v4 multi-bucket application keys), the first commit parsed the v4 `b2_authorize_account` key scope from top-level `storageApi.bucketIds` and `namePrefix`, but v4 puts it under `storageApi.allowed`, so a restricted key looked unrestricted. | `1b7db5d` (first commit of the PR; never on `main`) | `1b7db5d`: `base/base.go:495-496`, `internal/b2types/b2types.go:35-40` | Wire | `c061051` (PR branch, second commit; `main` has squash `fa10b35`) | `base/base_v4_test.go`: `TestAuthorizeAccountV4ReadsAllowedNesting`; live `b2/integration_test.go`: `TestMultiBucketKeyListsAllowedBuckets` | proposed, needs code owner confirmation |
| #131 | PR-branch bug | In a feature PR (progress callbacks), the first commit reported upload and download progress only when a whole chunk finished, so a single large chunk gave no intermediate callback. | `20a5611` (first commit of the PR; never on `main`) | `20a5611` (PR branch): `b2/reader.go:206`, `b2/writer.go:147-151` (`completeChunk`) | Bugs | `4d7d071` (PR branch, second commit; `main` has squash `bf89b29`) | `b2/progress_test.go`: `TestProgressCallbacks` (`assertProgressSamples` with `intermediate=true` fails with `no intermediate byte-level progress`) | proposed, needs code owner confirmation |
| #230 (candidate, see note) | production bug | An upload of exactly `Writer.ChunkSize` bytes took the large-file path with a single part (`b2_start_large_file`, one part, `b2_finish_large_file`), but B2 requires at least two parts. `ReadFrom` with a seekable reader chose the simple upload only when `size < csize`, and `Write` sent the buffer as a part as soon as it was exactly full. | Original import (`aaacf80`); the `size < int64(w.csize)` and `len(p) < left` comparisons are unchanged since. | `385339c^`: `b2/writer.go:704` (`ReadFrom`), `:301` (`Write`) | Bugs | `385339c` (squash of #230 on `main`) | `b2/writer_chunk_boundary_wire_test.go`: `TestWriterChunkBoundaryWire` (rows `ReadFrom/ChunkSize` and `Write/ChunkSize`) | proposed, needs code owner confirmation |

Note on the last row: item 6 of `intent/review-process/intent.md` lists #147 as a `Writer.ReadFrom` bug. #147
is not that: it adds the in-process B2 simulator (`x/b2sim`), and its only bug (the simulator rejected a
part upload sent with the `hex_digits_at_end` SHA1, `x/b2sim/sim.go:510`, fixed in PR-branch commit
`236d255`) was in test support, fixed inside the same PR, and never on `main`. It is not a case for this set.
#230 is the only merged PR found whose fix changes `Writer.ReadFrom` behaviour for a bug (a `git log -S` over
`ReadFrom` and `hex_digits_at_end` shows no other), so it is listed as a candidate. That is a guess about
intent, not a confirmed match. Open question for Gonzalo: was #230 the PR meant, or another one?

Item 6 also lists #131 as a bug. `main` never had the per-chunk version, so it is a PR-branch case, as
labelled above. #30 is the same kind of case. #78 and #74 are the two cases where the bug shipped on `main`
and a later PR fixed it (#230 makes a third, pending the question above).

## How to reproduce the buggy diff

The fix reversed, without tests and changelog, is the diff that introduces the bug (for a PR-branch case,
the diff the second commit undid):

```sh
git diff 20e3216 20e3216^ -- ':!*_test.go' ':!CHANGELOG.md'        # #78
git diff 5c58436 5c58436^ -- ':!*_test.go' ':!CHANGELOG.md'        # #74
git diff 385339c 385339c^ -- b2/writer.go                          # #230
git diff c061051 c061051^ -- ':!*_test.go' ':!CHANGELOG.md'        # #30, after fetching pr-30
git diff 4d7d071 4d7d071^ -- ':!*_test.go' ':!.sdkharness'          # #131, after fetching pr-131
```

For a PR-branch case, the buggy code is the PR's first commit, so reviewing `git diff 1b7db5d^ 1b7db5d`
(#30) or `git diff 20a5611^ 20a5611` (#131) gives the reviewer the whole feature with the bug in it. That
is the harder and more realistic test, because the reviewer must find the bug without being told which
lines to look at. For #78, #74 and #230 the reproduced diff is a reversal of the fix.
