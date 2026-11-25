#!/usr/bin/env bash
# Check that every make target named by the verify skill exists.
set -euo pipefail

cd "$(dirname "$0")/.."
skill=.claude/skills/verify/SKILL.md
targets=$(grep -oE 'make[[:space:]]+[a-zA-Z0-9][a-zA-Z0-9_-]*' "$skill" | awk '{print $2}' | sort -u)
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
