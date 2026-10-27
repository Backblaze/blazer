#!/usr/bin/env bash
# Release gate: holds a release while solvable security or quality findings are open.
# Usage: scripts/release-check.sh [owner/repo]   (needs gh, jq, go; run from the repo root)
# RELEASE_FIXES_ADVISORIES: comma-separated GHSA ids of draft advisories this release fixes; they are
# published with the release instead of holding it.
# Exit 0 = clear to release, 1 = hold. Findings that cannot be read are reported as UNVERIFIED and
# must be checked by hand on the Security tab; they do not pass silently.
set -euo pipefail

for tool in gh jq go; do
  command -v "$tool" >/dev/null || { echo "HOLD: $tool is not installed; the gate cannot run." >&2; exit 1; }
done

repo=${1:-Backblaze/blazer}
govulncheck="golang.org/x/vuln/cmd/govulncheck@v1.1.4"
hold=0
unverified=0

section() { printf '\n== %s\n' "$1"; }
holdf() { echo "HOLD: $*"; hold=1; }

# Runs a paginated gh api list; prints JSON lines, or returns 2 when the endpoint cannot be read.
list() {
  local out
  if ! out=$(gh api --paginate "$1" --jq '.[] | @json' 2>&1); then
    echo "UNVERIFIED: cannot read $1 ($(tail -1 <<<"$out"))" >&2
    return 2
  fi
  printf '%s' "$out"
}

section "Code scanning (CodeQL security and quality) on the default branch"
if alerts=$(list "repos/$repo/code-scanning/alerts?state=open&per_page=100"); then
  n=$(grep -c . <<<"$alerts" || true)
  if [ "$n" -gt 0 ]; then
    jq -r '"  #\(.number) \(.rule.security_severity_level // .rule.severity) \(.rule.id) \(.most_recent_instance.location.path):\(.most_recent_instance.location.start_line) \(.html_url)"' <<<"$alerts"
    holdf "$n open code scanning alert(s). Fix each, or have a code owner dismiss it with a reason."
  else
    echo "  none open"
  fi
else
  unverified=1
fi

section "Dependabot alerts"
if alerts=$(list "repos/$repo/dependabot/alerts?state=open&per_page=100"); then
  solvable=$(jq -c 'select(.security_vulnerability.first_patched_version != null)' <<<"$alerts")
  unpatched=$(jq -c 'select(.security_vulnerability.first_patched_version == null)' <<<"$alerts")
  if [ -n "$solvable" ]; then
    jq -r '"  #\(.number) \(.security_advisory.severity) \(.dependency.package.name) in \(.dependency.manifest_path): upgrade to \(.security_vulnerability.first_patched_version.identifier) \(.html_url)"' <<<"$solvable"
    holdf "$(grep -c . <<<"$solvable") Dependabot alert(s) have a patched version."
  else
    echo "  none with a patched version"
  fi
  [ -n "$unpatched" ] && jq -r '"  note: #\(.number) \(.dependency.package.name) has no patched version yet (not blocking) \(.html_url)"' <<<"$unpatched"
else
  unverified=1
fi

section "Secret scanning"
if alerts=$(list "repos/$repo/secret-scanning/alerts?state=open&per_page=100"); then
  n=$(grep -c . <<<"$alerts" || true)
  if [ "$n" -gt 0 ]; then
    jq -r '"  #\(.number) \(.secret_type) \(.html_url)"' <<<"$alerts"
    holdf "$n open secret scanning alert(s). Revoke the secret, then close the alert."
  else
    echo "  none open"
  fi
else
  unverified=1
fi

section "Repository security advisories in triage or draft"
if advisories=$(list "repos/$repo/security-advisories?per_page=100"); then
  fixes=${RELEASE_FIXES_ADVISORIES:-}
  # Only GHSA ids and states are printed: summaries of unpublished advisories must stay private.
  fixed=$(jq -c --arg f ",$fixes," 'select(.state == "draft" and (.ghsa_id as $id | $f | contains("," + $id + ",")))' <<<"$advisories")
  open=$(jq -c --arg f ",$fixes," 'select((.state == "triage" or .state == "draft") and (.ghsa_id as $id | $f | contains("," + $id + ",") | not))' <<<"$advisories")
  if [ -n "$fixed" ]; then
    jq -r '"  \(.ghsa_id) fixed by this release: publish it right after tagging"' <<<"$fixed"
  fi
  if [ -n "$open" ]; then
    jq -r '"  \(.ghsa_id) \(.state)"' <<<"$open"
    holdf "$(grep -c . <<<"$open") advisory(ies) not yet fixed."
  elif [ -z "$fixed" ]; then
    echo "  none in triage or draft"
  fi
else
  unverified=1
fi

section "govulncheck (reachable known vulnerabilities)"
for dir in . bin/b2keys; do
  if out=$(cd "$dir" && go run "$govulncheck" ./... 2>&1); then
    echo "  $dir: no vulnerabilities found"
  else
    sed 's/^/    /' <<<"$out" | tail -20
    holdf "govulncheck reported findings in $dir (or failed to run)."
  fi
done

echo
if [ "$unverified" -eq 1 ]; then
  echo "UNVERIFIED sections above: check them on https://github.com/$repo/security before releasing."
fi
if [ "$hold" -eq 1 ]; then
  echo "RELEASE ON HOLD. See RELEASE.md, 'Security and quality gate'."
  exit 1
fi
if [ "$unverified" -eq 1 ]; then
  echo "Clear to release once the unverified sections are checked by hand."
else
  echo "Clear to release."
fi
