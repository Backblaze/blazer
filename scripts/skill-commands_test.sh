#!/usr/bin/env bash
# Check that every make target named by the verify skill exists.
set -euo pipefail

cd "$(dirname "$0")/.."
skill=${1:-.claude/skills/verify/SKILL.md}
if [ ! -f "$skill" ]; then
  echo "FAIL verify skill: $skill is missing"
  exit 1
fi
targets=$(grep -oE 'make[[:space:]]+[a-zA-Z0-9][a-zA-Z0-9_-]*' "$skill" | awk '{print $2}' | sort -u || true)
if [ -z "$targets" ]; then
  echo "FAIL verify skill: no make targets found"
  exit 1
fi

while IFS= read -r target; do
  if ! awk -F: -v target="$target" '$1 == target { found=1 } END { exit !found }' Makefile; then
    echo "FAIL verify skill: make target '$target' is missing from Makefile"
    exit 1
  fi
  echo "ok   verify skill: make $target exists"
done <<< "$targets"

expected=$(awk '/^ci:/ { sub(/^ci:[[:space:]]*/, ""); for (i=1; i<=NF; i++) print $i; exit }' Makefile)
actual=$(awk '/^## Stages$/ { in_stages=1; next } /^## / { in_stages=0 } in_stages && /^- `/ { split($0, parts, "`"); print parts[2] }' "$skill")
if [ -z "$expected" ] || [ "$actual" != "$expected" ]; then
  echo "FAIL verify skill: stage list does not match Makefile ci prerequisites"
  printf '  expected: %s\n  actual: %s\n' "$(echo "$expected" | tr '\n' ' ')" "$(echo "$actual" | tr '\n' ' ')"
  exit 1
fi
echo "ok   verify skill: stage list matches Makefile ci prerequisites"
