#!/bin/bash
# PreToolUse approval gate template. Reads the tool call as JSON on stdin and prints a decision.
# Per environment: dev allow, staging ask, production allow only with an approved change ticket.
# Edit the case arms. The gate must read approvals the agent has no way to write to.
input=$(cat)
tool=$(jq -r '.tool_name // empty' <<<"$input")
version=$(jq -r '.tool_input.version // empty' <<<"$input")
ticket=$(jq -r '.tool_input.ticket // empty' <<<"$input")

decide() {   # decide <allow|ask|deny> <reason>
  jq -n --arg decision "$1" --arg reason "$2" '{hookSpecificOutput: {
    hookEventName: "PreToolUse",
    permissionDecision: $decision,
    permissionDecisionReason: $reason}}'
  exit 0
}

case "$tool" in
  mcp__deploy-dev__release)
    decide allow "dev needs no approval" ;;
  mcp__deploy-staging__release)
    [ "${PIPELINE_RELEASE_FROM_MAIN:-}" = "1" ] && decide allow "pipeline release from main"
    decide ask "This ships $version to staging. Approve only if you own this release." ;;
  mcp__deploy-prod__release)
    route="Open a change ticket at https://change.example.com/new for version $version, wait for the release manager to approve it, then call release again with that ticket number."
    [[ "$ticket" =~ ^CHG-[0-9]+$ && "$version" =~ ^[0-9a-f]{7,40}$ ]] ||
      decide deny "Production needs an approved change ticket. $route"
    # Replace with your change system's CLI; it must emit JSON with .state, .version and .approver.
    if approver=$(change-ticket show --ticket "$ticket" --format json 2>/dev/null |
        jq -er --arg v "$version" 'select(.state == "approved" and .version == $v) | .approver'); then
      decide allow "$ticket approved by $approver"
    fi
    decide deny "$ticket is not approved for version $version. $route" ;;
  *)
    decide deny "No gate rule for '$tool'. Add one to this script first." ;;
esac
