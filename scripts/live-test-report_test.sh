#!/usr/bin/env bash
# Tests scripts/live-test-report.sh against a small `go test -json` fixture.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
cd "$root"

# Every top-level test of the live files, all passing.
live=$(for f in b2/integration_test.go b2/lock_test.go base/integration_test.go; do
  grep -oE '^func Test[A-Za-z0-9_]*\(t \*testing\.T\)' "$f" |
    sed -E "s#^func (Test[A-Za-z0-9_]*).*#$(dirname "$f") \1#"
done)
ev() { printf '{"Action":"%s","Package":"github.com/Backblaze/blazer/%s","Test":"%s","Elapsed":0.5}\n' "$1" "$2" "$3"; }
while read -r dir name; do ev pass "$dir" "$name"; done <<<"$live" >"$tmp/pass.json"
echo '{"Action":"pass","Package":"github.com/Backblaze/blazer/b2","Elapsed":12.5}' >>"$tmp/pass.json"

fail=0
check() { # name, expected exit, expected log text, json file
  : >"$tmp/summary.md"
  set +e
  out=$(GITHUB_STEP_SUMMARY="$tmp/summary.md" "$here/live-test-report.sh" "$4" 2>&1)
  code=$?
  set -e
  if [ "$code" = "$2" ] && grep -qF -- "$3" <<<"$out"; then
    echo "ok   $1"
  else
    echo "FAIL $1: exit $code, want $2 with '$3'"; sed 's/^/       /' <<<"$out"; fail=1
  fi
}

check "prints the summary to the log" 0 "Live integration tests" "$tmp/pass.json"
if grep -qF "Live integration tests" "$tmp/summary.md"; then echo "ok   writes the summary file too"; else echo "FAIL summary file is empty"; fail=1; fi
check "prints the per-package result" 0 "PASS b2 (12.5s)" "$tmp/pass.json"

cp "$tmp/pass.json" "$tmp/fail.json"
{
  printf '%s\n' '{"Action":"output","Package":"github.com/Backblaze/blazer/b2","Test":"TestAttrs","Output":"    attrs_test.go:9: boom\n"}'
  ev fail b2 TestAttrs
} >>"$tmp/fail.json"
check "prints the output of a failed test" 1 "attrs_test.go:9: boom" "$tmp/fail.json"

# A passing test whose name is a prefix of a failing one must not have its output printed.
cp "$tmp/pass.json" "$tmp/prefix.json"
{
  printf '%s\n' '{"Action":"output","Package":"github.com/Backblaze/blazer/b2","Test":"TestAttrs","Output":"PASSING-OUTPUT\n"}'
  printf '%s\n' '{"Action":"output","Package":"github.com/Backblaze/blazer/b2","Test":"TestAttrsLong","Output":"FAILING-OUTPUT\n"}'
  ev fail b2 TestAttrsLong
} >>"$tmp/prefix.json"
set +e
out=$(GITHUB_STEP_SUMMARY="$tmp/summary.md" "$here/live-test-report.sh" "$tmp/prefix.json" 2>&1)
set -e
if grep -qF FAILING-OUTPUT <<<"$out" && ! grep -qF PASSING-OUTPUT <<<"$out"; then
  echo "ok   prints only failed tests' output"
else
  echo "FAIL exact test-name matching"; sed 's/^/       /' <<<"$out"; fail=1
fi

# A timeout: Go attributes the stack trace to the running test and emits only a package failure.
cp "$tmp/pass.json" "$tmp/timeout.json"
{
  printf '%s\n' '{"Action":"run","Package":"github.com/Backblaze/blazer/b2","Test":"TestHangs"}'
  printf '%s\n' '{"Action":"output","Package":"github.com/Backblaze/blazer/b2","Test":"TestHangs","Output":"panic: test timed out after 30m0s\n"}'
  printf '%s\n' '{"Action":"output","Package":"github.com/Backblaze/blazer/b2","Test":"TestAttrs","Output":"UNRELATED-OUTPUT\n"}'
  printf '%s\n' '{"Action":"fail","Package":"github.com/Backblaze/blazer/b2","Elapsed":1800}'
} >>"$tmp/timeout.json"
check "prints the timeout reason of a cut-off test" 1 "panic: test timed out after 30m0s" "$tmp/timeout.json"
set +e
out=$(GITHUB_STEP_SUMMARY="$tmp/summary.md" "$here/live-test-report.sh" "$tmp/timeout.json" 2>&1)
set -e
if grep -qF UNRELATED-OUTPUT <<<"$out"; then echo "FAIL prints output of a passing test in a failed package"; fail=1; else echo "ok   skips finished tests in a failed package"; fi

# More than 300 lines of output must still reach the closing group and the error line.
cp "$tmp/pass.json" "$tmp/long.json"
# Enough output to fill the pipe, so a `head` that exits early would SIGPIPE the writer.
awk 'BEGIN { pad = sprintf("%80s", ""); for (i = 1; i <= 5000; i++) printf "{\"Action\":\"output\",\"Package\":\"github.com/Backblaze/blazer/b2\",\"Test\":\"TestAttrs\",\"Output\":\"line %d%s\\n\"}\n", i, pad }' >>"$tmp/long.json"
ev fail b2 TestAttrs >>"$tmp/long.json"
check "keeps the error line after long failure output" 1 "::error::Tests failed in the live run." "$tmp/long.json"

exit "$fail"
