#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
if ! cmp -s .claude/skills/b2keys/SKILL.md bin/b2keys/skill/SKILL.md; then
  echo "FAIL: copy the repository skill to bin/b2keys/skill/SKILL.md"
  exit 1
fi
echo "ok   b2keys embedded skill matches the repository skill"
