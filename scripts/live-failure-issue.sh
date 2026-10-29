#!/usr/bin/env bash
# Comments on the workflow's own open "live tests failing" issue, or opens one.
# Usage: scripts/live-failure-issue.sh <comment body>   (needs gh with GH_TOKEN and GH_REPO)
# Only issues opened by GitHub Actions with the exact title count, so an issue anyone else opens with the
# same or a longer title cannot capture the report.
set -euo pipefail

body=${1:?usage: live-failure-issue.sh <comment body>}
title="Live integration tests are failing on main"

n=$(gh issue list --state open --author app/github-actions --search "\"$title\" in:title" \
  --limit 100 --json number,title,author |
  jq -r --arg t "$title" \
    '[.[] | select(.title == $t and (.author.login == "app/github-actions" or .author.login == "github-actions"))]
     | sort_by(.number) | .[0].number // empty')

if [ -n "$n" ]; then
  gh issue comment "$n" --body "$body"
else
  gh issue create --title "$title" --body "$body"
fi
