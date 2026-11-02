#!/usr/bin/env bash
# Tests scripts/live-failure-issue.sh against a fake gh that records its calls.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

cat >"$tmp/gh" <<'EOF'
#!/usr/bin/env bash
echo "$*" >>"$GH_CALLS"
if [ "$1 $2" = "issue list" ]; then
  # Like GitHub, honor --author (filter) and --jq (post-process).
  author="" filter="."
  while [ $# -gt 0 ]; do
    case $1 in --author) author=$2 ;; --jq) filter=$2 ;; esac
    shift
  done
  jq --arg a "$author" '[.[] | select($a == "" or .author.login == $a)]' "$GH_ISSUES" | jq -r "$filter"
fi
EOF
chmod +x "$tmp/gh"
export PATH="$tmp:$PATH" GH_CALLS="$tmp/calls"

title="Live integration tests are failing on main"
fail=0
check() { # name, issues JSON, expected call
  : >"$GH_CALLS"
  printf '%s' "$2" >"$tmp/issues.json"
  GH_ISSUES="$tmp/issues.json" "$here/live-failure-issue.sh" "run failed" >/dev/null
  if grep -qF -- "$3" "$GH_CALLS"; then
    echo "ok   $1"
  else
    echo "FAIL $1: want a call containing '$3', got:"; sed 's/^/       /' "$GH_CALLS"; fail=1
  fi
}

check "comments on the workflow's own issue" \
  "[{\"number\":7,\"title\":\"$title\",\"author\":{\"login\":\"app/github-actions\"}}]" \
  "issue comment 7 "
check "ignores an issue with the same title opened by someone else" \
  "[{\"number\":3,\"title\":\"$title\",\"author\":{\"login\":\"mallory\"}}]" \
  "issue create --title $title"
check "ignores a longer title that contains the phrase" \
  "[{\"number\":5,\"title\":\"$title (not really)\",\"author\":{\"login\":\"app/github-actions\"}}]" \
  "issue create --title $title"
check "picks the oldest matching issue" \
  "[{\"number\":9,\"title\":\"$title\",\"author\":{\"login\":\"app/github-actions\"}},{\"number\":4,\"title\":\"$title\",\"author\":{\"login\":\"app/github-actions\"}}]" \
  "issue comment 4 "
check "opens an issue when none exists" "[]" "issue create --title $title"

exit "$fail"
