# AI assistants: the same play on Claude Code, Codex, Copilot and Gemini

The playbook is written for any AI coding assistant. The skills, hooks and CI jobs name one concrete mechanism as a worked example, usually Claude Code's because the course this playbook follows uses it; this page gives the equivalent for OpenAI Codex, GitHub Copilot and Google Gemini CLI.

Checked against each vendor's documentation on 2026-11-11 (links below). These products change monthly, and parts of this table came from summaries of long reference pages, so check the linked page before you depend on an exact flag or path. "Not documented" means we did not find it, not that it cannot be done.

## Instructions and skills

| Concept | Claude Code | OpenAI Codex | GitHub Copilot | Gemini CLI |
|---|---|---|---|---|
| Repo instruction file | `CLAUDE.md` (imports with `@AGENTS.md`) | `AGENTS.override.md`, else `AGENTS.md`, else `project_doc_fallback_filenames`, per directory; closer files win; 32 KiB default cap | `AGENTS.md` (nearest wins), `.github/copilot-instructions.md`, `.github/instructions/*.instructions.md`; also reads `CLAUDE.md`, `GEMINI.md` | `GEMINI.md`; set `context.fileName` to include `AGENTS.md` |
| Skills (`SKILL.md` folders) | `.claude/skills/` | `.agents/skills/` | `.github/skills/`, `.agents/skills/`, `.claude/skills/` | `.gemini/skills/` or `.agents/skills/` |
| Custom agents and subagents | `.claude/agents/*.md` | `.codex/agents/*.toml` | `.github/agents/*.agent.md` | `.gemini/agents/*.md` |
| Read-only planning | Plan mode (Shift+Tab, `--permission-mode plan`) | `/plan`; or `--sandbox read-only` | CLI plan mode (Shift+Tab) | Plan mode (`--approval-mode plan`, `/plan`) |
| MCP servers | `.mcp.json` | `[mcp_servers]` in `.codex/config.toml`; limit with `enabled_tools`, `disabled_tools` | Repo settings (coding agent); `~/.copilot/mcp-config.json` (CLI) | `mcpServers` in `.gemini/settings.json` |

The playbook uses `AGENTS.md` as the canonical instruction file because three of the four read it as is. For Claude Code, keep a `CLAUDE.md` whose only line is `@AGENTS.md`; for Gemini CLI, add `AGENTS.md` to `context.fileName`. The playbook stores its skills in `.claude/skills/`; `scripts/install-skills.sh --for` copies them to the folder each assistant reads.

## Gates, policy and automation

| Concept | Claude Code | OpenAI Codex | GitHub Copilot | Gemini CLI |
|---|---|---|---|---|
| Hook before a tool runs, able to block | `PreToolUse` in `.claude/settings.json`; exit 2 or `permissionDecision: "deny"` | `PreToolUse` in `.codex/hooks.json` or `[hooks]` in config; exit 2 or `deny` | `preToolUse` in `.github/hooks/*.json`; exit 2 or `permissionDecision: "deny"` | `BeforeTool` in `.gemini/settings.json`; exit 2 or `decision: "deny"` |
| Hook after an edit | `PostToolUse` | `PostToolUse` | `postToolUse` | `AfterTool` |
| Settings users cannot override | Managed settings (`allowManagedHooksOnly`) | `requirements.toml` (`/etc/codex/`) | Enterprise policy, `managed-settings.json`, `/etc/github-copilot/policy.d/` | System settings (`/etc/gemini-cli/settings.json`), admin policies |
| Shell command allow and deny | `permissions.allow` / `deny` (`Bash(git push *)`) | Starlark `prefix_rule` in `.codex/rules/*.rules` | `--allow-tool` / `--deny-tool` (`shell(git:*)`) | Policy TOML rules (`run_shell_command`, `commandPrefix`) |
| Headless run in CI | `claude -p` (`--output-format json`, `--tools` for built-ins, `--strict-mcp-config` for MCP servers, `--allowedTools`) | `codex exec` (`--json`; `--sandbox read-only` limits the shell, not MCP tools; run outside any checkout with a job-only `CODEX_HOME`) | `copilot -p` (`--output-format=json`, `--allow-tool`, `--deny-tool`) | `gemini -p` (`--output-format json`, `--allowed-tools`, `--approval-mode`) |
| GitHub integration | `anthropics/claude-code-action` | `openai/codex-action` | Copilot coding agent (assign an issue), `copilot-setup-steps.yml`, Copilot code review | `google-github-actions/run-gemini-cli` |

## Notes

- Hooks are the deterministic backstop in this playbook, and all four now have a pre-tool hook that can block. Their JSON formats differ: port `sdlc-hook-gate/assets/gate-template.sh` by changing how it reads the tool call and prints the decision, not its rules.
- Copilot code review reads `AGENTS.md`, `.github/copilot-instructions.md` and `REVIEW.md`, so the review instructions in `sdlc-pr-review` apply without a workflow.
- Gemini CLI's documentation says it was replaced by Antigravity CLI for unpaid and Google One users on 2026-06-18; check which product your team uses.
- Admin controls are not equally strong. Gemini's enterprise page says a user with local admin rights can bypass them; for a gate that must hold, also require the check in CI.
- The headless commands differ in how they restrict tools and report results. The CI templates use Claude Code's flags; `sdlc-evals/assets/normalize_result.py` reads both Claude Code's and Codex's output into one result for grading, and `scripts/skill_triggers.py run --assistant codex` measures routing with Codex. Copilot and Gemini CLI need their result parser added before those templates can grade them.
- Restricting tools is two separate limits: built-in tools (shell, file edits) and MCP servers. Claude Code's `--tools` covers only the first, so add `--strict-mcp-config`; Codex's `--sandbox` covers only the shell and edits, and its configuration merges the home, every trusted project's `.codex/config.toml` and admin settings, so run the job outside any checkout with a job-only `CODEX_HOME`, restrict the server with `enabled_tools`, and check the effective server list (`sdlc-cicd`).

## Sources

- Claude Code: [documentation](https://code.claude.com/docs/en/overview).
- OpenAI Codex: [AGENTS.md](https://developers.openai.com/codex/guides/agents-md), [skills](https://developers.openai.com/codex/skills), [hooks](https://learn.chatgpt.com/docs/hooks), [non-interactive mode](https://developers.openai.com/codex/noninteractive), [GitHub Action](https://developers.openai.com/codex/github-action), [managed configuration](https://learn.chatgpt.com/docs/enterprise/managed-configuration) (the developers.openai.com links redirect to learn.chatgpt.com).
- GitHub Copilot: [repository instructions](https://docs.github.com/en/copilot/how-tos/configure-custom-instructions/add-repository-instructions), [agent skills](https://docs.github.com/en/copilot/concepts/agents/about-agent-skills), [hooks reference](https://docs.github.com/en/copilot/reference/hooks-reference), [CLI programmatic reference](https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-programmatic-reference), [custom agents](https://docs.github.com/en/copilot/how-tos/copilot-on-github/customize-copilot/customize-cloud-agent/create-custom-agents).
- Gemini CLI: [GEMINI.md](https://geminicli.com/docs/cli/gemini-md/), [skills](https://geminicli.com/docs/cli/creating-skills/), [hooks](https://geminicli.com/docs/hooks/reference/), [headless mode](https://geminicli.com/docs/cli/headless/), [plan mode](https://geminicli.com/docs/cli/plan-mode/), [enterprise](https://geminicli.com/docs/cli/enterprise/), [GitHub Action](https://github.com/google-github-actions/run-gemini-cli).
