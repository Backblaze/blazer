#!/bin/bash
# Grades one eval run. Usage: check.sh <eval.json> <result.json>
# Run from the root of the checkout the agent worked in. Needs jq and git.
set -u
eval_file=$1
result_file=$2

name=$(jq -r '.name' "$eval_file")
export EVALS=$(cd "$(dirname "$eval_file")" && pwd)   # checks may call helpers kept beside the evals

# Files changed outside the folders/files the eval allows (Git-ignored files don't show up).
strays() {
  git status --porcelain | cut -c4- | while read -r path; do
    jq -e --arg p "$path" \
      'any(.may_change[]; . as $ok | $p == $ok or ($p | startswith($ok + "/")))' \
      "$eval_file" >/dev/null || echo "       $path"
  done
}

total=$(jq '.checks | length' "$eval_file")
passed=0
report=""

if [ "$(jq -r '.is_error' "$result_file" 2>/dev/null)" != "false" ]; then
  report="     the run ended in an error"$'\n'
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

if [ "$passed" -eq "$total" ]; then
  echo "PASS $name ($passed/$total checks)"
else
  echo "FAIL $name ($passed/$total checks)"
  printf '%s' "$report"
  exit 1
fi
