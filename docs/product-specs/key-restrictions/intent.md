# Intent: expose a key's bucket restriction and name prefix
Author: Felipe Fumero. Issue: #227. Related: #220. Status: draft.

## Problem

B2 returns each application key's restrictions in `b2_create_key` and `b2_list_keys`: the buckets the key is
limited to (`bucketId` in a v3 response, `bucketIds` in v4) and its `namePrefix`. Blazer decodes all three into
`b2types.Key` (`internal/b2types/b2types.go`, `BucketID`, `BucketIDs`, `Prefix`), then drops them: `base.Key`
(`base/base.go`) has no field for them and `b2.Key` (`b2/key.go`) has no accessor.

A caller can create a key restricted to a bucket and a prefix (`BucketIDs(...)`, `Prefix(...)`), but cannot read
those restrictions back, either from the key it just created or from `Client.ListKeys`. Code that audits key
scope, including `bin/b2keys`, therefore cannot tell an all-buckets key from a key limited to one prefix of one
bucket.

## Proposed outcome

`b2.Key` reports the bucket IDs and the name prefix a key is restricted to, for keys returned by
`Client.CreateKey`, `Bucket.CreateKey` and `Client.ListKeys`, consistently across v3 single-bucket and v4
multi-bucket responses.

## Affected users and APIs

- New public read-only accessors on `*b2.Key`. No change to existing methods or options.
- `base.Key` gains two fields (`base` is importable but documented as low level).

## Constraints

- Additive only: no breaking change, no change to how keys are created.
- Names mirror the existing creation options (`BucketIDs`, `Prefix`) so the read side matches the write side.
- One representation for both API versions: callers must not need to know whether the response was v3 or v4.
- Expected values in tests come from the B2 docs for `b2_create_key` and `b2_list_keys`.
- Builds on #220 (#228), which edits the same `ListKeys` mapping.

## Open questions

1. Should `bin/b2keys` print the restrictions? Out of scope here; a follow-up can use the accessors.
