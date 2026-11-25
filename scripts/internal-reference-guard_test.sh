#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
guard="$root/scripts/internal-reference-guard.sh"
export TRACKER_KEYS='B2|DEV|DEVDOCS|DOCS|HD'
export INTERNAL_HOSTS='backblaze\.atlassian\.net|atlassian\.net'
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
echo 'internal-reference-guard tests PASS'
