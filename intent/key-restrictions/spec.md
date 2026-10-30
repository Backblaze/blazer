# Spec: expose a key's bucket restriction and name prefix
From: intent.md (Felipe Fumero). Issue: #227. Status: ready for review.
Skills applied: none beyond `AGENTS.md`, `REVIEW.md` and `docs/testing.md`.

Sources of truth:
- B2 `b2_create_key` (https://www.backblaze.com/apidocs/b2-create-key) and `b2_list_keys`
  (https://www.backblaze.com/apidocs/b2-list-keys): each key carries `bucketId` (v3; null when unrestricted) or
  `bucketIds` (v4; null when unrestricted) and `namePrefix` (null when unrestricted).
- Blazer's existing options `BucketIDs(ids ...string)` and `Prefix(prefix string)` (`b2/key.go`).

## Public API

```go
// BucketIDs returns the IDs of the buckets this application key is restricted to,
// or nil if the key is not restricted to any bucket. A key created with a single
// bucket reports one ID. The returned slice is a copy.
func (k *Key) BucketIDs() []string

// Prefix returns the file-name prefix this application key is restricted to, or
// "" if it has none.
func (k *Key) Prefix() string
```

## Requirements

- **R1. Accessors.** `*b2.Key` gains `BucketIDs() []string` and `Prefix() string` exactly as above. Both work for keys
  from `Client.CreateKey`, `Bucket.CreateKey` and `Client.ListKeys`.
- **R2. One representation across versions.** In `base`, a key's bucket IDs are `BucketIDs` when the response has a
  non-empty `bucketIds`, otherwise `[]string{BucketID}` when it has a non-empty `bucketId`, otherwise nil. An empty
  `bucketIds: []` (which decodes to a non-nil empty slice) normalizes to nil. The prefix is `namePrefix`, with null
  or absent reported as `""`.
- **R3. Unrestricted keys.** A key with no bucket restriction returns `nil` from `BucketIDs()` (not an empty slice),
  and `""` from `Prefix()`.
- **R4. Copies.** `BucketIDs()` returns a new slice each call, built so that nil stays nil (for example
  `append([]string(nil), ids...)`, never `make` plus `copy`); changing it does not change the key.
- **R5. `base.Key` fields.** `base.Key` gains `BucketIDs []string` and `Prefix string`, filled by
  `doCreateKeyWithState` (v3 and v4 create) and `ListKeys`.
- **R6. Tests.** Credential-free tests, with literal responses from the B2 docs' shapes:
  - `base`: v3 create with `bucketId` and `namePrefix`; v4 create and v4 list with two `bucketIds`; an unrestricted
    key with null fields.
  - `b2` (recording server): `Client.CreateKey` with `BucketIDs("b1","b2")` and `Prefix("p/")` reads both back;
    `Bucket.CreateKey` reads back its own bucket ID; `Client.ListKeys` returns restricted and unrestricted keys
    correctly; `BucketIDs()` returns a copy.
  - Both layers: a key whose response has `"bucketIds": []` reports `BucketIDs() == nil` (R3 holds for an empty
    array, not only for null).
  - **Live** (credential-gated, skipped without credentials like `TestCreateDeleteKey`): create a key restricted to
    the test bucket with `Prefix("live-227/")`, assert `BucketIDs()` and `Prefix()` on the created key and on the same
    key found through `Client.ListKeys`, then delete it. B2's real key responses have differed from the docs before
    (AGENTS.md lesson 9), so fixtures alone are not enough for this behavior.
- **R7. Changelog.** An Added entry.

## Design

- `base/base.go`: add the two fields to `Key`, and one helper `keyBucketIDs(k b2types.Key) []string`, used by both
  `doCreateKeyWithState` (via its `CreateKeyResponse`) and `ListKeys`.
- `b2/baseline.go`: `b2KeyInterface` and `b2Key` gain `bucketIDs()` and `prefix()`.
- `b2/backend.go`: `beKeyInterface` and `beKey` gain the same, forwarding.
- `b2/key.go`: the two public accessors; `BucketIDs` copies.
- Every test fake that implements these unexported interfaces gains the two methods.

## Deferred

- `options` (for example `["s3"]`), which `b2_list_keys` returns and b2-sdk-python exposes as
  `ApplicationKey.options`, is not surfaced here. A separate issue can add an accessor.

## Rejected alternatives

- **`NamePrefix()` / `Buckets()`.** They match B2's field names but not blazer's own `Prefix` and `BucketIDs` options;
  the read side should mirror the write side.
- **Bucket names instead of IDs.** B2 returns IDs only; resolving names costs a `b2_list_buckets` call per key and
  can fail. Callers that need names can map IDs themselves.
- **A single `Restriction` struct.** More API surface for two values; it can be added later without breaking these
  accessors.
- **Exposing the raw v3 `bucketId` separately.** Leaks the API version to callers; R2 normalizes it.

## How each constraint is met

| Constraint | Met by |
| --- | --- |
| Additive only | R1, R5: new accessors and fields; nothing removed or renamed |
| Names mirror the options | `BucketIDs`, `Prefix` |
| One representation for v3 and v4 | R2 |
| Expected values from the docs | R6 literals in the documented shapes |
| Builds on #220 | Stacked on #228's branch |

## Areas of concern

1. **Nil versus empty.** R3 chooses nil for "unrestricted" so callers can test `len(ids) == 0` or `ids == nil`. A
   reviewer may prefer documenting only `len == 0`. Owner: @ffumero2003.
2. **`base` is public.** Adding fields to `base.Key` is additive and source-compatible for callers that use field
   names; positional struct literals would break, and none are expected. Owner: @ffumero2003.
