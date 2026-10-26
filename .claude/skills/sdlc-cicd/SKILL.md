---
name: sdlc-cicd
description: Run Claude Code non-interactively in CI/CD (claude -p, claude-code-action), sandbox agent jobs, expose deploy/status/rollback as per-environment MCP tools with allowlists, and tier autonomy per environment. Use when asked to "add Claude to our pipeline", "triage failed builds with Claude", "let Claude deploy to staging", "set up rollback", or when wiring any other play into CI.
---

# CI/CD integration and deployment

Claude runs headless in the pipeline for the judgment steps (triage a failing build, summarize a flaky test, draft the changelog) in a sandbox with scoped credentials. Deployment tooling is exposed through MCP so the workflow that wrote and tested a change can also ship and roll it back, inside gates the organization defines per environment. The agent may act up to the production gate and cannot pass it.

## Prerequisites

PR review (`sdlc-pr-review`) and approval hooks (`sdlc-hook-gate`) exist first: the gates must be in place before automation accelerates anything through them.

## Rollout order

1. **Read-only judgment steps.** `claude -p` in a pipeline job, e.g. on failure:
   ```yaml
   - name: Triage failed build
     if: failure()
     run: >
       claude -p "Read the build log at out/build.log. Identify the most
       likely cause, say whether the failure looks flaky or real, and write a
       three-line summary for the PR thread." >> triage.md
   ```
2. **Write steps behind existing gates**: fix lint, update generated docs, address review comments via `@claude` mentions. Everything arrives as a PR through branch protection; the agent has no route to push to main.
3. **Sandbox execution.** Agent jobs run in containers under a network policy with short-lived scoped tokens and no standing production credentials. Model access via the API, or Amazon Bedrock, Microsoft Foundry or Vertex AI where traffic must stay on the organization's cloud agreement.
4. **Deployment through MCP.** One MCP server per environment (`deploy-dev`, `deploy-staging`, `deploy-prod`), each exposing `release(version[, ticket])`, `status`, `rollback`. A permission rule names the server and the tool, not its arguments, so separate servers are what let an allowlist distinguish staging from production. Give each job only its environment's token. In `.mcp.json`:
   ```json
   { "mcpServers": {
       "deploy-staging": {
         "type": "http",
         "url": "https://deploy.example.com/mcp/staging",
         "headers": { "Authorization": "Bearer ${DEPLOY_STAGING_TOKEN}" } } } }
   ```
5. **Pre-approve only what the job needs**, and deny everything else without prompting:
   ```bash
   claude -p "Release $SHA to staging, then call status. If any endpoint's 5xx rate is over 1%, roll back. Finish with three lines for the release notes." \
     --allowedTools "mcp__deploy-staging__release,mcp__deploy-staging__status,mcp__deploy-staging__rollback" \
     --permission-mode dontAsk >> release-notes.md
   ```
   `dontAsk` denies any call that would otherwise prompt, so a tool off the list is refused and the job never hangs.
6. **Tier autonomy by environment.**

   | Environment | Tools allowed | `release` | Approver |
   |---|---|---|---|
   | Dev | release, status, rollback | hook allows | nobody |
   | Staging | release, status, rollback | hook asks, or allows from the pipeline on `main` | engineer or approved merge |
   | Production | status, rollback | denied unless the gate hook allows (approved change ticket) | release manager |

   Keep `release` off the production allowlist so the hook's `allow` is the only way through; then production fails closed if the hook fails to run. `rollback` stays allowed because it is a runbook approved in advance.
7. **Rehearse rollback**: a single command the agent can run, exercised regularly in staging. `sdlc-close-the-loop` calls it automatically when a band breaches, so it must be proven first.

## Managed settings interplay

With `allowManagedHooksOnly`, the gate hook must live in managed settings; with `allowManagedMcpServersOnly`, the deployment servers must be on the managed allowlist.

## Workflow hygiene

- Any workflow with a `pull_request:` trigger guards every job against Dependabot: `if: ${{ github.actor != 'dependabot[bot]' && github.event.pull_request.user.login != 'dependabot[bot]' }}`.
- Each non-interactive run acts under the agent's own identity, so logs separate what the agent did from what the triggering engineer did.
- Secrets come from the CI secret store, never the repo; never echo tokens.

## Measure

- Leading: share of pipeline failures triaged without paging a human (pipeline logs).
- Lagging: DORA measures the CI system and deployment tooling already emit.
