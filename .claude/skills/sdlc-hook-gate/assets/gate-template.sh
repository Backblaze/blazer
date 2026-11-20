#!/bin/bash
# Pre-tool approval gate template. Reads the tool call as JSON on stdin (tool_name, tool_input: Claude Code
# and Codex use these names; check your assistant's hook input) and prints a decision.
# Usage: release-gate.sh [--format claude|codex|copilot|gemini]   (default claude)
# Codex and Gemini hooks cannot ask a person, and Codex treats an unsupported "ask" as a failed hook and
# runs the tool, so with those formats every "ask" below becomes a deny. Give them an approval route that does
# not depend on the prompt (the pipeline signal, or a recorded approval as for production).
# Per environment: dev allow, staging ask, production allow only with an approved change ticket.
# Edit the case arms. The gate must read approvals the agent has no way to write to.
# Never resolve tools through the caller's PATH, which the agent can influence. Adjust if jq lives elsewhere.
PATH=/usr/bin:/bin; export PATH
# Fail closed: exit 2 blocks the call even if jq is missing and no decision can be printed.
command -v jq >/dev/null 2>&1 || { echo "release gate: jq is required; blocking" >&2; exit 2; }
format=claude
[ "${1:-}" = --format ] && format="${2:-}"
case "$format" in claude|codex|copilot|gemini) ;; *) echo "release gate: unknown --format '$format'; blocking" >&2; exit 2 ;; esac
input=$(cat)
tool=$(jq -r '.tool_name // empty' <<<"$input")
version=$(jq -r '.tool_input.version // empty' <<<"$input")
ticket=$(jq -r '.tool_input.ticket // empty' <<<"$input")
inputs=$(jq -r '(.tool_input // {}) | keys | join(", ")' <<<"$input")
# The approval lookup must not resolve through PATH, which the agent can influence. Use an absolute path.
ticket_cli="/usr/local/bin/change-ticket"
# Who may approve a production release: named people, not a pattern. Bots and service accounts are never listed.
release_managers='["release-manager-login"]'

decide() {   # decide <allow|ask|deny> <reason>, printed in the assistant's format
  local decision=$1 reason=$2
  if [ "$decision" = ask ] && { [ "$format" = codex ] || [ "$format" = gemini ]; }; then
    decision=deny
    reason="Needs a person's approval, and this assistant's hooks cannot ask for it. $reason"
  fi
  case "$format" in
    claude) jq -n --arg d "$decision" --arg r "$reason" \
              '{hookSpecificOutput: {hookEventName: "PreToolUse", permissionDecision: $d, permissionDecisionReason: $r}}' ;;
    codex) [ "$decision" = allow ] || jq -n --arg r "$reason" \
              '{hookSpecificOutput: {hookEventName: "PreToolUse", permissionDecision: "deny", permissionDecisionReason: $r}}' ;;
    copilot) jq -n --arg d "$decision" --arg r "$reason" '{permissionDecision: $d, permissionDecisionReason: $r}' ;;
    gemini) [ "$decision" = allow ] || jq -n --arg r "$reason" '{decision: "deny", reason: $r}' ;;
  esac
  exit 0
}

# Register with the matcher "mcp__deploy-.*" so every tool on every deploy server comes here, not only
# release; anything without an arm below is denied.
case "$tool" in
  mcp__deploy-dev__status|mcp__deploy-staging__status|mcp__deploy-prod__status)
    decide allow "read-only status" ;;
  mcp__deploy-dev__release|mcp__deploy-dev__rollback)
    decide allow "dev needs no approval" ;;
  mcp__deploy-staging__rollback|mcp__deploy-prod__rollback)
    # Only "back to the previous release", with no inputs at all. Any input (version, release id, steps)
    # picks what ships, which makes it a release that needs its gate.
    [ -z "$inputs" ] && decide allow "rollback to the previous release"
    decide deny "A rollback with inputs ($inputs) chooses what ships. Use release, with its approval." ;;
  mcp__deploy-staging__release)
    extra=$(jq -r '(.tool_input // {}) | keys - ["version"] | join(", ")' <<<"$input")
    [ -z "$extra" ] || decide deny "Staging release takes only version; got: $extra."
    [ "${PIPELINE_RELEASE_FROM_MAIN:-}" = "1" ] && decide allow "pipeline release from main"
    decide ask "This ships $version to staging. Approve only if you own this release." ;;
  mcp__deploy-prod__release)
    # Only the fields the approval covers. Any other input (image, ref, flags) could ship something unapproved.
    extra=$(jq -r '(.tool_input // {}) | keys - ["version", "ticket"] | join(", ")' <<<"$input")
    [ -z "$extra" ] || decide deny "Production release takes only version and ticket; got: $extra."
    route="Open a change ticket at https://change.example.com/new for version $version, wait for the release manager to approve it, then call release again with that ticket number."
    [[ "$ticket" =~ ^CHG-[0-9]+$ && "$version" =~ ^[0-9a-f]{7,40}$ ]] ||
      decide deny "Production needs an approved change ticket. $route"
    # Replace with your change system's CLI; it must emit JSON with .state, .version, .environment, .requester,
    # .approver and optionally .expires_at (ISO 8601, UTC). A ticket for another environment or past expiry denies.
    # The approver must be in release_managers and must not be the requester.
    # pipefail: a failed lookup denies even if it printed an approved record first
    if approver=$(set -o pipefail; "$ticket_cli" show --ticket "$ticket" --format json 2>/dev/null |
        jq -er --arg v "$version" --argjson managers "$release_managers" 'select(.state == "approved" and .version == $v
          and ((.environment // "") | ascii_downcase | . == "production" or . == "prod")
          and (.expires_at == null or ((.expires_at | fromdateiso8601) > now))
          and (.approver as $a | $managers | index($a) != null)
          and (.requester | type == "string" and length > 0)
          and .approver != .requester) | .approver'); then
      decide allow "$ticket approved by $approver"
    fi
    decide deny "$ticket is not approved for version $version. $route" ;;
  *)
    decide deny "No gate rule for '$tool'. Add one to this script first." ;;
esac
