#!/usr/bin/env bash
# Exercise real Git/SSH verification with disposable keys, never the releaser's keys.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
# Ignore ambient signing settings, hooks and identities.
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null LC_ALL=C
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE
ssh-keygen -q -t ed25519 -N '' -C test-releaser -f "$tmp/enrolled"
ssh-keygen -q -t ed25519 -N '' -C test-outsider -f "$tmp/outsider"
git init -q "$tmp/repo"
cd "$tmp/repo"
git config user.name 'Release test'
git config user.email release-test@example.invalid
git config gpg.format ssh
git config user.signingkey "$tmp/enrolled"
printf 'test@example.invalid namespaces="git" %s\n' "$(cat "$tmp/enrolled.pub")" > MAINTAINERS
mkdir -p .changes base
printf '## Release notes\n\nFixture release.\n' > .changes/v1.2.3.md
printf '\tDefaultUserAgent = "blazer/1.2.3"\n' > base/base.go
git add .
git commit -qm 'Release fixture'
release_commit=$(git rev-parse HEAD)
git update-ref refs/remotes/origin/main "$release_commit"
git tag -s v1.2.3 -m v1.2.3
tag_object=$(git rev-parse refs/tags/v1.2.3)

fail=0
# GitHub documents github.sha as the tip commit for a push event.
if [ "$(grep -Fc 'PUSH_COMMIT: ${{ github.sha }}' "$here/../.github/workflows/release.yml")" = 2 ]; then
  echo 'ok   both jobs use the documented push commit context'
else
  echo 'FAIL both jobs must use github.sha for the push commit'; fail=1
fi
check() { # name, expected status, expected diagnostic
  local name=$1 expected=$2 diagnostic=$3 rc=0
  "$here/verify-release-tag.sh" v1.2.3 "$tag_object" >"$tmp/output" 2>&1 || rc=$?
  if { [ "$expected" = pass ] && [ "$rc" = 0 ]; } ||
     { [ "$expected" = fail ] && [ "$rc" != 0 ] && grep -Fq "$diagnostic" "$tmp/output"; }; then
    printf 'ok   %s (gate exit %s)\n' "$name" "$rc"
    sed 's/^/     /' "$tmp/output"
  else
    printf 'FAIL %s: expected %s (%s), got exit %s\n' "$name" "$expected" "$diagnostic" "$rc"
    cat "$tmp/output"
    fail=1
  fi
}
retag() {
  git tag -d v1.2.3 >/dev/null
  git tag "$@" v1.2.3 -m v1.2.3
  tag_object=$(git rev-parse refs/tags/v1.2.3)
}

check 'enrolled signed release tag' pass ''
# A push event supplies the tagged commit, not the annotated tag object.
if "$here/verify-release-tag.sh" v1.2.3 "$tag_object" "$release_commit" >"$tmp/output" 2>&1; then
  echo 'ok   push event commit matches signed tag'
else
  echo 'FAIL push event commit matches signed tag'; cat "$tmp/output"; fail=1
fi
if "$here/verify-release-tag.sh" v1.2.3 "$tag_object" "$(git hash-object --stdin </dev/null)" >"$tmp/output" 2>&1; then
  echo 'FAIL mismatched push event commit accepted'; fail=1
elif grep -Fq 'tagged commit differs from the triggering push' "$tmp/output"; then
  echo 'ok   mismatched push event commit rejected'
else
  echo 'FAIL mismatched push event commit rejected for wrong reason'; cat "$tmp/output"; fail=1
fi
retag -a
check 'unsigned annotated tag rejected' fail 'no signature found'
git tag -d v1.2.3 >/dev/null
git tag v1.2.3
tag_object=$(git rev-parse refs/tags/v1.2.3)
check 'lightweight tag rejected' fail 'annotated tag required'
retag -s -u "$tmp/outsider"
check 'unenrolled signer rejected' fail 'No principal matched'

# An attacker cannot enroll a key in the tag they want to publish.
printf 'attacker@example.invalid %s\n' "$(cat "$tmp/outsider.pub")" >> MAINTAINERS
git add MAINTAINERS
git commit -qm 'Self-enroll outsider only on tagged branch'
retag -s -u "$tmp/outsider"
check 'tag-local key enrollment rejected' fail 'No principal matched'
git reset -q --hard "$release_commit"

retag -s
if "$here/verify-release-tag.sh" v1.2.3 "$release_commit" >"$tmp/output" 2>&1; then
  echo 'FAIL moved tag accepted'; fail=1
elif grep -Fq 'tag object changed' "$tmp/output"; then
  echo 'ok   moved tag rejected'
else
  echo 'FAIL moved tag rejected for wrong reason'; cat "$tmp/output"; fail=1
fi

printf '\tDefaultUserAgent = "blazer/1.2.2"\n' > base/base.go
git add .
git commit -qm 'Stale release version'
git update-ref refs/remotes/origin/main HEAD
retag -s
check 'stale User-Agent rejected' fail 'DefaultUserAgent does not match'
git reset -q --hard "$release_commit"
rm .changes/v1.2.3.md
git add -u
git commit -qm 'Missing release notes'
git update-ref refs/remotes/origin/main HEAD
retag -s
check 'missing release notes rejected' fail 'missing or empty release notes'
git reset -q --hard "$release_commit"
git update-ref refs/remotes/origin/main HEAD
printf 'off-main\n' > extra
git add extra
git commit -qm 'Unmerged commit'
retag -s
check 'release outside main rejected' fail 'release commit is not on origin/main'

exit "$fail"
