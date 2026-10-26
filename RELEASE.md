# Releasing Blazer

Blazer is a Go module: a release is a `Version X.Y.Z` commit on `main` and a `vX.Y.Z` tag on that commit.
The changelog is assembled from `.changes/unreleased` fragments with [changie](https://changie.dev).

## What a release commit contains

Every release since v0.7.0 (for example v0.9.0, `4ebbb6d`) changed exactly these, in one commit:

| File | Change | Done by |
| --- | --- | --- |
| `CHANGELOG.md` | New `## [X.Y.Z] - YYYY-MM-DD` section and its compare link | `changie batch` + `changie merge` |
| `base/base.go` | `DefaultUserAgent = "blazer/X.Y.Z"` (sent on every request) | `changie merge` (replacement in `.changie.yaml`) |
| `bin/b2keys/go.mod` | `require github.com/Backblaze/blazer vX.Y.Z` | `changie merge` (replacement in `.changie.yaml`) |

Past releases were pushed straight to `main`. From now on the release commit goes through a PR like any
other change, so CI and a code owner see it before it is tagged.

## Versioning

Semantic versioning. Before 1.0, a breaking change bumps the minor version. changie picks the bump from
the fragment kinds (`Added`, `Changed`, `Deprecated`, `Removed` → minor; `Fixed`, `Security` → patch);
pass an explicit version to `changie batch` to override it.

## Steps

1. Make sure `main` is green and every PR meant for the release has merged.
2. Branch from `main`: `git switch -c release/vX.Y.Z origin/main`.
3. Batch the fragments and rebuild the changelog and version strings:

   ```bash
   go run github.com/miniscruff/changie@v1.24.0 batch auto --dry-run
   go run github.com/miniscruff/changie@v1.24.0 batch auto
   go run github.com/miniscruff/changie@v1.24.0 merge
   ```

4. Check the diff touches exactly the three files above plus `.changes/`. Edit `.changes/vX.Y.Z.md` for
   readability if needed and run `merge` again. Run `make ci`.
5. Commit as `Version X.Y.Z` and open a PR with the changelog section as its body. A code owner approves.
6. After it merges (`main` requires linear history, so squash or rebase), tag the release PR's merge
   commit, not the tip of `main`, which may already include later PRs:

   ```bash
   git switch main && git pull --ff-only
   sha=$(gh pr view <release-pr-number> --json mergeCommit --jq .mergeCommit.oid)
   git log -1 --oneline "$sha"   # must be the "Version X.Y.Z" commit
   git tag -a vX.Y.Z "$sha" -m "vX.Y.Z"
   git push origin vX.Y.Z
   ```

7. Create the GitHub release from the tag (none exist for past versions; start with this one):

   ```bash
   gh release create vX.Y.Z --title vX.Y.Z --notes-file .changes/vX.Y.Z.md
   ```

8. Ask the Go proxy to index it, then check pkg.go.dev shows the version:

   ```bash
   GOPROXY=https://proxy.golang.org go list -m github.com/Backblaze/blazer@vX.Y.Z
   ```

## Never

- Move or delete a published tag. The Go checksum database has already recorded it; release a new patch
  version instead.
- Release from a branch other than `main`.
- Edit a released `.changes/vX.Y.Z.md` without a reason recorded in the PR.
