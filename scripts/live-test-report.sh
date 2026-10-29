#!/usr/bin/env bash
# Summarizes a `go test -json` run of the live tests and fails unless they really ran against B2.
# Usage: scripts/live-test-report.sh test.json
# Writes a Markdown summary to $GITHUB_STEP_SUMMARY when set. Exit 1 when a test failed, when any test
# skipped, or when a test defined in a live test file did not pass.
set -euo pipefail

in=${1:?usage: live-test-report.sh test.json}
summary=${GITHUB_STEP_SUMMARY:-/dev/stdout}
module=github.com/Backblaze/blazer
# Every top-level test in these files must run and pass; there is no other way to tell they reached B2.
live_files=(b2/integration_test.go b2/lock_test.go base/integration_test.go)

required=$(for f in "${live_files[@]}"; do
  grep -oE '^func Test[A-Za-z0-9_]*\(t \*testing\.T\)' "$f" |
    sed -E "s#^func (Test[A-Za-z0-9_]*).*#$module/$(dirname "$f") \1#"
done | sort -u)
passed=$(jq -r 'select(.Action == "pass" and .Test != null) | "\(.Package) \(.Test)"' "$in" | sort -u)
missing=$(comm -23 <(echo "$required") <(echo "$passed"))
skipped=$(jq -r 'select(.Action == "skip" and .Test != null) | "\(.Package) \(.Test)"' "$in" | sort -u)
failed=$(jq -r 'select(.Action == "fail" and .Test != null) | "\(.Package) \(.Test)"' "$in" | sort -u)
# A package that crashes or times out fails with no Test field and may leave no failed test behind.
failed_pkgs=$(jq -r 'select(.Action == "fail" and .Test == null) | .Package' "$in" | sort -u)

# Top-level tests only; subtests contain a slash.
top() { jq -c "select(.Test != null and (.Test | contains(\"/\") | not) and .Action == \"$1\")" "$in" | grep -c . || true; }
nreq=$(grep -c . <<<"$required" || true)
nlive=$((nreq - $(grep -c . <<<"$missing" || true)))

list() {
  [ -z "$2" ] && return
  echo
  echo "### $1"
  while IFS= read -r line; do echo "- \`${line#"$module"/}\`"; done <<<"$2"
}

{
  echo "## Live integration tests"
  echo
  echo "Commit \`${GITHUB_SHA:-local}\` · trigger \`${GITHUB_EVENT_NAME:-local}\`"
  echo
  echo "| Live tests passed | Passed | Failed | Skipped |"
  echo "| --- | --- | --- | --- |"
  echo "| $nlive of $nreq | $(top pass) | $(top fail) | $(top skip) |"
  list "Failed" "$failed"
  list "Failed packages" "$failed_pkgs"
  list "Skipped (no test may skip in the live run)" "$skipped"
  list "Live tests that did not pass" "$missing"
} >>"$summary"

status=0
[ -n "$failed" ] && { echo "::error::Tests failed in the live run."; status=1; }
[ -n "$failed_pkgs" ] && { echo "::error::Packages failed in the live run (crash, timeout or build error)."; status=1; }
[ -n "$skipped" ] && { echo "::error::Tests skipped in the live run; a skipped live test did not reach B2."; status=1; }
[ -n "$missing" ] && { echo "::error::$((nreq - nlive)) of $nreq live tests did not pass."; status=1; }
[ "$nreq" -eq 0 ] && { echo "::error::No live tests found in ${live_files[*]}."; status=1; }
exit "$status"
