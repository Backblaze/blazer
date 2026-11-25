#!/usr/bin/env bash
set -euo pipefail

if [[ -z ${TRACKER_KEYS:-} || -z ${INTERNAL_HOSTS:-} ]]; then
  echo 'TRACKER_KEYS and INTERNAL_HOSTS must both be set' >&2
  exit 2
fi

mode=${1:?source required}; shift
case "$mode" in
  file) label=$1; input=$1 ;;
  title) label='PR title'; input= ;;
  body) label='PR body'; input= ;;
  commit) label="commit $1"; shift; input= ;;
  *) echo "unknown source: $mode" >&2; exit 2 ;;
esac
if [[ $mode == file ]]; then
  [[ -f $input ]] || { echo "cannot read $input" >&2; exit 2; }
  content=$(cat -- "$input")
else
  content=${1-}
fi
# awk reports the exact match and line while preserving the caller's label.
printf '%s\n' "$content" | awk -v label="$label" -v file="$mode" '
  BEGIN {
    key_pattern = "(^|[^[:alnum:]])(" ENVIRON["TRACKER_KEYS"] ")-[0-9]+([^[:alnum:]]|$)"
    host_pattern = "(^|[^[:alnum:]])(" ENVIRON["INTERNAL_HOSTS"] ")([^[:alnum:]]|$)"
  }
  function scan(line, pattern, fold,    rest, text, hit) {
    rest = line
    while (length(rest) > 0) {
      text = fold ? tolower(rest) : rest
      if (!match(text, pattern)) break
      hit = substr(rest, RSTART, RLENGTH)
      sub(/^[^[:alnum:]]/, "", hit)
      sub(/[^[:alnum:]]$/, "", hit)
      if (file == "file") printf "%s:%d: %s\n", label, NR, hit
      else printf "%s: %s\n", label, hit
      found = 1
      rest = substr(rest, RSTART + RLENGTH - 1)
      if (length(rest) <= 1) break
    }
  }
  { scan($0, key_pattern, 0); scan($0, host_pattern, 1) }
  END { if (found) exit 1 }
'
