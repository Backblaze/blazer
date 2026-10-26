#!/bin/bash
# Grades one eval run. Usage: check.sh <eval.json> <result.json>
# Run from the root of the checkout the agent worked in. Needs jq and git.
# RUN_AS (e.g. "sudo -u agent -H") runs each check as the agent's user, so code under test cannot
# write to this script or the eval definitions.
# GRADE_DIR names a checkout of the same commit that the grader owns. When set, the run's files
# (without its agent-writable .git) are copied there and inspected, so Git never reads metadata
# the agent could have planted.
set -u
eval_file=$1
result_file=$2

name=$(jq -r '.name' "$eval_file")
export EVALS=$(cd "$(dirname "$eval_file")" && pwd)   # checks may call helpers kept beside the evals

# Files changed outside the folders/files the eval allows (Git-ignored files don't show up).
# A tree that cannot be copied or inspected counts as a stray, never as clean.
strays() {
  local dir=$PWD status
  if [ -n "${GRADE_DIR:-}" ]; then
    rsync -a --delete --exclude=/.git "$PWD"/ "$GRADE_DIR"/ 2>/dev/null \
      || { echo "       (could not copy the run's files for inspection)"; return; }
    dir=$GRADE_DIR
  fi
  status=$(mktemp)
  if ! git -C "$dir" status --porcelain=v1 -z --untracked-files=all --no-renames > "$status" 2>/dev/null; then
    echo "       (git status failed; the working tree could not be inspected)"
    rm -f "$status"
    return
  fi
  while IFS= read -r -d '' entry; do
    path=${entry:3}
    jq -e --arg p "$path" \
      'any(.may_change[]; . as $ok | $p == $ok or ($p | startswith($ok + "/")))' \
      "$eval_file" >/dev/null || echo "       $path"
  done < "$status"
  rm -f "$status"
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
    if out=$(${RUN_AS:-} env EVALS="$EVALS" bash -c "$cmd" 2>&1 </dev/null); then  # sudo resets the env
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
