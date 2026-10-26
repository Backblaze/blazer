# Review instructions

For human reviewers and review agents. Approval always comes from a code owner in
`.github/CODEOWNERS`; an agent's findings never approve or block on their own, and the agent that wrote
a change never reviews it.

## Before the passes

1. Read the linked issue and, if present, `intent/<slug>/spec.md` and `plan.md`.
2. Merge the PR onto current `main` locally and run `make ci`. Say what you ran.
3. For each behavior the PR claims to fix or add, revert that line and confirm a test fails. Name any
   behavior no test pins.

## Passes

Tag each finding with its pass.

- **Bugs**: logic errors, off-by-one thresholds, nil derefs, error paths that leak resources (unfinished
  large files, keys, replication config), lost wakeups, locks held across callbacks or channel sends.
- **Wire**: the request body matches the B2 docs for the API version used; omitted vs `null` vs `[]` is
  deliberate; `b2_update_bucket` sends only fields the caller changed; retry, reauth and `Retry-After`
  behave like sibling calls; gzip `Content-Encoding` downloads still report length and SHA1.
- **Security**: secrets (application keys, auth tokens, SSE-C keys) never logged or put in errors;
  capability checks fail closed; integer conversions are bounded (CodeQL).
- **Compliance**: the change matches the issue, `spec.md` and `plan.md`; the PR description matches the
  code; a `.changes/unreleased` entry exists and reads well for users; godoc says what is not supported;
  `AGENTS.md` is updated when the change makes it stale.

## What Important means here

Important: breaks existing callers, sends a wrong request to B2, loses or leaks data or credentials, hangs,
or leaves resources behind. Missing tests for a claimed behavior are Important. Style and naming are nits.

## Format

Start with what you checked and against what (issue number, live B2 or simulator, commands run). Then a
table sorted by severity: `Severity | File:Line | Pass: finding`. At most five nits; summarize the rest as
a count. Do not report generated files or what CI already enforces (gofmt, vet, lint).
