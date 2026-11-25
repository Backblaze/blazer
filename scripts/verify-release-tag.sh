#!/usr/bin/env bash
# Verify the pushed tag against reviewed main, never a tag-supplied trust list.
set -euo pipefail

if { [ "$#" != 2 ] && [ "$#" != 3 ]; } || [[ ! "$1" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo 'usage: verify-release-tag.sh vX.Y.Z expected-tag-object [expected-push-commit]' >&2
  exit 1
fi
tag=$1
ref="refs/tags/$tag"
if [ "$(git rev-parse "$ref")" != "$2" ]; then
  echo 'tag object changed since the triggering push' >&2
  exit 1
fi
if [ "$(git cat-file -t "$ref")" != tag ]; then
  echo 'annotated tag required' >&2
  exit 1
fi
if [ "$#" = 3 ] && [ "$(git rev-parse "$ref^{commit}")" != "$3" ]; then
  echo 'tagged commit differs from the triggering push' >&2
  exit 1
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
git show origin/main:MAINTAINERS > "$tmp/signers"
# SSH only: an unrelated OpenPGP/X.509 keyring must never authorize a release.
git -c gpg.ssh.allowedSignersFile="$tmp/signers" \
  -c gpg.ssh.program=ssh-keygen -c gpg.openpgp.program=false -c gpg.x509.program=false \
  verify-tag "$ref"
if ! git merge-base --is-ancestor "$ref^{commit}" origin/main; then
  echo 'release commit is not on origin/main' >&2
  exit 1
fi
if ! git show "$ref:.changes/$tag.md" > "$tmp/notes" || [ ! -s "$tmp/notes" ]; then
  echo 'missing or empty release notes' >&2
  exit 1
fi
user_agent=$(git show "$ref:base/base.go" | sed -n 's/^[[:space:]]*DefaultUserAgent = "\(.*\)"$/\1/p')
if [ "$user_agent" != "blazer/${tag#v}" ]; then
  echo 'DefaultUserAgent does not match the release tag; run changie merge' >&2
  exit 1
fi
printf 'Verified %s (%s), release notes and DefaultUserAgent\n' "$tag" "$2"
