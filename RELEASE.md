# Releasing Blazer

Blazer is a Go module: a release is a `Version X.Y.Z` commit on `main` and a `vX.Y.Z` tag on that commit.
The changelog is assembled from `.changes/unreleased` fragments with [changie](https://changie.dev).

## What a release commit contains

Release commits update these files. The supported security line changes when the minor version changes:

| File | Change | Done by |
| --- | --- | --- |
| `CHANGELOG.md` | New `## [X.Y.Z] - YYYY-MM-DD` section and its compare link | `changie batch` + `changie merge` |
| `base/base.go` | `DefaultUserAgent = "blazer/X.Y.Z"` (sent on every request) | `changie merge` (replacement in `.changie.yaml`) |
| `bin/b2keys/go.mod` | `require github.com/Backblaze/blazer vX.Y.Z` | `changie merge` (replacement in `.changie.yaml`) |
| `SECURITY.md` | Supported minor release line in the text and table | Manually when the supported minor line changes |

Past releases were pushed straight to `main`. From now on, the release commit goes through a PR like any
other change. CI and a code owner see the release commit before tagging.

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
| Repository security advisories | An advisory is in triage or draft that this release does not fix | Fixing it in this release (see below) |
| Secret scanning | Any alert is open | Revoking the secret, then closing the alert |

Alerts with no patched version yet do not hold the release. The gate lists them as notes. If the script
cannot read a section (for example secret scanning without security access), the gate reports it as UNVERIFIED.
That section must be checked by hand on the Security tab. The gate never passes it silently.

Every release aims to clear the gate, not just pass it at the last minute. When a release is planned, open
an issue in that release's milestone for each finding that is already public (for example a Dependabot alert
for a published CVE). Never open a public issue for a repository advisory or a code scanning security alert:
track those in the private advisory, so an unfixed vulnerability is not disclosed early. A finding that
cannot be fixed in time needs a code owner's written dismissal reason on the alert.

A draft advisory fixed by the pending release stays private through tagging. List its id in
`RELEASE_FIXES_ADVISORIES=GHSA-xxxx-xxxx-xxxx make release-check`. The gate reports the advisory instead of
holding. The code owner approving the release PR confirms the fix is in the release. After pushing the tag,
publish the advisory right away (step 7). The gate prints only advisory ids, never summaries.

## Release tag signing

Release tags must be annotated and SSH-signed by a key enrolled in [`MAINTAINERS`](MAINTAINERS).
This file is an OpenSSH `allowed_signers` file. Unsigned tags, lightweight tags and unenrolled keys fail the
release workflow. SSH signing requires Git 2.34+ and OpenSSH with `ssh-keygen -Y` support.
OpenPGP and X.509 signatures are not enrolled by this workflow.

Generate a dedicated signing key locally (choose a strong passphrase; never upload the private key):

```bash
ssh-keygen -t ed25519 -C 'your-email@example.com' -f ~/.ssh/blazer-release
ssh-keygen -lf ~/.ssh/blazer-release.pub
```

Before the first signed release, a maintainer must [add the public key to their GitHub account](https://docs.github.com/en/authentication/connecting-to-github-with-ssh/adding-a-new-ssh-key-to-your-github-account)
with key type **Signing key**, then open a separate enrollment PR adding that same **public** key to
`MAINTAINERS`, for example:

```text
your-email@example.com namespaces="git" ssh-ed25519 <base64-field-from-the-public-key>
```

A human code owner must independently confirm the fingerprint with the maintainer before approving.
Key enrollment is a maintainer action, outside this release-tooling change. The allowlist ships empty
and fails closed until that enrollment PR is merged; GitHub registration alone does not authorize a
release in this workflow. This does not affect PR CI: release verification runs only on tag pushes,
and its automated tests use ephemeral keys in isolated repositories. To rotate or
revoke a key, update the allowlist through the same review process; CI always uses the current `main`
allowlist, never one supplied by the release tag. Do not enroll a disposable test key.

Configure signing in your checkout and load the passphrase-protected key into your SSH agent:

```bash
git config gpg.format ssh
git config user.signingkey ~/.ssh/blazer-release.pub
git config gpg.ssh.allowedSignersFile "$(pwd)/MAINTAINERS"
ssh-add ~/.ssh/blazer-release
```

The tag workflow validates the exact tag object from the push. It verifies the signature with
`git verify-tag` and requires the release commit to be on `main`. It checks that the versioned notes
exist and `DefaultUserAgent` matches the tag. The workflow repeats verification immediately before publishing.
`gh release create --verify-tag` checks tag existence, not its signature; it does not replace this gate.

Key enrollment and release-workflow changes are security-sensitive and require code-owner review.
Repository administrators should use a tag ruleset to restrict creation of `v*` tags to releasers.
They should also prohibit tag updates and deletion. The workflow gates GitHub Release publication.
It cannot undo a pushed Go module tag or prevent a privileged actor from bypassing Actions. Verify locally before pushing.

## Local health check

Before tagging, run the repository-owned golden path against a fresh loopback simulator with an
existing scratch bucket. Start/provision it as described in the [harness README](.sdkharness/README.md),
then run from the Blazer checkout:

```bash
SDKHARNESS_TEST_LEVEL=health SDKHARNESS_SCENARIO=golden-path \
SDKHARNESS_SIMULATOR_URL="http://127.0.0.1:<port>" B2_BUCKET_NAME=sdkharness-healthcheck \
  .sdkharness/tests/run-health
```

Require `SDKHARNESS_RESULT` with level `health`, scenario `golden-path` and verdict `PASS`.
`FAIL` or `SKIP` is not a successful health check. The dispatcher uses fixed simulator credentials,
rejects non-loopback endpoints and clears ambient B2 credentials; this is not a live-account check.

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

4. Check the diff covers the files above as needed plus `.changes/`, and explain any other release-process
   changes in the PR. Edit `.changes/vX.Y.Z.md` for readability if needed and run `merge` again.
   Confirm `DefaultUserAgent = "blazer/X.Y.Z"` and the `bin/b2keys/go.mod` requirement match the release;
   `.changie.yaml` already updates both. The tag workflow rejects a stale User-Agent instead of
   modifying a published tag. Run `make ci` and the local health check above.
5. Commit as `Version X.Y.Z` and open a PR with the changelog section and the `make release-check` output as
   its body; leave out any advisory ids until they are published. A code owner approves.
6. After it merges (`main` requires linear history, so squash or rebase), tag the release PR's merge
   commit, not the tip of `main`, which may already include later PRs:

   ```bash
   git switch main && git pull --ff-only
   sha=$(gh pr view <release-pr-number> --json mergeCommit --jq .mergeCommit.oid)
   git log -1 --oneline "$sha"   # must be the "Version X.Y.Z" commit
   git tag -s vX.Y.Z "$sha" -m "vX.Y.Z"
   git fetch origin main
   scripts/verify-release-tag.sh vX.Y.Z "$(git rev-parse refs/tags/vX.Y.Z)"
   git push origin vX.Y.Z
   ```

7. The [Release workflow](.github/workflows/release.yml) runs on the tag push. Its read-only
   `verify-tag` job must pass before the separate `publish` job creates the GitHub Release with the
   body from the tagged `.changes/vX.Y.Z.md`. No manual `gh release create` is needed. Check that both
   jobs succeed and the Release body matches those notes. If verification fails, resolve the cause;
   never move or replace the published tag. A rerun never edits an existing Release.

   Then publish each advisory listed in `RELEASE_FIXES_ADVISORIES`, naming vX.Y.Z as the patched version.

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
