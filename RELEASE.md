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

A major version is never computed by `auto`. `v1.0.0` is a deliberate `changie batch v1.0.0`. From v2 on,
Go requires the module path to change to `github.com/Backblaze/blazer/vN` in the same release, so a major
release is planned work, not a changelog bump. Entries starting with `**Breaking:**` tell the release
manager what callers must change.

## Security and quality gate

A release is held while any solvable security or quality finding is open on the
[Security tab](https://github.com/Backblaze/blazer/security). `make release-check` runs the gate and exits
non-zero to hold:

| Source | Holds the release when | Clear it by |
| --- | --- | --- |
| Code scanning (CodeQL security and quality queries) | Any alert is open on the default branch | Fixing it, or a code owner dismissing it with a written reason |
| Dependabot alerts | An open alert has a patched version | Upgrading the dependency |
| `govulncheck` (both modules) | A reachable known vulnerability is reported | Upgrading the module or Go toolchain |
| Repository security advisories | An advisory is in triage or draft | Fixing it and publishing the advisory with the release |
| Secret scanning | Any alert is open | Revoking the secret, then closing the alert |

Alerts with no patched version yet do not hold the release; the gate lists them as notes. A section the
script cannot read (for example secret scanning without security access) is reported as UNVERIFIED and must
be checked by hand on the Security tab; it never passes silently.

Every release aims to clear the gate, not just pass it at the last minute. When a release is planned, open
an issue for each finding the gate lists and add it to that release's milestone. A finding that cannot be
fixed in time needs a code owner's written dismissal reason on the alert, linked from the release PR.

## Steps

1. Make sure `main` is green and every PR meant for the release has merged. Run `make release-check`;
   if it holds, fix or dismiss the findings first (see the gate above).
2. Branch from `main`: `git switch -c release/vX.Y.Z origin/main`.
3. Batch the fragments and rebuild the changelog and version strings:

   ```bash
   go run github.com/miniscruff/changie@v1.24.0 batch auto --dry-run
   go run github.com/miniscruff/changie@v1.24.0 batch auto
   go run github.com/miniscruff/changie@v1.24.0 merge
   ```

4. Check the diff touches exactly the three files above plus `.changes/`. Edit `.changes/vX.Y.Z.md` for
   readability if needed and run `merge` again. Run `make ci`.
5. Commit as `Version X.Y.Z` and open a PR with the changelog section and the `make release-check` output as
   its body. A code owner approves.
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

- Release while `make release-check` holds, or with an UNVERIFIED section nobody checked by hand.

- Move or delete a published tag. The Go checksum database has already recorded it; release a new patch
  version instead.
- Release from a branch other than `main`.
- Edit a released `.changes/vX.Y.Z.md` without a reason recorded in the PR.
