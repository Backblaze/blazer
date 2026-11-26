# Intent: Application key management contract
Author: Felipe Fumero. Issue: [#528](https://github.com/Backblaze/blazer/issues/528). Status: draft.

## Problem

Application key behavior spans the Go API, the `b2keys` tool, tests and change specs. Callers need one place to find the current contract.

## Proposed outcome

Callers can create, list and delete application keys. They can request capabilities, bucket restrictions, a prefix and an expiration.
This standing spec records current behavior and identifies requirements without a test pin.

## Affected users and APIs

Go applications use `Client.CreateKey`, `Bucket.CreateKey`, `Client.ListKeys` and `Key` methods. The `b2keys create` command also creates application keys.

## Constraints

- Describe the code at `286e90d`. Do not change key management behavior.
- Cite code and tests for each requirement. State when no test pins a requirement.
- Keep the [key restrictions change spec](../key-restrictions/spec.md) and [create-key wire change spec](../create-key-wire/spec.md) as separate records.
- Use the [keys and sessions guide](../../guides/keys-and-sessions.md) as supporting context.

## Open questions

See the standing spec for test coverage gaps and unspecified error matching.
