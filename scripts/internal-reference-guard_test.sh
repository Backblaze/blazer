#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
guard="$root/scripts/internal-reference-guard.sh"
workflow=${GUARD_TEST_WORKFLOW:-$root/.github/workflows/internal-reference-guard.yml}
TRACKER_KEYS=$(sed -n "s/^  TRACKER_KEYS: '\(.*\)'$/\1/p" "$workflow")
INTERNAL_HOSTS=$(sed -n "s/^  INTERNAL_HOSTS: '\(.*\)'$/\1/p" "$workflow")
if [[ -z $TRACKER_KEYS || -z $INTERNAL_HOSTS ]]; then
  echo 'FAIL: workflow TRACKER_KEYS and INTERNAL_HOSTS must both be present' >&2
  exit 1
fi
export TRACKER_KEYS INTERNAL_HOSTS
key='DE''V-12'
host='atlassian''.net'
fullhost='backblaze.''atlassian''.net'
printf '%s\n' "sample $key" > "$tmp/bad"
printf '%s\n' 'DEVICE-12 NODEV-12 public example' > "$tmp/clean"
check() {
  local expected=$1; shift
  local status=0
  "$guard" "$@" > "$tmp/out" 2>&1 || status=$?
  if [[ $expected == fail && $status != 1 ]] || [[ $expected == pass && $status != 0 ]]; then
    echo "FAIL: expected $expected for $* (exit $status)"; cat "$tmp/out"; exit 1
  fi
}
check fail file "$tmp/bad"
check pass file "$tmp/clean"
check fail title "$key"
check pass title 'DEVICE-12'
check fail body "$key"
check pass body 'clean body'
check fail commit abc "$key"
check pass commit abc 'clean commit'
check fail body "see https://$fullhost/wiki"
check fail title 'Backblaze.''Atlassian''.Net'
check fail commit abc "see $fullhost"
check pass title 'dev-12'
check pass body 'notatlassian.network'
check pass title 'atlassianXnet'
check pass title 'backblazeXatlassianXnet'
check fail title "$host"
check fail title "$fullhost"
printf '%s\n' 'Backblaze.''Atlassian''.Net' > "$tmp/host"
check fail file "$tmp/host"
cd "$root"
check pass file scripts/internal-reference-guard.sh
check pass file scripts/internal-reference-guard_test.sh
check pass file .github/workflows/internal-reference-guard.yml
mkdir -p "$tmp/scripts"
printf '%s\n' "$key" > "$tmp/scripts/internal-reference-guard.sh"
(cd "$tmp" && check fail file scripts/internal-reference-guard.sh)
for source in title body commit file; do
  for sample in "${key%12}4521_large-file-retry" "fix_${key%12}4521" "${fullhost}_x"; do
    case "$source" in
      commit) check fail commit abc "$sample" ;;
      file) printf '%s\n' "$sample" > "$tmp/case"; check fail file "$tmp/case" ;;
      *) check fail "$source" "$sample" ;;
    esac
  done
  for sample in "NO${key%12}4521" "${key%12}4521abc" 'DEVICE-12' 'atlassianXnet'; do
    case "$source" in
      commit) check pass commit abc "$sample" ;;
      file) printf '%s\n' "$sample" > "$tmp/case"; check pass file "$tmp/case" ;;
      *) check pass "$source" "$sample" ;;
    esac
  done
done
if [[ ${GUARD_TEST_NO_LINK:-} != 1 ]]; then
  sed 's/|DE[V]|/|/' "$workflow" > "$tmp/workflow.yml"
  status=0
  GUARD_TEST_WORKFLOW="$tmp/workflow.yml" GUARD_TEST_NO_LINK=1 "$0" > "$tmp/link.out" 2>&1 || status=$?
  if [[ $status != 1 ]] || ! grep -q 'FAIL: expected fail for file' "$tmp/link.out"; then
    echo 'FAIL: removing the key from the workflow did not fail the test'; cat "$tmp/link.out"; exit 1
  fi
fi
echo 'internal-reference-guard tests PASS'
