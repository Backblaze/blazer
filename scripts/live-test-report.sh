#!/usr/bin/env bash
# Summarizes a `go test -json` run of the live tests and fails unless they really ran against B2.
# Usage: scripts/live-test-report.sh test.json
# Writes a Markdown summary to $GITHUB_STEP_SUMMARY when set. Exit 1 when a test failed, when any live test
# skipped for missing credentials, or when no test ran at all.
set -euo pipefail

in=${1:?usage: live-test-report.sh test.json}
summary=${GITHUB_STEP_SUMMARY:-/dev/stdout}

# Top-level tests only; subtests contain a slash.
tests=$(jq -c 'select(.Test != null and (.Test | contains("/") | not) and (.Action == "pass" or .Action == "fail" or .Action == "skip"))' "$in")
count() { grep -c "\"Action\":\"$1\"" <<<"$tests" || true; }
pass=$(count pass)
fail=$(count fail)
skip=$(count skip)
nocreds=$(jq -r 'select(.Action == "output" and (.Output | contains("skipping integration tests"))) | "\(.Package) \(.Test)"' "$in" | sort -u)

{
  echo "## Live integration tests"
  echo
  echo "Commit \`${GITHUB_SHA:-local}\` · trigger \`${GITHUB_EVENT_NAME:-local}\`"
  echo
  echo "| Passed | Failed | Skipped |"
  echo "| --- | --- | --- |"
  echo "| $pass | $fail | $skip |"
  if [ "$fail" -gt 0 ]; then
    echo
    echo "### Failed"
    jq -r 'select(.Action == "fail" and .Test != null) | "- `\(.Package | sub("github.com/Backblaze/blazer/"; ""))` \(.Test)"' "$in" | sort -u
  fi
  if [ -n "$nocreds" ]; then
    echo
    echo "### Skipped for missing credentials (the live tests did NOT run)"
    while IFS= read -r line; do echo "- $line"; done <<<"$nocreds"
  fi
} >>"$summary"

status=0
[ "$fail" -gt 0 ] && { echo "::error::$fail live test(s) failed."; status=1; }
[ -n "$nocreds" ] && { echo "::error::Live tests skipped for missing credentials; they did not run against B2."; status=1; }
[ "$pass" -eq 0 ] && { echo "::error::No test passed; the live suite did not run."; status=1; }
exit "$status"
