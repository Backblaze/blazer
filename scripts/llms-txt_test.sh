#!/usr/bin/env bash
# Check the index structure and its repository file links.
set -euo pipefail
cd "$(dirname "$0")/.."

if [ ! -f llms.txt ]; then
  echo 'FAIL llms.txt: file not found'
  exit 1
fi
if ! head -n 1 llms.txt | grep -q '^# [^#]'; then
  echo 'FAIL llms.txt: first line must be an H1'
  exit 1
fi
if ! awk '/^## / {section=1; exit} /^> / {found=1} END {exit !(found && section)}' llms.txt; then
  echo 'FAIL llms.txt: blockquote must precede the first H2'
  exit 1
fi
paths=$(grep -Eo 'https://github\.com/Backblaze/blazer/blob/main/[^)#[:space:]]+' llms.txt | sed 's@^https://github.com/Backblaze/blazer/blob/main/@@' || true)
if [ -z "$paths" ]; then
  echo 'FAIL llms.txt: no repository file links found'
  exit 1
fi
while IFS= read -r path; do
  if [ ! -f "$path" ]; then
    echo "FAIL llms.txt: missing file $path"
    exit 1
  fi
done <<< "$paths"
echo 'ok   llms.txt: structure and repository file links'
