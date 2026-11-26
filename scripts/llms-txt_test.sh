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
links=$(grep -Eo 'https://github\.com/Backblaze/blazer/blob/[^)#[:space:]]*' llms.txt || true)
if [ -z "$links" ]; then
  echo 'FAIL llms.txt: no repository file links found'
  exit 1
fi
while IFS= read -r link; do
  target=${link#https://github.com/Backblaze/blazer/blob/}
  ref=${target%%/*}
  if [ "$ref" != main ]; then
    echo "FAIL llms.txt: invalid repository ref $ref (expected main)"
    exit 1
  fi
  path=${target#*/}
  if [ "$path" = "$target" ] || [ ! -f "$path" ]; then
    echo "FAIL llms.txt: missing file $path"
    exit 1
  fi
done <<< "$links"
echo 'ok   llms.txt: structure and repository file links'

missing=0
for file in llms.txt AGENTS.md; do
  if ! grep -Fq 'https://github.com/backblaze-labs/b2-mcp' "$file"; then
    echo "FAIL $file: missing b2-mcp link"
    missing=1
  fi
done
if [ "$missing" -ne 0 ]; then
  exit 1
fi
echo 'ok   llms.txt and AGENTS.md: b2-mcp link'
