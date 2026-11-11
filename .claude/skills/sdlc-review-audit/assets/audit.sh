#!/bin/bash
# Read-only audit of a repo against the code review adoption checklist.
# Usage: audit.sh [repo-dir] [--strict]
# Prints PASS / WARN / MISSING / UNKNOWN per check with the skill that fixes it.
# --strict exits 1 when anything is MISSING. Needs git and jq; uses gh for branch protection if logged in.
# Tests can set AUDIT_PROTECTION_JSON and AUDIT_RULESETS_JSON to skip the GitHub calls.
set -u
dir="."
strict=0
for a in "$@"; do
  case "$a" in
    --strict) strict=1 ;;
    *) dir="$a" ;;
  esac
done
cd "$dir" || { echo "cannot enter $dir" >&2; exit 2; }

missing=0
row() {   # row <STATUS> <check> <detail/fix>
  printf '%-8s %-44s %s\n' "$1" "$2" "$3"
  if [ "$1" = MISSING ]; then missing=$((missing + 1)); fi
  return 0
}
have_any() { local p; for p in "$@"; do [ -e "$p" ] && return 0; done; return 1; }
code_of() { grep -v '^[[:space:]]*#' "$1"; }   # a workflow without its comment lines
on_pr() { code_of "$1" | grep -qE '^\s*pull_request(_target)?:|^on:\s*pull_request|\[.*pull_request.*\]'; }
filtered() { code_of "$1" | grep -qE '^\s*paths(-ignore)?:'; }   # runs only on PRs touching some paths
ci_missing() {   # prints which of lint, types, tests, build have no sign in $1
  local m=""
  grep -qiE '(^|[^a-z])(lint|ruff|eslint|flake8|golangci|clippy|rubocop|format:check)' <<<"$1" || m="$m lint"
  grep -qiE 'tsc|mypy|pyright|typecheck|type-check|type check|cargo check|go vet' <<<"$1" || m="$m types"
  grep -qiE '(^|[^a-z])(test|tests|pytest|jest|vitest|unittest)([^a-z]|$)' <<<"$1" || m="$m tests"
  grep -qiE '(^|[^a-z])(build|compile)' <<<"$1" || m="$m build"
  echo "$m"
}

echo "Audit of $(pwd)"
echo

# --- Before the PR
inst=""
for f in AGENTS.md CLAUDE.md GEMINI.md .github/copilot-instructions.md; do [ -f "$f" ] && { inst=$f; break; }; done   # any assistant's
if [ -n "$inst" ]; then
  n=$(wc -l < "$inst" | tr -d ' ')
  if [ "$n" -gt 120 ]; then row WARN "agent instruction file" "$inst is $n lines; trim to what a new joiner needs (sdlc-agent-instructions)"
  else row PASS "agent instruction file" "$inst, $n lines"; fi
  grep -qiE 'make test|npm test|pytest|go test|cargo test|verif' "$inst" \
    && row PASS "instruction file lists how to verify work" "" \
    || row WARN "instruction file lists how to verify work" "add Commands and Verifying your work (sdlc-feedback-loop)"
  miss=""
  grep -qiE '(^|[^a-z])(test|pytest|jest|vitest|unittest)' "$inst" || miss="$miss test"   # whole words: "latest" is not a test
  grep -qiE '(^|[^a-z])(build|compile)' "$inst" || miss="$miss build"
  grep -qiE '(^|[^a-z])(lint|ruff|eslint|flake8|golangci|clippy|rubocop)' "$inst" || miss="$miss lint"
  [ -z "$miss" ] && row PASS "instruction file has test, build and lint commands" "" \
    || row WARN "instruction file has test, build and lint commands" "no mention of:$miss (N/A items are fine, say so)"
  grep -qiE '(^|[^-a-z])goals?([^a-z]|$)' "$inst" && grep -qiE 'non-goal|not in scope|out of scope' "$inst" \
    && row PASS "instruction file states goals and non-goals" "" \
    || row WARN "instruction file states goals and non-goals" "add a Goals and non-goals section (sdlc-agent-instructions)"
  grep -qE '\]\([^)]+\)|docs/|^@' "$inst" \
    && row PASS "instruction file points to other docs" "" \
    || row WARN "instruction file points to other docs" "link to docs instead of copying them (sdlc-agent-instructions)"
else
  row MISSING "agent instruction file" "no AGENTS.md, CLAUDE.md, GEMINI.md or .github/copilot-instructions.md; run sdlc-agent-instructions"
fi
# Hooks of any assistant: Claude Code, Codex, Gemini CLI and Copilot (references/ai-assistants.md).
hooks_in=""; hook_cmds=""
# A hook counts only with a handler that runs something; an event key with an empty list runs nothing.
# Present in config is not proof it runs: trust (Codex), managed-only settings and failures are not visible here.
for f in .claude/settings.json .codex/hooks.json .gemini/settings.json .github/copilot/settings.json .github/hooks/*.json; do
  [ -f "$f" ] || continue
  cmds=$(jq -r '(.hooks // {}) | objects | .. | objects | (.command, .bash, .powershell) | strings | select(length > 0)' "$f" 2>/dev/null)
  [ -n "$cmds" ] || continue
  hooks_in="$hooks_in $f"
  hook_cmds+="$cmds"$'\n'
done
if [ -f .codex/config.toml ] && grep -qE '^\[\[hooks\.' .codex/config.toml; then   # Codex inline TOML hooks
  cmds=$(awk '/^\[\[hooks\./ { h = 1; next } /^\[/ { h = 0 } h && /^[[:space:]]*command[[:space:]]*=/ { sub(/^[^=]*=[[:space:]]*/, ""); print }' .codex/config.toml)
  [ -n "$cmds" ] && { hooks_in="$hooks_in .codex/config.toml"; hook_cmds+="$cmds"$'\n'; }
fi
[ -n "$hooks_in" ] && row PASS "agent hooks configured" "in:$hooks_in" \
  || row WARN "agent hooks configured" "formatter, linter or policy hooks (sdlc-hook-gate, sdlc-policy-skill)"
fmt_re='prettier|eslint|biome|ruff|black|isort|flake8|pylint|gofmt|goimports|golangci|rustfmt|clippy|rubocop|shfmt|shellcheck|clang-format|ktlint|swiftlint|(^|[^a-z])(lint|format|fmt)([^a-z]|$)'
if have_any .pre-commit-config.yaml lefthook.yml .lefthook.yml .husky || grep -qiE "$fmt_re" <<<"$hook_cmds"; then
  row PASS "formatters or linters run automatically" ""
elif [ -n "$(tr -d '[:space:]' <<<"$hook_cmds")" ]; then
  row UNKNOWN "formatters or linters run automatically" "agent hooks run, but none names a formatter or linter; check what they run"
else
  row WARN "formatters or linters run automatically" "pre-commit, husky or an agent hook (sdlc-feedback-loop, sdlc-hook-gate)"
fi
n_skills=$(find .claude/skills .agents/skills .github/skills .gemini/skills -name SKILL.md 2>/dev/null | wc -l | tr -d ' ')
[ "$n_skills" -gt 0 ] && row PASS "agent skills" "$n_skills skills" \
  || row WARN "agent skills" "encode recurring policy as skills (sdlc-policy-skill)"

# --- On every PR
have_any REVIEW.md .github/REVIEW.md && row PASS "REVIEW.md" "" || row MISSING "REVIEW.md" "run sdlc-pr-review (Mode A)"
rv=$(cat REVIEW.md .github/REVIEW.md 2>/dev/null)
if [ -n "$rv" ]; then
  grep -qi 'nit' <<<"$rv" && grep -qiE 'at most|no more than|cap |limit' <<<"$rv" \
    && row PASS "REVIEW.md caps nits" "" || row WARN "REVIEW.md caps nits" "state a maximum number of nits per review (sdlc-pr-review)"
  grep -qiE 'do not report|skip|ignore|exclude|generated' <<<"$rv" \
    && row PASS "REVIEW.md lists paths to skip" "" || row WARN "REVIEW.md lists paths to skip" "name generated and vendored paths (sdlc-pr-review)"
  grep -qiE 'file and line|file:line|line number|cite|evidence' <<<"$rv" \
    && row PASS "REVIEW.md requires file and line evidence" "" || row WARN "REVIEW.md requires file and line evidence" "every finding cites file and line (sdlc-pr-review)"
fi
docs_rounds=$(grep -rliE 'review rounds?|rounds? of review|(three|3|max.*) rounds' REVIEW.md .github/REVIEW.md AGENTS.md CLAUDE.md GEMINI.md .github/*.md docs 2>/dev/null | head -1)
[ -n "$docs_rounds" ] && row PASS "review round cap documented" "$docs_rounds" \
  || row WARN "review round cap documented" "cap rounds, then re-review only the new diff (sdlc-review-tuning)"
have_any .github/pull_request_template.md pull_request_template.md docs/pull_request_template.md \
  && row PASS "PR template" "" || row MISSING "PR template (Testing, Risk, How to revert)" "sdlc-issue-templates"
if have_any .github/pull_request_template.md pull_request_template.md docs/pull_request_template.md; then
  tpl=$(cat .github/pull_request_template.md pull_request_template.md docs/pull_request_template.md 2>/dev/null)
  for s in Testing Risk revert; do
    grep -qi "$s" <<<"$tpl" || row WARN "PR template mentions $s" "add the section"
  done
fi
for f in feature bug task; do
  have_any ".github/ISSUE_TEMPLATE/$f.yml" ".github/ISSUE_TEMPLATE/$f.yaml" ".github/ISSUE_TEMPLATE/$f.md" \
    && row PASS "issue template: $f" "" || row MISSING "issue template: $f" "sdlc-issue-templates"
done
ai_review=""
pr_jobs=""   # job ids and names in PR-triggered workflows, to compare with the required checks
if compgen -G ".github/workflows/*.y*ml" >/dev/null; then
  row PASS "CI workflows" "$(ls .github/workflows/*.y*ml | wc -l | tr -d ' ') files"
  ci=$(cat .github/workflows/*.y*ml)
  pr_all=""; pr_every=""   # PR workflows, and those without path filters (they run on every PR)
  for w in .github/workflows/*.y*ml; do
    on_pr "$w" || continue
    pr_all+=$(code_of "$w")$'\n'
    pr_jobs+=$(code_of "$w" | awk '/^jobs:/ { j = 1; next } j && /^[^[:space:]]/ { j = 0 }
      j && /^  [A-Za-z0-9_-]+:[[:space:]]*$/ { k = $1; sub(/:$/, "", k); print k }
      j && /^    name:/ { sub(/^    name:[[:space:]]*/, ""); print }' | tr -d "\"'")$'\n'
    filtered "$w" || pr_every+=$(code_of "$w")$'\n'
  done
  if [ -z "$pr_all" ]; then
    row WARN "CI runs lint, types, tests and build" "no workflow runs on pull_request (sdlc-cicd)"
  elif [ -z "$(ci_missing "$pr_every")" ]; then
    row PASS "CI runs lint, types, tests and build" "in pull_request workflows without path filters; job if: conditions not checked"
  elif [ -z "$(ci_missing "$pr_all")" ]; then
    row UNKNOWN "CI runs lint, types, tests and build" "some only in path-filtered PR workflows; confirm they run on every PR"
  else
    row WARN "CI runs lint, types, tests and build" "no sign in PR workflows of:$(ci_missing "$pr_all") (may live inside another script; N/A is fine)"
  fi
  grep -qiE 'red_check|red-check' <<<"$pr_all" \
    && row PASS "new tests fail without the change (red check in CI)" "shows the tests detect the change, not the order they were written in" \
    || row WARN "new tests fail without the change (red check in CI)" "no PR job checks that new tests fail without the change (sdlc-feedback-loop)"
  ro=$(grep -hoE 'runs-on:[^#]*' .github/workflows/*.y*ml | sed 's/runs-on: *//' | sort -u | tr '\n' ';')
  row UNKNOWN "CI covers every platform you ship" "runs-on: ${ro:-none found}; compare with your supported platforms"
  appr=""
  for w in .github/workflows/*.y*ml; do
    grep -qiE "gh pr review[^#]*(--approve|[[:space:]]-a([[:space:]]|$))|auto-approve|approve-pull-request|event[\"']?[[:space:]]*[:=][[:space:]]*[\"']?APPROVE" "$w" \
      && appr="$appr $(basename "$w")"
  done
  [ -z "$appr" ] && row PASS "no workflow approves pull requests" "" \
    || row WARN "no workflow approves pull requests" "approves in:$appr; a bot approval must never satisfy a required human review (sdlc-risk-tiers)"
  if grep -qiE 'canary|staged|prerelease|channel|stable|environment:' <<<"$ci"; then
    row PASS "staged release path" "workflow mentions canary, channel, stable or environments"
  else
    row UNKNOWN "staged release path" "not detected in workflows; ask the owner (sdlc-cicd)"
  fi
  unguarded=""
  for w in .github/workflows/*.y*ml; do
    if on_pr "$w" && ! code_of "$w" | grep -qE 'if:.*dependabot\[bot\]'; then
      unguarded="$unguarded $(basename "$w")"
    fi
  done
  [ -z "$unguarded" ] && row PASS "pull_request workflows guard Dependabot" "" \
    || row WARN "pull_request workflows guard Dependabot" "no dependabot guard in:$unguarded"
  # A job in a path-filtered workflow does not run on every PR, so it alone is not coverage.
  sec=""; sec_part=""
  for w in .github/workflows/*.y*ml; do
    on_pr "$w" && code_of "$w" | grep -qiE 'security-review|codeql|semgrep' || continue
    filtered "$w" && sec_part="$sec_part $(basename "$w")" || sec="$sec $(basename "$w")"
  done
  if [ -n "$sec" ]; then row PASS "automated security pass in CI" "on every pull request in:$sec"
  elif [ -n "$sec_part" ]; then row UNKNOWN "automated security pass in CI" "only in path-filtered workflows:$sec_part; confirm it runs on every PR"
  else row MISSING "automated security pass in CI" "no PR-triggered security job (CodeQL default setup is a setting: check it by hand) (sdlc-pr-review)"; fi
  ai_part=""
  ai_re='claude-code-action|claude -p|codex-action|codex exec|run-gemini-cli|gemini -p|copilot -p|code-review'
  for w in .github/workflows/*.y*ml; do   # a PR-triggered job that runs an AI assistant to review
    on_pr "$w" || continue
    code_of "$w" | grep -qiE "$ai_re" && code_of "$w" | grep -qi 'review' || continue
    filtered "$w" && ai_part="$ai_part $(basename "$w")" || ai_review="$ai_review $(basename "$w")"
  done
  if [ -n "$ai_review" ]; then
    row PASS "AI review wired in CI" "PR-triggered review in:$ai_review"
  elif [ -n "$ai_part" ]; then
    ai_review="$ai_part"
    row UNKNOWN "AI review wired in CI" "only in path-filtered workflows:$ai_part; confirm it reviews every PR"
  elif grep -rqliE "$ai_re|claude|codex|gemini" .github/workflows; then
    row UNKNOWN "AI review wired in CI" "an AI assistant runs in CI but no PR-triggered review job was found; check by hand, or for managed review (Code Review, Copilot code review)"
  else
    row WARN "AI review wired in CI" "managed review (Code Review, Copilot code review) or an assistant's review job: claude-code-action, codex-action, run-gemini-cli (sdlc-pr-review, sdlc-cicd)"
  fi
else
  row MISSING "CI workflows" "required checks need CI (sdlc-cicd)"
fi
# Depth is read from the review instructions, or from the code (not comments) of the AI review workflows.
depth_re='review depth|strictness|max-findings|max_findings|min-severity|severity threshold'
depth=$(grep -lsiE "$depth_re" REVIEW.md .github/REVIEW.md .claude/settings.json .github/copilot-instructions.md 2>/dev/null | head -1)
for w in $ai_review; do
  [ -z "$depth" ] && code_of ".github/workflows/$w" | grep -qiE "$depth_re|--effort|^[[:space:]]*(review_)?effort:" && depth=".github/workflows/$w"
done
if [ -n "$depth" ]; then
  row PASS "AI review depth set deliberately" "set in $depth; confirm it is stricter for higher-cost repos"
elif [ -n "$ai_review" ]; then
  row WARN "AI review depth set deliberately" "AI review runs with default depth; set strictness by risk (sdlc-review-tuning)"
else
  row N/A "AI review depth set deliberately" "no PR-triggered AI review found"
fi

# --- Approval
co_file=""   # GitHub reads only the first of these
for f in .github/CODEOWNERS CODEOWNERS docs/CODEOWNERS; do [ -f "$f" ] && { co_file=$f; break; }; done
[ -n "$co_file" ] && row PASS "CODEOWNERS" "$co_file" || row MISSING "CODEOWNERS" "always-human paths (sdlc-risk-tiers)"
if [ -n "$co_file" ]; then
  # owns <path>: as on GitHub, the last rule whose pattern matches the path decides, and it must name
  # an owner (an owner-less rule removes ownership). Patterns follow CODEOWNERS glob rules.
  owns() {
    awk -v path="$1" '
      function glob2re(p,   anch, out, i, c, n, tail) {
        anch = (substr(p, 1, 1) == "/"); if (anch) p = substr(p, 2)
        if (substr(p, length(p), 1) == "/") p = substr(p, 1, length(p) - 1)
        tail = (substr(p, length(p) - 1) == "/*") ? "" : "(/.*)?"   # docs/* owns docs/a.md, not docs/x/b.md
        if (index(p, "/") > 0) anch = 1
        out = ""; n = length(p)
        for (i = 1; i <= n; i++) {
          c = substr(p, i, 1)
          if (substr(p, i, 3) == "**/") { out = out "(.*/)?"; i += 2 }
          else if (substr(p, i, 2) == "**") { out = out ".*"; i++ }
          else if (c == "*") out = out "[^/]*"
          else if (c == "?") out = out "[^/]"
          else if (index(".+()|^$[]{}\\", c)) out = out "\\" c
          else out = out c
        }
        return (anch ? "^" : "^(.*/)?") out tail "$"
      }
      { sub(/[[:space:]]+#.*$/, "") }   # an inline comment is not an owner
      !/^[[:space:]]*(#|$)/ {
        for (i = 2; i <= NF; i++)   # GitHub skips a line with an owner that is not @user, @org/team or an email
          if ($i !~ /^@[A-Za-z0-9][A-Za-z0-9-]*(\/[A-Za-z0-9._-]+)?$/ && $i !~ /^[^@]+@[^@]+\.[^@]+$/) next
        if (path ~ glob2re($1)) n = NF
      }
      END { exit !(n >= 2) }' "$co_file"
  }
  unowned=""
  wfs=$(ls .github/workflows/*.y*ml 2>/dev/null); [ -n "$wfs" ] || wfs=".github/workflows/ci.yml"
  for w in $wfs; do owns "$w" || unowned="$unowned $(basename "$w")"; done
  [ -z "$unowned" ] && row PASS "CODEOWNERS covers CI workflows" "" \
    || row WARN "CODEOWNERS covers CI workflows" "no owner for:$unowned; workflow files must always need a person"
  gaps=""
  for k in auth billing payment install release secret credential migration; do
    while IFS= read -r f; do
      owns "${f#./}" || { gaps="$gaps $k"; break; }
    done < <(find . \( -name .git -o -name node_modules -o -name vendor \) -prune -o -ipath "*$k*" -print 2>/dev/null)
  done
  # Every file inside the assistants' config folders and evals/ too, so a later owner-less rule cannot hide one.
  agent_gaps=""
  n_gaps=0
  while IFS= read -r f; do
    owns "${f#./}" && continue
    n_gaps=$((n_gaps + 1))
    [ "$n_gaps" -le 5 ] && agent_gaps="$agent_gaps ${f#./}"
  done < <(find . \( -name .git -o -name node_modules -o -name vendor \) -prune -o \( -name CLAUDE.md -o -name CLAUDE.local.md \
             -o -name AGENTS.md -o -name AGENTS.override.md -o -name GEMINI.md -o -name REVIEW.md -o -name .mcp.json -o -name risk-tiers.json -o -name classify.py \
             -o -path ./.github/copilot-instructions.md -o -path ./.github/instructions -o -path './.github/instructions/*' \
             -o -path ./.github/agents -o -path './.github/agents/*' -o -path ./.github/hooks -o -path './.github/hooks/*' \
             -o -path ./.github/skills -o -path './.github/skills/*' \
             -o -name .claude -o -path '*/.claude/*' -o -name .codex -o -path '*/.codex/*' -o -name .gemini -o -path '*/.gemini/*' \
             -o -name .agents -o -path '*/.agents/*' \
             -o -path ./evals -o -path './evals/*' \) -print 2>/dev/null)
  [ "$n_gaps" -gt 5 ] && agent_gaps="$agent_gaps and $((n_gaps - 5)) more"
  [ -z "$agent_gaps" ] && row PASS "CODEOWNERS covers agent configuration" "" \
    || row WARN "CODEOWNERS covers agent configuration" "no owner for:$agent_gaps; a PR could change the agent's rules, tools or tier without a person (sdlc-risk-tiers)"
  [ -z "$gaps" ] && row PASS "CODEOWNERS covers sensitive paths" "" \
    || row WARN "CODEOWNERS covers sensitive paths" "paths matching:$gaps exist but CODEOWNERS never names them (sdlc-risk-tiers)"
  bots=$(awk '{ sub(/[[:space:]]+#.*$/, "") } !/^[[:space:]]*(#|$)/ { for (i = 2; i <= NF; i++) if (tolower($i) ~ /\[bot\]|-bot$|^@(claude|copilot|codex|gemini|github-actions)/) print $i }' "$co_file" | sort -u | tr '\n' ' ')
  [ -z "$bots" ] && row PASS "CODEOWNERS names only people and teams" "" \
    || row WARN "CODEOWNERS names only people and teams" "bot owners: $bots- a bot can satisfy code-owner review (sdlc-risk-tiers)"
fi
have_any risk-tiers.json .github/risk-tiers.json ops/risk-tiers.json \
  && row PASS "risk tiers config" "" || row WARN "risk tiers config (risk-tiers.json)" "sdlc-risk-tiers"
for f in risk-tiers.json .github/risk-tiers.json ops/risk-tiers.json; do
  [ -f "$f" ] || continue
  ta=$(jq -r '(.trusted_authors // []) | join(",")' "$f" 2>/dev/null)
  if [ -z "$ta" ]; then row N/A "lighter path limited to trusted authors" "no trusted_authors in $f, so no lighter path"
  elif grep -q 'owning-team-member-login' <<<"$ta"; then row WARN "lighter path limited to trusted authors" "replace the placeholder login in $f"
  elif grep -qiE '\[bot\]|-bot(,|$)|(^|,)(claude|copilot|codex|gemini|github-actions)' <<<"$ta"; then row WARN "lighter path limited to trusted authors" "a bot or agent account is listed in $f; only people may be trusted"
  else row PASS "lighter path limited to trusted authors" "$ta"; fi
  break
done

obj() { [ -n "$1" ] && jq -e 'type == "object" and (has("message") | not)' >/dev/null 2>&1 <<<"$1"; }   # not an API error
arr() { [ -n "$1" ] && jq -e 'type == "array"' >/dev/null 2>&1 <<<"$1"; }

# --- Branch protection, rulesets and merge queue (GitHub; classic protection and ruleset details need admin read)
protection="${AUDIT_PROTECTION_JSON:-}"
default_branch="${AUDIT_DEFAULT_BRANCH:-main}"
rulesets="${AUDIT_RULESETS_JSON:-}"
branch_rules="${AUDIT_BRANCH_RULES_JSON:-}"   # active ruleset rules on the branch; needs only read access
wf_perms="${AUDIT_WORKFLOW_PERMS_JSON:-}"     # repo Actions settings; needs admin read
collabs="${AUDIT_COLLABORATORS_JSON:-}"       # accounts with write access; needs push access
if [ -z "$protection$rulesets$branch_rules$wf_perms$collabs" ] && command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
  repo=$(gh repo view --json nameWithOwner,defaultBranchRef -q '.nameWithOwner + " " + .defaultBranchRef.name' 2>/dev/null)
  if [ -n "$repo" ]; then
    nwo=${repo% *}; branch=${repo#* }; default_branch=$branch
    # A failed call prints GitHub's error body, so keep a response only when the call succeeded.
    protection=$(gh api "repos/$nwo/branches/$branch/protection" 2>/dev/null) \
      || { grep -q '"Branch not protected"' <<<"$protection" && protection='{}' || protection=""; }
    wf_perms=$(gh api "repos/$nwo/actions/permissions/workflow" 2>/dev/null) || wf_perms=""
    branch_rules=$(gh api "repos/$nwo/rules/branches/$branch" --paginate 2>/dev/null) \
      && branch_rules=$(jq -s 'add // []' <<<"$branch_rules" 2>/dev/null) || branch_rules=""
    collabs=$(gh api "repos/$nwo/collaborators?permission=push" --paginate 2>/dev/null) \
      && collabs=$(jq -s 'add // []' <<<"$collabs" 2>/dev/null) || collabs=""
    rulesets=$(gh api "repos/$nwo/rulesets" 2>/dev/null) || rulesets=""
    ids=$(jq -r '.[].id' <<<"$rulesets" 2>/dev/null || true)
    if [ -n "$ids" ]; then
      detail="[]"
      for id in $ids; do
        # An unreadable ruleset keeps its summary entry (no rules, no bypass actors), so it reads as unknown.
        r=$(gh api "repos/$nwo/rulesets/$id" 2>/dev/null) && obj "$r" \
          || r=$(jq -c --argjson id "$id" 'first(.[] | select(.id == $id))' <<<"$rulesets")
        d=$(jq -c --argjson r "$r" '. + [$r]' <<<"$detail" 2>/dev/null) && detail="$d"
      done
      rulesets="$detail"
    fi
  fi
fi
# A ruleset counts only when it is active and targets the default branch.
# Ref patterns are fnmatch globs (refs/heads/release/*), in both include and exclude.
applies='def g2r: gsub("(?<c>[.+()|^$\\\\{}])"; "\\\(.c)") | gsub("\\*\\*"; "\u0001") | gsub("\\*"; "[^/]*")
    | gsub("\u0001"; ".*") | gsub("\\?"; "[^/]") | "^" + . + "$";
  def refmatch($b): (if . == "~DEFAULT_BRANCH" then "refs/heads/" + $b elif . == "~ALL" then "**" else . end) as $p
    | ("refs/heads/" + $b) | test($p | g2r);
  def applies($b): .enforcement == "active"
    and ((.conditions.ref_name.include // []) | any(refmatch($b)))
    and (((.conditions.ref_name.exclude // []) | any(refmatch($b))) | not);'
if obj "$protection" || arr "$branch_rules"; then
  # Effective protection: classic branch protection and the active ruleset rules on the branch, combined.
  obj "$protection" && p="$protection" || p='{}'
  arr "$branch_rules" && r="$branch_rules" || r='[]'
  eff=$(jq -n --argjson p "$p" --argjson r "$r" '
    def rv: $p.required_pull_request_reviews;
    def pr: [$r[] | select(.type == "pull_request") | .parameters // {}];
    { reviews: ((rv != null) or (pr | length > 0)),
      approvals: ([rv.required_approving_review_count // 0] + [pr[] | .required_approving_review_count // 0] | max),
      code_owner: ((rv.require_code_owner_reviews // false) or any(pr[]; .require_code_owner_review == true)),
      stale: ((rv.dismiss_stale_reviews // false) or any(pr[]; .dismiss_stale_reviews_on_push == true)),
      threads: (($p.required_conversation_resolution.enabled // false) or any(pr[]; .required_review_thread_resolution == true)),
      checks: (((($p.required_status_checks.contexts // []) + (($p.required_status_checks.checks // []) | map(.context))) | length > 0)
               or any($r[]; .type == "required_status_checks")),
      contexts: ([($p.required_status_checks.contexts // [])[], (($p.required_status_checks.checks // [])[] | .context)]
                 + [$r[] | select(.type == "required_status_checks") | .parameters.required_status_checks[]?.context]),
      merge_queue: any($r[]; .type == "merge_queue"),
      scanning: any($r[]; .type == "code_scanning"
               and ((.parameters.code_scanning_tools // []) | any((.security_alerts_threshold // "none") != "none"))) }')
  is() { jq -e ".$1" >/dev/null <<<"$eff"; }
  # Not found in what we could read: MISSING when classic protection was readable, else UNKNOWN (it may set it).
  absent() { if obj "$protection"; then row MISSING "$1" "$2"; else row UNKNOWN "$1" "not in the active rulesets; classic protection needs admin read, check by hand"; fi; }
  is reviews && row PASS "branch protection: PR reviews required" "" \
    || absent "branch protection: PR reviews required" "sdlc-risk-tiers"
  [ "$(jq .approvals <<<"$eff")" -ge 1 ] && row PASS "branch protection: at least one approving review" "approvals from apps or bot accounts with write access also count; keep them off the repo" \
    || { have_any risk-tiers.json .github/risk-tiers.json ops/risk-tiers.json \
         && row WARN "branch protection: at least one approving review" "required count is 0; fine only if a required check enforces the classifier's humans_required (sdlc-risk-tiers)" \
         || absent "branch protection: at least one approving review" "required count is 0 (sdlc-risk-tiers)"; }
  # Bypass is checked in both places a review rule can live: classic protection and every active ruleset
  # on the branch that has a pull_request rule. Each side is none, clear, bypass or unknown.
  if ! obj "$protection"; then classic=unknown
  elif jq -e '.required_pull_request_reviews == null' >/dev/null <<<"$protection"; then classic=none
  elif jq -e '((.required_pull_request_reviews.bypass_pull_request_allowances // {}) | ((.users // []) + (.teams // []) + (.apps // [])) | length == 0)
              and .enforce_admins.enabled == true' >/dev/null <<<"$protection"; then classic=clear
  else classic=bypass; fi
  arr "$rulesets" && rsj="$rulesets" || rsj=null
  if arr "$branch_rules"; then
    # rules/branches names the ruleset behind each rule GitHub enforces here, whatever pattern it targets.
    rs=$(jq -rn --argjson br "$branch_rules" --argjson rs "$rsj" '
      def can_bypass: ((.bypass_actors // []) | length > 0) or ((.current_user_can_bypass // "never") != "never");
      ([$br[] | select(.type == "pull_request") | .ruleset_id] | unique) as $ids
      | if ($ids | length) == 0 then "none"
        elif ($rs | type) != "array" then "unknown"
        else [$ids[] as $i | $rs[] | select(.id == $i)] as $hit
          | if any($hit[]; can_bypass) then "bypass:" + ([$hit[] | select(can_bypass) | .name] | join(", "))
            elif ($hit | length) < ($ids | length) or any($hit[]; has("bypass_actors") | not) then "unknown"
            else "clear" end end' 2>/dev/null) || rs=unknown
  elif arr "$rulesets"; then
    rs=$(jq -r --arg b "$default_branch" "$applies"'
      def can_bypass: ((.bypass_actors // []) | length > 0) or ((.current_user_can_bypass // "never") != "never");
      [.[] | select(applies($b) and ((.rules // []) | any(.type == "pull_request")))] as $rs
      | if ($rs | length) == 0 then (if any(.[]; has("rules") | not) then "unknown" else "none" end)
        elif any($rs[]; can_bypass) then "bypass:" + ([$rs[] | select(can_bypass) | .name] | join(", "))
        elif all($rs[]; has("bypass_actors")) then "clear"
        else "unknown" end' <<<"$rulesets" 2>/dev/null) || rs=unknown
  else rs=unknown; fi
  if [ "$classic" = bypass ] || [ "${rs%%:*}" = bypass ]; then
    why=""
    [ "$classic" = bypass ] && why="branch protection: admins (enforce_admins off) or listed users, teams or apps; "
    [ "${rs%%:*}" = bypass ] && why="${why}rulesets with bypass actors: ${rs#bypass:}; "
    row WARN "branch protection: no bypass of PR reviews" "${why%; } (sdlc-risk-tiers)"
  elif [ "$classic" = unknown ] || [ "$rs" = unknown ]; then
    row UNKNOWN "branch protection: no bypass of PR reviews" "classic protection or ruleset bypass actors need admin read; check by hand"
  elif [ "$classic" = none ] && [ "$rs" = none ]; then
    row N/A "branch protection: no bypass of PR reviews" "no review rule to bypass"
  else
    row PASS "branch protection: no bypass of PR reviews" ""
  fi
  is code_owner && row PASS "branch protection: code owner review" "" \
    || absent "branch protection: code owner review" "sdlc-risk-tiers"
  is stale && row PASS "branch protection: dismiss stale approvals" "" \
    || absent "branch protection: dismiss stale approvals" "approvals are per commit (sdlc-risk-tiers)"
  if ! is checks; then
    absent "branch protection: required status checks" "make CI and review jobs required"
  else
    required=$(jq -r '.contexts | unique | join(", ")' <<<"$eff")
    match=$(jq -r --arg jobs "$pr_jobs" '($jobs | split("\n") | map(select(length > 0) | ascii_downcase)) as $j
      | [.contexts[] | ascii_downcase | select(. as $c | any($j[]; $c == . or ($c | startswith(. + " ("))))] | length' <<<"$eff")
    if [ -z "$(tr -d '[:space:]' <<<"$pr_jobs")" ] || [ "${match:-0}" -gt 0 ]; then
      row PASS "branch protection: required status checks" "required: $required"
    else
      row WARN "branch protection: required status checks" "required ($required) match no PR workflow job; make the CI and security jobs required"
    fi
  fi
  is threads && row PASS "branch protection: threads must be resolved" "" \
    || absent "branch protection: threads must be resolved" "every finding gets fixed or closed with a reason"
else
  row UNKNOWN "branch protection" "needs gh and read access to the repo; check settings by hand"
fi
if obj "$wf_perms" && jq -e 'has("can_approve_pull_request_reviews")' >/dev/null <<<"$wf_perms"; then
  jq -e '.can_approve_pull_request_reviews == false' >/dev/null <<<"$wf_perms" \
    && row PASS "GitHub Actions cannot approve pull requests" "" \
    || row WARN "GitHub Actions cannot approve pull requests" "a workflow token can approve PRs; turn off \"Allow GitHub Actions to create and approve pull requests\" (sdlc-risk-tiers)"
else
  row UNKNOWN "GitHub Actions cannot approve pull requests" "needs admin read of the repo's Actions settings; check by hand"
fi
rbots=""
arr "$collabs" && rbots=$(jq -r '[.[] | select(.type == "Bot" or (.login | test("\\[bot\\]$|-bot$"; "i"))) | .login] | join(" ")' <<<"$collabs")
if [ -n "$rbots" ]; then
  row WARN "only people can supply required approvals" "bot accounts with write access: $rbots; their approvals count (sdlc-risk-tiers)"
elif arr "$collabs"; then
  row UNKNOWN "only people can supply required approvals" "no bot collaborators with write access; GitHub Apps with write access can still approve, so check installed apps by hand"
else
  row UNKNOWN "only people can supply required approvals" "needs push access to list collaborators; check collaborators and installed GitHub Apps by hand"
fi
if arr "$branch_rules"; then
  is merge_queue && row PASS "merge queue" "active rule on $default_branch" \
    || row WARN "merge queue" "no active merge queue rule on $default_branch; checks should re-run on current main (sdlc-cicd)"
  is scanning && row PASS "open security findings block the merge" "active code scanning rule with a security alert threshold" \
    || row UNKNOWN "open security findings block the merge" "no active code scanning rule with an alert threshold found; check by hand"
elif arr "$rulesets"; then
  if jq -e --arg b "$default_branch" "$applies"' any(.[]; applies($b) and ((.rules // []) | any(.type == "merge_queue")))' >/dev/null <<<"$rulesets"; then
    row PASS "merge queue" "active ruleset on $default_branch"
  elif jq -e 'length > 0 and any(.[]; has("rules") | not)' >/dev/null <<<"$rulesets"; then
    row UNKNOWN "merge queue" "rulesets exist but their rules were not readable; check by hand"
  else
    row WARN "merge queue" "no active merge queue rule on $default_branch; checks should re-run on current main (sdlc-cicd)"
  fi
  if jq -e --arg b "$default_branch" "$applies"' any(.[]; applies($b) and ((.rules // []) | any(.type == "code_scanning"
        and ((.parameters.code_scanning_tools // []) | any((.security_alerts_threshold // "none") != "none")))))' >/dev/null <<<"$rulesets"; then
    row PASS "open security findings block the merge" "active code scanning rule with a security alert threshold"
  else
    row UNKNOWN "open security findings block the merge" "no active code scanning rule with an alert threshold found; check by hand"
  fi
else
  row UNKNOWN "merge queue" "needs gh and admin read access; check settings by hand"
  row UNKNOWN "open security findings block the merge" "needs gh and admin read access; check settings by hand"
fi

# --- Measure and improve
# A folder counts only when it holds something: an empty evals/ tests nothing.
n_evals=$(find evals -maxdepth 1 -name '*.json' -type f 2>/dev/null | wc -l | tr -d ' ')
n_review=$(find evals/review -name case.json -type f 2>/dev/null | wc -l | tr -d ' ')
n_incidents=$(find docs/incident-reviews incident-reviews -type f ! -name '.*' 2>/dev/null | wc -l | tr -d ' ')
[ "$n_evals" -gt 0 ] && row PASS "evals/" "$n_evals eval file(s)" \
  || row WARN "evals/" "no eval files in evals/; regression-test the agent configuration (sdlc-evals)"
[ "$n_review" -gt 0 ] && row PASS "reviewer eval set (evals/review)" "$n_review case(s)" \
  || row WARN "reviewer eval set (evals/review)" "no cases; incidents become reviewer evals (sdlc-review-tuning)"
have_any intent && row PASS "intent/ artifact home" "" || row WARN "intent/ artifact home" "sdlc-intent"
[ "$n_incidents" -gt 0 ] && row PASS "incident reviews kept" "$n_incidents review(s)" \
  || row WARN "incident reviews kept" "use the template in sdlc-close-the-loop"

# --- Merge and after (mostly process: when the repo cannot show it, the row is UNKNOWN, not MISSING)
manifests="package.json requirements*.txt pyproject.toml go.mod Gemfile Cargo.toml pom.xml build.gradle"
flags=$(grep -lsiE 'launchdarkly|unleash|flagsmith|growthbook|openfeature|flipper|splitio|configcat|statsig' $manifests 2>/dev/null | head -1)
hosted=$(grep -lsiE 'launchdarkly|unleash|flagsmith|growthbook|splitio|configcat|statsig' $manifests 2>/dev/null | head -1)
if [ -n "$flags" ]; then
  row PASS "feature flags in use" "flag library in $flags"
  if [ -n "$hosted" ]; then
    row PASS "flags can be turned off without a deploy" "hosted flag service SDK in $hosted; confirm kill switches exist"
  else
    row UNKNOWN "flags can be turned off without a deploy" "the flag library may read deploy-time config; ask the owner (sdlc-cicd)"
  fi
else
  row UNKNOWN "feature flags in use" "no flag library in dependencies; ask whether new behavior ships behind flags"
  row UNKNOWN "flags can be turned off without a deploy" "no flag service found; ask whether a kill switch needs a release (sdlc-cicd)"
fi
if have_any docs/rollback.md ROLLBACK.md docs/runbooks runbooks RUNBOOK.md docs/runbook.md \
   || grep -qsiE 'roll ?back' README.md AGENTS.md CLAUDE.md GEMINI.md RELEASE.md docs/*.md; then
  row PASS "rollback path documented" ""
else
  row WARN "rollback path documented" "write the one-step revert or rollback down (sdlc-cicd)"
fi
if have_any ONCALL.md docs/oncall.md docs/on-call.md docs/runbooks runbooks RUNBOOK.md INCIDENT_RESPONSE.md docs/incident-response.md; then
  row PASS "on-call or incident process documented" ""
else
  row WARN "on-call or incident process documented" "name an owner and a process (sdlc-close-the-loop)"
fi
if git rev-parse --git-dir >/dev/null 2>&1; then
  reverts=$(git log --since=180.days -i --grep='^revert' --oneline 2>/dev/null | wc -l | tr -d ' ')
  if [ "$(git rev-parse --is-shallow-repository 2>/dev/null)" = true ]; then
    row UNKNOWN "reverts feed back into evals or incident reviews" "shallow clone, so reverts cannot be counted; fetch full history or check revert PRs by hand"
  elif [ "$reverts" -eq 0 ]; then
    row PASS "reverts feed back into evals or incident reviews" "no reverts in 180 days"
  elif [ "$n_incidents" -gt 0 ] || [ "$n_review" -gt 0 ]; then
    row UNKNOWN "reverts feed back into evals or incident reviews" "$reverts reverts in 180 days; reviews or reviewer evals exist, so check by hand that they cover these"
  else
    row WARN "reverts feed back into evals or incident reviews" "$reverts reverts in 180 days and no reviews or reviewer evals (sdlc-close-the-loop)"
  fi
else
  row UNKNOWN "reverts feed back into evals or incident reviews" "not a git repository"
fi

echo
echo "MISSING: $missing"
[ "$strict" = 1 ] && [ "$missing" -gt 0 ] && exit 1
exit 0
