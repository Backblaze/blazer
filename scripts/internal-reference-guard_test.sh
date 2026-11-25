#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
guard="$root/scripts/internal-reference-guard.sh"
printf '%s\n' 'sample DEV-12' > "$tmp/bad"
printf '%s\n' 'DEVICE-12 NODEV-12 public example' > "$tmp/clean"
check() {
  local expected=$1; shift
  local status=0
  "$guard" "$@" > "$tmp/out" 2>&1 || status=$?
  if [[ $expected == fail && $status == 0 ]] || [[ $expected == pass && $status != 0 ]]; then
    echo "FAIL: expected $expected for $* (exit $status)"; cat "$tmp/out"; exit 1
  fi
}
check fail file "$tmp/bad"
check pass file "$tmp/clean"
check fail title 'DEV-12'
check pass title 'DEVICE-12'
check fail body 'DEV-12'
check pass body 'clean body'
check fail commit abc 'DEV-12'
check pass commit abc 'clean commit'
check fail body 'see https://backblaze.atlassian.net/wiki'
check fail title 'Backblaze.Atlassian.Net'
check fail commit abc 'see backblaze.atlassian.net'
check pass title 'dev-12'
check pass body 'notatlassian.network'
printf '%s\n' 'Backblaze.Atlassian.Net' > "$tmp/host"
check fail file "$tmp/host"
echo 'internal-reference-guard tests PASS'
