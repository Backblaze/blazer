#!/usr/bin/env bash
# Tests that scripts/docs-consistency_test.sh fails when a required file is missing, and skips only when
# DOCS_CONSISTENCY_ALLOW_PARTIAL=1 opts in. Runs on a scratch copy, so the real tree is never touched.
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d) || exit 1
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/scripts"
cp "$here/docs-consistency_test.sh" "$tmp/scripts/"
fail=0

# Every required file exists except docs/upgrading.md.
for f in CHANGELOG.md README.md examples/simple/simple.go bin/b2keys/b2keys.go base/base.go; do
  mkdir -p "$tmp/$(dirname "$f")"
  : >"$tmp/$f"
done

out=$("$tmp/scripts/docs-consistency_test.sh" 2>&1)
rc=$?
if [ "$rc" -eq 0 ] || ! grep -q 'docs/upgrading.md' <<<"$out"; then
  echo "FAIL: default run with docs/upgrading.md missing exited $rc, want non-zero naming the file: $out"
  fail=1
else
  echo "ok   default run fails when a required file is missing"
fi

out=$(DOCS_CONSISTENCY_ALLOW_PARTIAL=1 "$tmp/scripts/docs-consistency_test.sh" 2>&1)
rc=$?
if [ "$rc" -ne 0 ] || ! grep -q '^SKIP' <<<"$out"; then
  echo "FAIL: DOCS_CONSISTENCY_ALLOW_PARTIAL=1 run exited $rc, want 0 with SKIP: $out"
  fail=1
else
  echo "ok   DOCS_CONSISTENCY_ALLOW_PARTIAL=1 skips on a partial checkout"
fi
exit $fail
