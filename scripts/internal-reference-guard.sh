#!/usr/bin/env bash
set -euo pipefail

# Maintainers add tracker keys and hostnames to these two lists.
TRACKER_KEYS='B2|DEV|DEVDOCS|DOCS|HD'
INTERNAL_HOSTS='backblaze\.atlassian\.net|atlassian\.net'
pattern="(^|[^[:alnum:]_])(($TRACKER_KEYS)-[0-9]+|($INTERNAL_HOSTS))([^[:alnum:]_]|$)"

mode=${1:?source required}; shift
case "$mode" in
  file) label=$1; input=$1 ;;
  title) label='PR title'; input= ;;
  body) label='PR body'; input= ;;
  commit) label="commit $1"; shift; input= ;;
  *) echo "unknown source: $mode" >&2; exit 2 ;;
esac
if [[ $mode == file ]]; then
  # The guard and its tests contain the patterns by design; do not scan them.
  case "$input" in scripts/internal-reference-guard.sh|scripts/internal-reference-guard_test.sh) exit 0 ;; esac
  [[ -f $input ]] || { echo "cannot read $input" >&2; exit 2; }
  content=$(cat -- "$input")
else
  content=${1-}
fi
# awk reports the exact match and line while preserving the caller's label.
printf '%s\n' "$content" | awk -v pattern="$pattern" -v label="$label" -v file="$mode" '
  { rest = $0; while (match(rest, pattern)) {
      hit = substr(rest, RSTART, RLENGTH)
      sub(/^[^[:alnum:]_]/, "", hit)
      sub(/[^[:alnum:]_]$/, "", hit)
      if (file == "file") printf "%s:%d: %s\n", label, NR, hit
      else printf "%s: %s\n", label, hit
      found = 1
      rest = substr(rest, RSTART + RLENGTH - 1)
      if (length(rest) <= 1) break
    } }
  END { if (found) exit 1 }
'
