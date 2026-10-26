---
name: sdlc-hook-gate
description: Turn a human approval requirement (release sign-off, change ticket, protected paths, no test edits during fixes) into a Claude Code hook that allows, asks or blocks, with a self-explaining reason, plus managed-settings guidance. Use when asked to "add an approval gate", "block Claude from editing X", "require a change ticket for production", or "lock down our Claude Code settings".
---

# Hooks as approval gates

Governance is enforced as the AI acts. A hook is a script that runs before (or after) Claude acts and can **allow**, **ask** (pause until the person running the session confirms) or **block**. When the approver is someone else, such as a release manager, the hook looks for their recorded approval and blocks until it exists. Hooks are not deploy-specific: they apply wherever Claude acts (protected migrations, infra, test files, secrets).

## Steps

1. List the human approvals the change process requires and who gives each (change sign-off, release authorization, edits to protected paths). Get this from engineering leadership, change management and compliance, not from guesswork.
2. For each gate, decide per environment or path: allow / ask / deny, and what counts as approval (an approved change ticket tied to this version, a named approver, an approved merge).
3. Write the hook from `assets/gate-template.sh`. It reads the tool call as JSON on stdin and prints a `hookSpecificOutput` decision. Copy it to `.claude/hooks/<name>.sh`, `chmod +x`, edit the `case` arms.
4. Register it in `.claude/settings.json` (team) or managed settings (non-negotiable):
   ```json
   {
     "hooks": {
       "PreToolUse": [
         {
           "matcher": "mcp__deploy-.*__release",
           "hooks": [
             { "type": "command",
               "command": "${CLAUDE_PROJECT_DIR}/.claude/hooks/release-gate.sh",
               "args": [] }
           ]
         }
       ]
     }
   }
   ```
   The empty `args` list makes Claude Code run the script directly, not through a shell. Matchers are regexes over the tool name (`Edit|Write`, `mcp__server__tool`).
5. Test before trusting it: pipe sample tool calls into the script and read the decision (see below). Test the deny and the fall-through case.
6. Commit, and log decisions (OpenTelemetry export records each hook verdict with a timestamp).

## Design rules

- **A block must explain itself.** The reason on a `deny` is shown to Claude, so name the route to approval (where to open the ticket, who approves) and let Claude pass it on. The reason on an `ask` is shown to the person.
- **Fail closed.** Unknown tool, missing input, a failed lookup: deny with "no rule for X, add one first".
- **Approvals must be unforgeable by the agent.** The gate reads records (tickets, approvals) the agent has no way to write to. Say so in the script comments and check it.
- **Non-interactive runs cannot answer an `ask`**, so Claude Code denies it. Where a pipeline legitimately acts (e.g. staging release from `main` after an approved merge), allow it explicitly on a pipeline signal; know that an engineer can fake the signal locally, which only skips a prompt they would have answered themselves.
- **Build-phase hooks stay fast and file-scoped.** `PostToolUse` hooks that exit 2 return the message to Claude to fix the edit; they cannot undo it, so back them with a required CI check on the PR for changes that bypass `Edit|Write` (a shell command rewriting a file).
- **Gates that need a person** (asks, approvals) belong at deploy, not mid-build, where they would put a human on the critical path of every parallel session.

## Testing a hook

```bash
echo '{"tool_name":"mcp__deploy-prod__release","tool_input":{"version":"abc1234"}}' \
  | .claude/hooks/release-gate.sh | jq .
```

Expect `permissionDecision` of `deny` with a reason naming the ticket route.

## Managed settings for a regulated enterprise

Settings deployed through MDM or the admin console that engineers cannot override. Treat this as a starting point; every deny rule removes a capability, so tune to the repo's data classification, and check the settings reference for current key names.

- `permissions.deny` for secrets and arbitrary network tools, `permissions.allow` for the safe inner loop (so denies do not cause prompt fatigue), `disableBypassPermissionsMode` + `allowManagedPermissionRulesOnly` so nobody widens the rules.
- `sandbox` with `failIfUnavailable`, `allowUnsandboxedCommands: false`, a network domain allowlist, and denied credential files and env vars. Tool-level denies do not stop a shell command reaching the network; the OS-level sandbox does.
- `allowManagedHooksOnly`: only hooks from managed settings run, so any approval gate that must hold must be defined in the managed file's own `hooks` block, not in project settings.
- `allowManagedMcpServersOnly`, `strictKnownMarketplaces`, `disableSideloadFlags`: the agent's tool surface and plugins come only from what the platform team approved.
- `requiredMinimumVersion`: refuse to start below the assessed version floor.

## Measure

- Leading: time spent waiting on each approval gate (per-gate verdict timestamps in the OTel export).
- Lagging: gate violations reaching production before vs after hooks (incident tracker).
