# Intent: Object listing iterator contract
Author: Felipe Fumero. Issue: [#528](https://github.com/Backblaze/blazer/issues/528). Status: draft.

## Problem

The listing contract spans godoc, implementation, tests and change specs. Callers need one place to find its behavior and limits.

## Proposed outcome

The iterator selects objects in a bucket and retrieves results in pages. Callers can choose a prefix, delimiter, listing mode and page size. This standing spec records current behavior and test gaps.

## Affected users and APIs

Go applications use `Bucket.List`, `ObjectIterator` and `ListOption` values to list objects.

## Constraints

- Describe the code at `286e90d`. Do not change listing behavior.
- Cite code and tests for each requirement. State when no test pins a requirement.
- Keep the [listing prefix and cursor change spec](../listing-prefix-cursor/spec.md) as a separate record.

## Open questions

See the standing spec for unspecified ordering and error matching, and the concurrency constraint.
