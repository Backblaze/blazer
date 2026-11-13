---
name: sdlc-hook-gate
description: Turn a human approval requirement (release sign-off, change ticket, protected paths, no test edits during fixes) into an assistant hook (Claude Code, Codex, Copilot, Gemini) that allows, asks or blocks, with a self-explaining reason, plus managed-settings guidance. Use when asked to "add an approval gate", "block the agent from editing X", "require a change ticket for production", or "lock down our agent settings".
---

# Hooks as approval gates

Governance is enforced as the AI acts. A hook is a script that runs before (or after) the AI assistant (Claude, Codex, Copilot, Gemini) acts and can **allow**, **ask** (pause until the person running the session confirms) or **block**. When the approver is someone else, such as a release manager, the hook looks for their recorded approval and blocks until it exists. Hooks are not deploy-specific: they apply wherever the assistant acts (protected migrations, infra, test files, secrets). All four assistants have a pre-tool hook that can block; the examples below use Claude Code's format, and `references/ai-assistants.md` gives the equivalents.

## Steps

1. List the human approvals the change process requires and who gives each (change sign-off, release authorization, edits to protected paths). Get this from engineering leadership, change management and compliance, not from guesswork.
2. For each gate, decide per environment or path: allow / ask / deny, and what counts as approval (an approved change ticket tied to this version, a named approver, an approved merge).
3. Write the hook from `assets/gate-template.sh`. It reads the tool call as JSON on stdin and prints the decision in your assistant's format: `--format claude` (default), `codex`, `copilot` or `gemini`. Codex and Gemini hooks cannot ask a person, and Codex treats an `ask` as a failed hook and runs the tool, so with those formats every `ask` becomes a deny; give them an approval route that does not need the prompt (a pipeline signal, or a recorded approval as for production). The template reads `tool_name` and `tool_input`, as Claude Code and Codex send them; check the field names for Copilot and Gemini. Edit the `case` arms. For a gate on production, install it outside the repo where the agent cannot write (for example `/opt/agent-gates/release-gate.sh`, root-owned, mode 0755; see Design rules). Build-time hooks such as formatters can live in the repo (`.claude/hooks/` for Claude Code).
4. In Claude Code, register a production gate in managed settings, with `allowManagedHooksOnly: true` so other settings cannot replace or drop it. Build-time hooks can go in `.claude/settings.json`:
   ```json
   {
     "hooks": {
       "PreToolUse": [
         {
           "matcher": "mcp__deploy-.*",
           "hooks": [
             { "type": "command",
               "command": "/opt/agent-gates/release-gate.sh",
               "args": [],
               "onFailure": "block" }
           ]
         }
       ]
     }
   }
   ```
   The empty `args` list makes Claude Code run the script directly, not through a shell. `"onFailure": "block"` (Claude Code v2.1.295 or later) stops the tool call when the hook cannot start, times out, exits with a code other than 0 or 2, or prints invalid output; without it a broken gate lets the call go ahead. Also keep gated tools out of `permissions.allow`, so a missing hook falls back to a prompt or a denial. Matchers are regexes over the tool name (`Edit|Write`, `mcp__server__tool`). Match every tool on the deploy servers, not only `release`, so a new tool there is denied until it has a rule. The gate only sees MCP calls: deny other routes to production in `permissions.deny` (for example `Bash(kubectl *)`, `Bash(helm *)`, `Bash(terraform apply*)`, your deploy CLI, and `curl` to the deploy API), or the agent can go around it.
   In Codex, the same gate in `.codex/hooks.json` (or as `[[hooks.PreToolUse]]` in `config.toml`):
   ```json
   {
     "hooks": {
       "PreToolUse": [
         { "matcher": "^mcp__deploy-.*",
           "hooks": [ { "type": "command", "command": "/opt/agent-gates/release-gate.sh --format codex", "timeout": 30 } ] }
       ]
     }
   }
   ```
   Codex runs project hooks only after someone trusts them (`/hooks`), and skips a new or changed hook until it is trusted again, so an edited gate stops running without an error. Codex documents only exit 2 and a `deny` as blocking; treat any other failure as letting the call through. For a production gate, deploy it through `requirements.toml` (settings users cannot override) and keep the deploy credentials out of the agent's reach, so a gate that did not run still cannot ship. Copilot: `preToolUse` in `.github/hooks/*.json` (`--format copilot`, which keeps `ask`); Gemini: `BeforeTool` in `.gemini/settings.json` with system settings (`--format gemini`). Shell rules differ too (`references/ai-assistants.md`).
5. Test before trusting it: pipe sample tool calls into the script and read the decision (see below). Test the deny and the fall-through case. That tests the rules, not the enforcement: also run the assistant against a harmless mock deploy server and check that an unapproved release never reaches it, including with the hook missing, changed (untrusted, in Codex) or broken.
6. Commit, and log decisions (Claude Code's OpenTelemetry export records each hook verdict with a timestamp).

## Design rules

- **A block must explain itself.** The reason on a `deny` is shown to the assistant, so name the route to approval (where to open the ticket, who approves) and let the assistant pass it on. The reason on an `ask` is shown to the person.
- **Fail closed.** Unknown tool, missing input, a failed lookup: deny with "no rule for X, add one first".
- **Approvals must be unforgeable by the agent.** The gate reads records (tickets, approvals) the agent has no way to write to, through a CLI called by absolute path (never resolved through `PATH`, which the agent can influence), and neither the hook script nor its registration can be changed by the agent by any route, the shell included. An `Edit|Write` deny alone does not stop a Bash command from rewriting a file. For a gate that matters: register the hook where project and user settings cannot add, replace or drop hooks (in Claude Code, managed settings with `allowManagedHooksOnly: true`); install the script outside the repo in a directory the agent's user cannot write (for example root-owned, mode 0755); and in Claude Code turn on `sandbox.enabled` with `sandbox.filesystem.denyWrite` covering the script and the settings files. Admin controls are not equally strong across assistants (`references/ai-assistants.md`), so also require the check in CI. Say so in the script comments and check it.
- **Non-interactive runs cannot answer an `ask`**, so Claude Code denies it; check how your assistant's headless mode treats one. Where a pipeline legitimately acts (e.g. staging release from `main` after an approved merge), allow it explicitly on a pipeline signal; know that an engineer can fake the signal locally, which only skips a prompt they would have answered themselves.
- **Git as a team choice.** Some teams want the agent read-only in Git: it may inspect and diff, while a person stages, commits and pushes. In Claude Code, put `Bash(git commit *)`, `Bash(git push *)`, `Bash(git rebase *)` and `Bash(git reset *)` in `permissions.deny` (or `ask`); other assistants have their own shell rules (`references/ai-assistants.md`). Test-first still works: the person commits the failing test before the code, which is the evidence of order; the red check in CI shows the tests detect the change (`sdlc-feedback-loop`). Others let the agent commit and push new branches and keep force-push, history rewrites and merges for people. Write down which, in `AGENTS.md`.
- **Build-phase hooks stay fast and file-scoped.** Post-edit hooks (`PostToolUse` in Claude Code) that exit 2 return the message to the assistant to fix the edit; they cannot undo it, so back them with a required CI check on the PR for changes that bypass `Edit|Write` (a shell command rewriting a file).
- **Name the approvers.** The gate accepts a production approval only from a listed release manager (`release_managers` in the template), never from a pattern such as "not a bot": bots, service accounts and agents are never on the list, and the requester cannot approve their own ticket.
- **Agent approvals never stand in for human approvals.** A gate that requires a person reads a human's recorded approval, and nothing an agent can write satisfies it. Approvals attach to a commit: a later push, especially an agent push or a hand-resolved merge, needs re-approval (`sdlc-risk-tiers`).
- **Gates that need a person** (asks, approvals) belong at deploy, not mid-build, where they would put a human on the critical path of every parallel session.

## Testing a hook

```bash
echo '{"tool_name":"mcp__deploy-prod__release","tool_input":{"version":"abc1234"}}' \
  | /opt/agent-gates/release-gate.sh | jq .
```

The input is Claude Code's hook JSON. Expect `permissionDecision` of `deny` with a reason naming the ticket route.

## Managed settings for a regulated enterprise

Settings deployed through MDM or the admin console that engineers cannot override. The keys below are Claude Code's; Codex uses `requirements.toml`, Copilot enterprise policy and `managed-settings.json`, Gemini system settings (`references/ai-assistants.md`). Map each item to its equivalent, and where there is none, enforce it in CI. Treat this as a starting point; every deny rule removes a capability, so tune to the repo's data classification, and check the settings reference for current key names.

- `permissions.deny` for secrets and arbitrary network tools, `permissions.allow` for the safe inner loop (so denies do not cause prompt fatigue), `disableBypassPermissionsMode` + `allowManagedPermissionRulesOnly` so nobody widens the rules.
- `sandbox` with `failIfUnavailable`, `allowUnsandboxedCommands: false`, a network domain allowlist, and denied credential files and env vars. Tool-level denies do not stop a shell command reaching the network; the OS-level sandbox does.
- `allowManagedHooksOnly`: only hooks from managed settings run, so any approval gate that must hold must be defined in the managed file's own `hooks` block, not in project settings.
- `allowManagedMcpServersOnly`, `strictKnownMarketplaces`, `disableSideloadFlags`: the agent's tool surface and plugins come only from what the platform team approved.
- `requiredMinimumVersion`: refuse to start below the assessed version floor.

## Measure

- Leading: time spent waiting on each approval gate (per-gate verdict timestamps in the OTel export).
- Lagging: gate violations reaching production before vs after hooks (incident tracker).
