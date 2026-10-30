# Review evals

A seed set of past blazer bugs, to score a review agent whenever [REVIEW.md](../REVIEW.md),
[AGENTS.md](../AGENTS.md) or the model changes. It is the first step of item 6 in
`intent/review-process/intent.md`. Every row is `proposed, needs code owner confirmation`: the facts
below were read from the PR diffs, but a code owner has not yet agreed that each one belongs in the set.

## How to score

1. Rebuild the buggy diff with the command in the last column and give only that diff to the reviewer,
   with the current `REVIEW.md` and `AGENTS.md`.
2. It is a hit when a finding names the bug class in the `Pass` column, with the `File:Line` in the
   buggy version (a few lines either way). Naming only a nearby style issue is a miss.
3. Record hits and misses per row. A miss becomes a line in [review-lessons.md](review-lessons.md) (or a
   sharper `REVIEW.md` pass) with the PR link, and the set is run again.

`Buggy version` is the commit whose code has the bug. `Fix` is the commit on `main` (or on the PR branch
when `main` holds only a squash). Line numbers refer to the buggy version. PR-branch commits need
`git fetch origin refs/pull/<n>/head:refs/remotes/origin/pr-<n>` first.

| PR | Bug (one sentence) | Buggy version, File:Line | Pass | Fix | Pinning test | Status |
| --- | --- | --- | --- | --- | --- | --- |
| #78 | `b2_update_bucket` sent the cached `lifecycleRules` on every update, so an unrelated update could overwrite rules set elsewhere, and an empty list was dropped by `omitempty`, so rules could not be cleared. | `fd652a4`: `base/base.go:682-689`, `internal/b2types/b2types.go:149`, `b2/baseline.go:279-289` | Wire | `20e3216` | `b2/lifecycle_update_test.go`: `TestUnrelatedUpdateDoesNotWipeRulesAddedElsewhere`, `TestUpdateWithEmptyLifecycleRulesClearsThem` | proposed, needs code owner confirmation |
| #74 | A bucket whose default encryption has an empty mode kept that value in the cached bucket, so each `Update` re-sent it and B2 answered 400 `Unsupported bucket default server-side encryption mode`. | `20e3216`: `base/base.go:707` (sent), `:734` and `:819` (stored raw from the response) | Wire | `5c58436` | `b2/bucket_empty_sse_repeat_test.go`: `TestRepeatBucketUpdateDoesNotResendEmptyDefaultSSE`; `b2/bucket_empty_sse_test.go`: `TestBucketEmptyDefaultSSERoundTrip` | proposed, needs code owner confirmation |
| #30 | The v4 `b2_authorize_account` parser read key scope from top-level `storageApi.bucketIds` and `namePrefix`, but v4 puts it under `storageApi.allowed`, so a restricted key looked unrestricted. | `1b7db5d`: `base/base.go:495-496`, `internal/b2types/b2types.go:35-40` | Wire | `c061051` | `base/base_v4_test.go`: `TestAuthorizeAccountV4ReadsAllowedNesting`; live `b2/integration_test.go`: `TestMultiBucketKeyListsAllowedBuckets` | proposed, needs code owner confirmation |
| #131 | Upload and download progress was reported only when a whole chunk finished, so a single large chunk gave no intermediate callback. | `20a5611` (PR branch): `b2/reader.go:206`, `b2/writer.go:196,249,372` | Bugs | `4d7d071` (PR branch; `main` has squash `bf89b29`) | `b2/progress_test.go`: `TestProgressCallbacks` (`assertProgressSamples` with `intermediate=true` fails with `no intermediate byte-level progress`) | proposed, needs code owner confirmation |
| #147 (see note) | The `x/b2sim` simulator rejected a part upload sent with the `hex_digits_at_end` SHA1 that `Writer.ReadFrom` uses for seekable input, so a `ReadFrom` multipart upload failed with `part SHA1 mismatch`. | `250fe40` (PR branch): `x/b2sim/sim.go:509` (part), `:457-461` (simple upload handles it) | Wire | `236d255` (PR branch; `main` has squash `7b61e90`) | `x/b2sim/sim_test.go`: `TestReadFromMultipartTrailingSHA1`, `TestPartTrailingDigestRejectsInvalid` | proposed, needs code owner confirmation |

Note on #147: the PR adds the in-process simulator (`x/b2sim`), not a change to `Writer`. The bug is in the
simulator, which is test support, and `b2/buffer.go:73` and `base/base.go:1549` show that blazer does send
`hex_digits_at_end` on the `ReadFrom` path. If the intent was a bug in production code, another PR is
meant and the row needs a replacement. Item 6 also lists #131 as a bug, but `main` has no per-chunk
version: it existed only in commit `20a5611` on the PR branch, and was fixed inside the same PR.

## How to reproduce the buggy diff

The fix reversed, without tests and changelog, is the diff that introduces the bug:

```sh
git diff 20e3216 20e3216^ -- ':!*_test.go' ':!CHANGELOG.md'        # #78
git diff 5c58436 5c58436^ -- ':!*_test.go' ':!CHANGELOG.md'        # #74
git diff c061051 c061051^ -- ':!*_test.go' ':!CHANGELOG.md'        # #30
git diff 4d7d071 4d7d071^ -- ':!*_test.go' ':!.sdkharness'          # #131, after fetching pr-131
git diff 236d255 236d255^ -- x/b2sim/sim.go                        # #147, after fetching pr-147
```

To review the whole original change instead, check out the buggy commit and diff it against its parent,
for example `git diff 1b7db5d^ 1b7db5d` for #30.
