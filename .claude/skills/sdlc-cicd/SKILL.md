---
name: sdlc-cicd
description: Run the AI assistant non-interactively in CI/CD (claude -p, codex exec, copilot -p, gemini -p, or their GitHub Actions), sandbox agent jobs, expose deploy/status/rollback as per-environment MCP tools with allowlists, and tier autonomy per environment. Use when asked to "add an AI agent to our pipeline", "triage failed builds with the agent", "let the agent deploy to staging", "set up rollback", or when wiring any other play into CI.
---

# CI/CD integration and deployment

The AI assistant (Claude, Codex, Copilot, Gemini) runs headless in the pipeline for the judgment steps (triage a failing build, summarize a flaky test, draft the changelog) in a sandbox with scoped credentials. Deployment tooling is exposed through MCP so the workflow that wrote and tested a change can also ship and roll it back, inside gates the organization defines per environment. The agent may act up to the production gate and cannot pass it.

## Prerequisites

PR review (`sdlc-pr-review`) and approval hooks (`sdlc-hook-gate`) exist first: the gates must be in place before automation accelerates anything through them.

## Rollout order

1. **Read-only judgment steps.** A headless run in a pipeline job, e.g. on failure. Claude Code is shown; `codex exec`, `copilot -p` and `gemini -p` are the equivalents (`references/ai-assistants.md`):
   ```yaml
   - name: Triage failed build
     if: failure()
     run: >
       claude -p "Read the build log at out/build.log. Identify the most
       likely cause, say whether the failure looks flaky or real, and write a
       three-line summary for the PR thread."
       --tools "Read,Grep" --allowedTools "Read,Grep" --strict-mcp-config --permission-mode dontAsk >> triage.md
   ```
2. **Write steps behind existing gates**: fix lint, update generated docs, address review comments through the assistant's GitHub integration (`@claude` mentions with `anthropics/claude-code-action`, or `openai/codex-action`, the Copilot coding agent, `google-github-actions/run-gemini-cli`). Everything arrives as a PR through branch protection; the agent has no route to push to main.
3. **Sandbox execution.** Agent jobs run in containers under a network policy with short-lived scoped tokens and no standing production credentials. Model access via the vendor's API, or through a cloud provider where traffic must stay on the organization's cloud agreement (for Claude: Amazon Bedrock, Microsoft Foundry or Vertex AI).
4. **Deployment through MCP.** One MCP server per environment (`deploy-dev`, `deploy-staging`, `deploy-prod`), each exposing `release(version[, ticket])`, `status`, `rollback`. A permission rule names the server and the tool, not its arguments, so separate servers are what let an allowlist distinguish staging from production. Give each job only its environment's token. In `.mcp.json` (Claude Code; the other assistants' MCP config is in `references/ai-assistants.md`):
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
     --tools "" \
     --mcp-config deploy-staging.mcp.json --strict-mcp-config \
     --allowedTools "mcp__deploy-staging__release,mcp__deploy-staging__status,mcp__deploy-staging__rollback" \
     --permission-mode dontAsk >> release-notes.md
   ```
   Two separate limits. `--tools ""` removes the built-in tools (no shell, no file edits); it does not touch MCP tools. `--mcp-config` with `--strict-mcp-config` loads only the staging server, so no other server from repo or user settings is callable. `--allowedTools` pre-approves the three tools, and `dontAsk` refuses anything else instead of hanging the job. Read-only jobs pass `--strict-mcp-config` with no `--mcp-config`, so they load no MCP server at all. For production, keep the release tool available but not pre-approved, so the gate hook decides.

   Codex keeps the same two limits differently. `--sandbox read-only` restricts only the shell and file edits, not MCP tools. Codex also merges configuration layers: the home (`CODEX_HOME`), each trusted project's `.codex/config.toml` from the repository root down to the working directory, and admin settings (`requirements.toml`, system settings). A job-only `CODEX_HOME` replaces only the first, so a trusted checkout can still add a server. Run the release job outside any repository, from an empty directory, with its own `CODEX_HOME` that holds only the deploy server and trusts no project, and list the tools it may call:
   ```toml
   # $CODEX_HOME/config.toml for the staging release job
   [mcp_servers.deploy-staging]
   url = "https://deploy.example.com/mcp/staging"
   bearer_token_env_var = "DEPLOY_STAGING_TOKEN"
   enabled_tools = ["release", "status", "rollback"]
   ```
   Then run `cd "$(mktemp -d)" && CODEX_HOME=$RUNNER_TEMP/codex-release codex exec --skip-git-repo-check --sandbox read-only "..."`, set tool approval with `default_tools_approval_mode` or `tools.<tool>.approval_mode`, and keep the gate hook from `sdlc-hook-gate` (`--format codex`) for production. Admin settings can still add servers, so before relying on the job, read the effective configuration it runs with and fail if any MCP server other than `deploy-staging` is there; then, with that exact command, check that a forbidden mock call fails and a status call succeeds. Copilot and Gemini have their own MCP and tool settings (`references/ai-assistants.md`); keep the same two limits with theirs.
6. **Tier autonomy by environment.**

   | Environment | Tools allowed | `release` | Approver |
   |---|---|---|---|
   | Dev | release, status, rollback | hook allows | nobody |
   | Staging | release, status, rollback | hook asks, or allows from the pipeline on `main` | engineer or approved merge |
   | Production | status, rollback | denied unless the gate hook allows (approved change ticket) | release manager |

   Keep `release` off the production allowlist so the hook's `allow` is the only way through; then production fails closed if the hook fails to run. `rollback` stays allowed because it is a runbook approved in advance.
7. **Rehearse rollback**: a single command the agent can run, exercised regularly in staging. `sdlc-close-the-loop` calls it automatically when a band breaches, so it must be proven first.

## Merge, release and rollback layers

Reversibility is what makes lighter human review acceptable. Put these around the agent's PRs:

- **Merge queue.** Re-run every check against current main with the change applied, so PRs that pass alone cannot combine into a broken main. Every workflow whose check is required must also trigger on `merge_group`, or the queue waits for a check that never reports.
- **Feature flags.** New behavior ships behind a flag, turned on for employees first. Heavy internal use often surfaces regressions before customers see them. Flags can be turned off without a deploy.
- **Staged release.** Publish to a `latest` channel first and let `stable` trail it.
- **One-step rollback**, rehearsed (step 7 above).
- **On-call and incident review.** A rotation with a standing question after every incident: which layer should have caught this? Fix that layer, in a rule, a check, a tier or an eval.
- **PR template** with Testing, Risk and How to revert, and required checks on every platform you ship.
- Detection still leans on the author, on-call and revert and incident dashboards. Put a control band on revert and incident rates and use a breach to tighten approval tiers (`sdlc-close-the-loop`, `sdlc-risk-tiers`).

- **Nightly review of existing code**: `sdlc-review-tuning/assets/nightly-review.yml` reviews one area a night with a read-only agent and files one issue, to catch old bugs early.
- **Check the setup**: run `sdlc-review-audit` to see which of these layers a repo is missing.

Source: this skill's `references/code-review-at-anthropic.md`.

## Managed settings interplay

In Claude Code, with `allowManagedHooksOnly`, the gate hook must live in managed settings; with `allowManagedMcpServersOnly`, the deployment servers must be on the managed allowlist. Other assistants' admin settings are listed in `references/ai-assistants.md`.

## Workflow hygiene

- Any workflow with a `pull_request:` trigger guards every job against Dependabot: `if: ${{ github.actor != 'dependabot[bot]' && github.event.pull_request.user.login != 'dependabot[bot]' }}`.
- Each non-interactive run acts under the agent's own identity, so logs separate what the agent did from what the triggering engineer did.
- Secrets come from the CI secret store, never the repo; never echo tokens.

## Measure

- Leading: share of pipeline failures triaged without paging a human (pipeline logs).
- Lagging: DORA measures the CI system and deployment tooling already emit.
