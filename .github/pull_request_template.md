Summary:

## Motivation

Why this change is needed. Link the issue (`Fixes #123`) and, if one exists, `intent/<slug>/`.

## Changes

What changed. If this PR is stacked on another, name it and the merge order.

## Testing

Paste the tail of `make ci`. For each behavior this PR adds or fixes, name the test that fails when the
change is reverted. List any live-B2 checks you ran, and any you could not. See
[docs/testing-standard.md](docs/testing-standard.md).

- [ ] RED first: the new test failed before the fix (output above).
- [ ] Revert proof: with the production change reverted, the new test fails (output above).
- [ ] Unit: `make ci` (tail above).
- [ ] Integration: ran (output above), or NOT RUN and why (for example no credentials).
- [ ] Contract: a claim was added or changed and its row and check are updated, or "no claim changed".
- [ ] System (simulator): ran (output above), or "not applicable" and why.
- [ ] Security: I touched auth, logging, errors, URLs, credentials or parsing and added or updated a security test, or "not touched".
- [ ] No test was deleted, skipped, loosened or retired, or a human approved it (link).
- [ ] Each new test states its evidence level (L1 to L5). No test was added only for coverage.

## Risk

What could break for existing callers, and whether the request sent to B2 changes. Name the riskiest
part of the diff.

## How to revert

Whether reverting this commit is enough, or whether callers would need to pin the previous release
(new exported API, changed wire format, behavior others may already depend on).

## Checklist

- [ ] `make ci` passes locally.
- [ ] A fresh-context review (`/code-review`, `/security-review` or a subagent) ran on the diff; findings are fixed or answered below.
- [ ] A `.changes/unreleased` entry exists (`make changelog-new`), or the PR is labelled `skip-changelog`.
- [ ] New or changed behavior is pinned by a test that asserts the request sent to B2.
- [ ] Godoc, README and `AGENTS.md` are updated if needed.
- [ ] The description matches the final code.
