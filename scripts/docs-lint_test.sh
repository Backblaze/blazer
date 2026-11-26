#!/usr/bin/env bash
set -euo pipefail
script=$(cd "$(dirname "$0")" && pwd)/docs-lint.sh
makefile=$(cd "$(dirname "$0")/.." && pwd)/Makefile
grep -E '^\.PHONY:.*(^|[[:space:]])docs-lint([[:space:]]|$)' "$makefile" >/dev/null || { echo 'FAIL docs-lint is not phony'; exit 1; }
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
cd "$tmp"
git init -q
git config user.name Test
git config user.email test@example.invalid
mkdir docs
printf 'baseline\n' > baseline
cat > ARCHITECTURE.md <<'DOC'
---
type: Library
owner: team
status: active
stale_after: 2999-01-01T00:00:00Z
---
# Architecture
DOC
git add baseline ARCHITECTURE.md
git commit -qm baseline
base=$(git rev-parse HEAD)
make_doc() {
  cat > "docs/$1.md" <<DOC
---
type: guide
owner: team
status: active
stale_after: $2
---
# Test
$3
DOC
}
make_doc good 2999-01-01T00:00:00Z '[baseline](../baseline)'
"$script" --base "$base" --strict >out 2>&1 || { cat out; echo 'FAIL good doc'; exit 1; }
echo 'ok   valid changed doc'
printf 'file\n' > 'docs/a file.txt'
make_doc good 2999-01-01T00:00:00Z '[file](a%20file.txt)'
"$script" --base "$base" --strict >out 2>&1 || { cat out; echo 'FAIL encoded link'; exit 1; }
echo 'ok   encoded relative link'
rm 'docs/a file.txt'
make_doc good 2999-01-01T00:00:00Z '[baseline](../baseline)'

make_doc missing 2999-01-01T00:00:00Z ''
sed -i.bak '/^owner:/d' docs/missing.md
rm docs/missing.md.bak
if "$script" --base "$base" --strict >out 2>&1; then echo 'FAIL missing field accepted'; exit 1; fi
grep -q 'owner' out || { cat out; exit 1; }
echo 'ok   missing field rejected'
make_doc missing 2999-01-01T00:00:00Z ''
sed -i.bak 's/^owner: team$/owner: ""/' docs/missing.md
rm docs/missing.md.bak
if "$script" --base "$base" --strict >out 2>&1; then echo 'FAIL empty field accepted'; exit 1; fi
grep -q 'owner' out || { cat out; exit 1; }
echo 'ok   empty field rejected'
rm docs/missing.md
make_doc broken 2999-01-01T00:00:00Z '[bad](missing.md)'
if "$script" --base "$base" --strict >out 2>&1; then echo 'FAIL broken link accepted'; exit 1; fi
grep -q 'missing.md' out || { cat out; exit 1; }
"$script" --base "$base" --warn-only >out 2>&1 || { cat out; echo 'FAIL warning mode'; exit 1; }
grep -i -q warning out || { cat out; exit 1; }
echo 'ok   broken link rejected; warning mode succeeds'
rm docs/broken.md
make_doc stale 2000-01-01T00:00:00Z ''
"$script" --base "$base" --strict >out 2>&1 || { cat out; echo 'FAIL stale doc'; exit 1; }
grep -i -q 'warning.*stale' out || { cat out; exit 1; }
echo 'ok   stale date warns without failure'
rm docs/stale.md
git add docs/good.md
git commit -qm 'add good document'
base=$(git rev-parse HEAD)
printf 'invalid old doc\n' > docs/old.md
git add docs/old.md
git commit -qm 'add legacy document'
base=$(git rev-parse HEAD)
printf '\nNew prose.\n' >> docs/good.md
"$script" --base "$base" --strict >out 2>&1 || { cat out; echo 'FAIL unchanged doc checked'; exit 1; }
echo 'ok   unchanged legacy doc ignored'
printf '\n[bad](missing.md)\n' >> docs/good.md
git add docs/good.md
if "$script" --base "$base" --strict >out 2>&1; then echo 'FAIL staged broken link accepted'; exit 1; fi
grep -q 'missing.md' out || { cat out; exit 1; }
echo 'ok   staged changed doc checked'
git reset -q HEAD -- docs/good.md
git checkout -- docs/good.md
printf '\n[ref]: absent.md\n' >> docs/good.md
if "$script" --base "$base" --strict >out 2>&1; then echo 'FAIL broken reference link accepted'; exit 1; fi
grep -q 'absent.md' out || { cat out; exit 1; }
echo 'ok   broken reference link rejected'
git checkout -- docs/good.md
cat >> docs/good.md <<'DOC'
````
```
````
[bad](missing.md)
DOC
if "$script" --base "$base" --strict >out 2>&1; then echo 'FAIL mismatched fence concealed broken link'; exit 1; fi
grep -q 'missing.md' out || { cat out; exit 1; }
echo 'ok   fence length'
git checkout -- docs/good.md
cat > ARCHITECTURE.md <<'DOC'
---
type: Library
owner: team
status: active
stale_after: 2999-01-01T00:00:00Z
---
# Architecture
[bad](missing.md)
DOC
if "$script" --base "$base" --strict >out 2>&1; then echo 'FAIL root doc skipped'; exit 1; fi
grep -q 'missing.md' out || { cat out; exit 1; }
echo 'ok   root doc checked'
git checkout -- ARCHITECTURE.md
git checkout -- docs/good.md
mkdir -p 'docs/product specs'
make_doc 'product specs/a b' 2999-01-01T00:00:00Z ''
printf '\n[spec](<product specs/a b.md>)\n' >> docs/good.md
"$script" --base "$base" --strict >out 2>&1 || { cat out; echo 'FAIL angle destination'; exit 1; }
echo 'ok   angle destination with spaces'
git checkout -- docs/good.md
make_doc 'notes_(v2)' 2999-01-01T00:00:00Z ''
printf '\n[notes](notes_(v2).md)\n' >> docs/good.md
"$script" --base "$base" --strict >out 2>&1 || { cat out; echo 'FAIL parenthesized destination'; exit 1; }
echo 'ok   parenthesized destination'
git checkout -- docs/good.md
printf '# No frontmatter\n[bad](missing.md)\n' > docs/plain.md
if "$script" --base "$base" --strict >out 2>&1; then echo 'FAIL invalid doc accepted'; exit 1; fi
grep -q 'missing frontmatter' out && grep -q 'broken link missing.md' out || { cat out; echo 'FAIL missing-frontmatter link not checked'; exit 1; }
echo 'ok   links checked without frontmatter'
rm docs/plain.md
cat > docs/block.md <<'DOC'
---
type: guide
owner:
  - "@x"
status: active
stale_after: 2999-01-01T00:00:00Z
---
# Test
DOC
"$script" --base "$base" --strict >out 2>&1 || { cat out; echo 'FAIL block owner'; exit 1; }
echo 'ok   block owner accepted'
rm docs/block.md
for field in owner status; do
  make_doc comments 2999-01-01T00:00:00Z ''
  if [ "$field" = owner ]; then sed -i.bak 's/^owner: team$/owner: "" # TODO/' docs/comments.md; else sed -i.bak 's/^status: active$/status: # pending/' docs/comments.md; fi
  rm docs/comments.md.bak
  if "$script" --base "$base" --strict >out 2>&1; then echo "FAIL commented empty $field accepted"; exit 1; fi
  grep -q "missing $field" out || { cat out; exit 1; }
done
make_doc comments 2999-01-01T00:00:00Z ''
sed -i.bak 's/^type: guide$/type: reference # note/; s/^owner: team$/owner: "team # name" # note/; s/^stale_after: .*/stale_after: 2999-01-01T00:00:00Z # note/' docs/comments.md
rm docs/comments.md.bak
"$script" --base "$base" --strict >out 2>&1 || { cat out; echo 'FAIL valid trailing comment'; exit 1; }
echo 'ok   YAML comments'
rm docs/comments.md
make_doc refs 2999-01-01T00:00:00Z '[guide][missing] and [absent][]'
if "$script" --base "$base" --strict >out 2>&1; then echo 'FAIL undefined references accepted'; exit 1; fi
grep -qi 'missing' out && grep -qi 'absent' out || { cat out; echo 'FAIL missing labels'; exit 1; }
make_doc refs 2999-01-01T00:00:00Z '[guide][  MIXED   Case ] and [mixed case][] and [ordinary] and [ ]'
printf '\n[mixed  CASE]: ../baseline\n' >> docs/refs.md
"$script" --base "$base" --strict >out 2>&1 || { cat out; echo 'FAIL defined reference'; exit 1; }
make_doc refs 2999-01-01T00:00:00Z '[guide][defined]'
printf '\n[defined]: nonexistent.md\n' >> docs/refs.md
if "$script" --base "$base" --strict >out 2>&1; then echo 'FAIL broken reference target accepted'; exit 1; fi
grep -q nonexistent.md out || { cat out; exit 1; }
echo 'ok   reference labels and targets'
rm docs/refs.md
make_doc indented 2999-01-01T00:00:00Z ''
printf '    ```\n[bad](missing.md)\n' >> docs/indented.md
if "$script" --base "$base" --strict >out 2>&1; then echo 'FAIL indented marker hid link'; exit 1; fi
grep -q missing.md out || { cat out; exit 1; }
make_doc indented 2999-01-01T00:00:00Z ''
printf '```\n    ```\n[ignored](missing.md)\n```\n[good](../baseline)\n' >> docs/indented.md
"$script" --base "$base" --strict >out 2>&1 || { cat out; echo 'FAIL indented marker closed fence'; exit 1; }
echo 'ok   indented fence markers'
make_doc indented 2999-01-01T00:00:00Z ''
printf '    ~~~\n[bad](missing.md)\n' >> docs/indented.md
if "$script" --base "$base" --strict >out 2>&1; then echo 'FAIL indented tilde marker hid link'; exit 1; fi
grep -q missing.md out || { cat out; exit 1; }
make_doc indented 2999-01-01T00:00:00Z ''
printf '~~~\n    ~~~\n[ignored](missing.md)\n~~~\n[good](../baseline)\n' >> docs/indented.md
"$script" --base "$base" --strict >out 2>&1 || { cat out; echo 'FAIL indented tilde marker closed fence'; exit 1; }
echo 'ok   indented tilde markers'
rm docs/indented.md
