#!/usr/bin/env bash
# Compare committed HEAD with its merge-base against BASE_REF (default origin/main).
# Needs git, Go and Ruby (standard-library YAML); no changes to go.mod or the working tree.
# apidiff, rather than gorelease, supports local base..head API snapshots instead of release versions.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
tmp=$(mktemp -d)
report="$tmp/report"
result=FAIL
echo 'Public API: ./b2, ./b2/sync, ./base' >"$report"
finish() {
  printf '\nResult: %s\n' "$result" >>"$report"
  cat "$report"
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    {
      printf '## API compatibility\n\n```text\n'
      cat "$report"
      printf '```\n'
    } >>"$GITHUB_STEP_SUMMARY"
  fi
  rm -rf "$tmp"
}
trap finish EXIT

base=$(git merge-base "${BASE_REF:-origin/main}" HEAD)
printf 'Base: %s\nHEAD: %s\n' "$base" "$(git rev-parse HEAD)" >>"$report"
GOBIN="$tmp/bin" go install golang.org/x/exp/cmd/apidiff@v0.0.0-20261106192419-47ce03d25ef0 >>"$report" 2>&1
apidiff="$tmp/bin/apidiff"

for revision in base head; do
  mkdir "$tmp/$revision"
  ref=HEAD
  [ "$revision" != base ] || ref="$base"
  git archive "$ref" | tar -x -C "$tmp/$revision"
done

incompatible=0
for package in b2 b2/sync base; do
  key=${package//\//-}
  for revision in base head; do
    (cd "$tmp/$revision" && GOWORK=off "$apidiff" -w "$tmp/$key-$revision.api" "./$package") >>"$report" 2>&1
  done
  printf '\n./%s:\n' "$package" >>"$report"
  "$apidiff" "$tmp/$key-base.api" "$tmp/$key-head.api" >>"$report" 2>&1
  # apidiff exits zero even when it reports incompatible changes.
  "$apidiff" -incompatible "$tmp/$key-base.api" "$tmp/$key-head.api" >"$tmp/raw-incompatible" 2>>"$report"
  # A release commit bumps the DefaultUserAgent version string by design (see
  # RELEASE.md). apidiff flags the const value change as incompatible, but the
  # string is telemetry callers must not pin to, so it is not a real break and
  # would otherwise block every release PR. See #375.
  grep -vxE '(- )?DefaultUserAgent: value changed from "blazer/[^"]*" to "blazer/[^"]*"' \
    "$tmp/raw-incompatible" >"$tmp/incompatible" || true
  if [ -s "$tmp/incompatible" ]; then incompatible=1; fi
done

if [ "$incompatible" -eq 0 ]; then
  result='PASS: no incompatible public API changes.'
  exit 0
fi

git diff --name-only --diff-filter=A -z "$base" HEAD -- '.changes/unreleased/*.yaml' >"$tmp/fragments"
while IFS= read -r -d '' fragment; do
  if git show "HEAD:$fragment" | ruby -ryaml -rdate -e '
    change = YAML.safe_load(STDIN.read, permitted_classes: [Date, Time])
    exit(change.is_a?(Hash) && change["body"].is_a?(String) && change["body"].include?("**Breaking:**") ? 0 : 1)
  '; then
    result="PASS: incompatible changes documented in $fragment."
    exit 0
  fi
done <"$tmp/fragments"

echo 'Incompatible API changes require a new .changes/unreleased/*.yaml fragment whose body contains **Breaking:**.' >>"$report"
exit 1
