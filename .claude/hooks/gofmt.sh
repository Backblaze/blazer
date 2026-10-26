#!/usr/bin/env bash
# PostToolUse: reject an edit that leaves a Go file unformatted; exit 2 hands the message back to Claude.
set -euo pipefail
file=$(jq -r '.tool_input.file_path // empty')
case "$file" in
  *.go) ;;
  *) exit 0 ;;
esac
[ -f "$file" ] || exit 0
if out=$(gofmt -l "$file" 2>&1) && [ -z "$out" ]; then
  exit 0
fi
[ "$out" = "$file" ] && out=""
echo "gofmt: $file is not formatted. Run 'gofmt -w $file' and re-check.${out:+ $out}" >&2
exit 2
