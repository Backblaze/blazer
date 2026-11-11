#!/bin/bash
# Grades one eval run. Usage: EVAL_BASE=<sha> check.sh <eval.json> <result.json>
# Run from the root of the checkout the agent worked in. EVAL_BASE is the commit recorded before the
# agent ran, so commits the agent makes still count as changes. Needs jq and git.
# result.json is normalize_result.py's output ({"ok", "final", "why"}), whichever assistant ran, so the
# run's success comes from the runtime's signals and never from text the model wrote.
set -u
eval_file=$1
result_file=$2
base=${EVAL_BASE:?set EVAL_BASE to the commit the run started from}
git() { command git -c core.fsmonitor=false -c core.hooksPath=/dev/null "$@"; }   # ignore hooks the run may have set
git cat-file -e "$base^{commit}" 2>/dev/null || { echo "FAIL: EVAL_BASE $base is not a commit here, so changes cannot be checked"; exit 1; }

name=$(jq -r '.name' "$eval_file")
export EVALS=$(cd "$(dirname "$eval_file")" && pwd)   # checks may call helpers kept beside the evals

# Files changed since the base commit, committed or not, plus new untracked files, that the eval does
# not allow (Git-ignored files don't show up). If Git cannot list them, that is a stray too.
strays() {
  local list
  list=$(mktemp)
  if ! { git diff -z --no-renames --no-ext-diff --name-only "$base" -- && git ls-files -z --others --exclude-standard; } >"$list" 2>/dev/null; then
    echo "       (git could not list the changed files)"
  fi
  sort -zu "$list" | while IFS= read -r -d '' path; do
    jq -e --arg p "$path" \
      'any(.may_change[]; . as $ok | $p == $ok or ($p | startswith($ok + "/")))' \
      "$eval_file" >/dev/null || echo "       $path"
  done
  rm -f "$list"
}

total=$(jq '(.checks // []) | length' "$eval_file" 2>/dev/null || echo 0)
passed=0
report=""

if [ "${total:-0}" -eq 0 ]; then
  report="     the eval defines no checks, so it proves nothing"$'\n'
elif ! jq -e '.ok == true' "$result_file" >/dev/null 2>&1; then
  report="     the run did not finish cleanly: $(jq -r '.why // "result is not normalize_result.py output"' "$result_file" 2>/dev/null)"$'\n'
elif [ -n "$(strays)" ]; then
  report="     the run changed files this eval does not allow:"$'\n'"$(strays)"$'\n'
else
  for i in $(seq 0 $((total - 1))); do
    label=$(jq -r ".checks[$i].name" "$eval_file")
    cmd=$(jq -r ".checks[$i].run" "$eval_file")
    if out=$(bash -c "$cmd" 2>&1 </dev/null); then
      passed=$((passed + 1))
    else
      report+="     failed: $label"$'\n'
      [ -n "$out" ] && report+="$(head -3 <<<"$out" | sed 's/^/       /')"$'\n'
    fi
  done
  # a check must not have modified the tree either (Makefile, scripts)
  if [ -n "$(strays)" ]; then
    passed=0
    report+="     a check changed files this eval does not allow:"$'\n'"$(strays)"$'\n'
  fi
fi

if [ "${total:-0}" -gt 0 ] && [ "$passed" -eq "$total" ]; then
  echo "PASS $name ($passed/$total checks)"
else
  echo "FAIL $name ($passed/$total checks)"
  printf '%s' "$report"
  exit 1
fi
