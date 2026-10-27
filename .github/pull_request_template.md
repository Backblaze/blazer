Summary:

## Motivation

Why this change is needed. Link the issue (`Fixes #123`) and, if one exists, `intent/<slug>/`.

## Changes

What changed. If this PR is stacked on another, name it and the merge order.

## Testing

Paste the tail of `make ci`. For each behavior this PR adds or fixes, name the test that fails when the
change is reverted. List any live-B2 checks you ran, and any you could not.

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
