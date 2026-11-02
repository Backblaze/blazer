# Review instructions

For human reviewers and review agents. Approval always comes from a code owner in
`.github/CODEOWNERS`; an agent's findings never approve or block on their own, and the agent that wrote
a change never reviews it.

## What failure looks like here

Blazer fails by sending the wrong request to B2 (a dropped, extra or mis-shaped field), by leaving
resources behind (unfinished large files, keys, replication config), by hanging (a lock or a wait that
never ends), or by leaking a credential into a log or error. A test that passes either way is also a
failure: it claims coverage it does not give.

## Before the passes

1. Read the linked issue and, if present, `intent/<slug>/spec.md` and `plan.md`.
2. Merge the PR onto current `main` locally and run `make ci`. Say what you ran.
3. For each behavior the PR claims to fix or add, check the author's revert proof (see "Reviewing tests"). Re-run
   it yourself for changes to `base/`, wire, auth or credential code, and name any behavior no test pins.
4. Check that the description says what the author's own fresh-context review (`/code-review`,
   `/security-review` or a subagent) reported and how each finding was answered. If it is missing, report
   it under Compliance.
