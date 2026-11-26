# Intent: Sync product contract
Author: Felipe Fumero. Issue: [#528](https://github.com/Backblaze/blazer/issues/528). Status: draft.

## Problem

The `b2/sync` contract spans package documentation, implementation, tests and upgrade notes. Callers need one place to find its direction, comparison rules, limits and failure behavior.

## Proposed outcome

The package sends a local directory tree to a B2 bucket prefix. This standing spec records the current sync behavior. It identifies behavior that has no direct test pin.

## Affected users and APIs

Go applications that call `sync.Sync`, `WithCompare`, `WithConcurrency` and `WithDryRun` use this contract. The contract also covers `Result` and `FileError`.

## Constraints

- Describe the code at `286e90d`. Do not change sync behavior.
- Cite code, public documentation and opened tests for each requirement.
- State the observable result for dry runs, file failures, cancellation and unsupported platforms.
- Distinguish internal worker concurrency from a caller concurrency guarantee.
- Use [B2 contracts](../../b2-contracts.md), the README and [the upgrade guide](../../upgrading.md) as supporting context.

## Open questions

The standing spec records current behavior only. It does not select a new deletion, conflict or resume policy.
