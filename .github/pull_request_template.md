Summary:

## Motivation

Why this change is needed. Link the issue (`Fixes #123`) and, if one exists, `intent/<slug>/`.

## Changes

What changed. If this PR is stacked on another, name it and the merge order.

## Testing

Paste the tail of `make ci`. For each behavior this PR adds or fixes, name the test that fails when the
change is reverted. List any live-B2 checks you ran, and any you could not.

## Checklist

- [ ] `make ci` passes locally.
- [ ] A `.changes/unreleased` entry exists (`make changelog-new`), or the PR is labelled `skip-changelog`.
- [ ] New or changed behavior is pinned by a test that asserts the request sent to B2.
- [ ] Godoc, README and `AGENTS.md` are updated if needed.
- [ ] The description matches the final code.
