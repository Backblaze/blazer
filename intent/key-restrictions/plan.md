# Plan: expose a key's bucket restriction and name prefix (from intent.md and spec.md)

Branch: `feat-b2-issue-227-key-restrictions`, stacked on #220's branch (`fix-base-issue-220-listkeys-capabilities`,
PR #228); #228 merges first. One PR for #227 carries this folder, the code and the tests. Apply the lean-code ladder
in `AGENTS.md`'s "Things agents get wrong here": reuse `b2types.Key`'s decoded fields and the existing
create/list paths; add one helper, no new types.

## Files that change

```
base/base.go                         (edit)  R2, R5: Key fields, keyBucketIDs helper, two mappings
b2/baseline.go                       (edit)  R1: b2KeyInterface, b2Key methods
b2/backend.go                        (edit)  R1: beKeyInterface, beKey methods
b2/key.go                            (edit)  R1, R3, R4: public accessors
b2/*_test.go fakes                   (edit)  satisfy the widened interfaces
base/key_restrictions_test.go        (new)   R6 base cases
b2/integration_test.go               (edit)  R6 live case, credential-gated
b2/key_restrictions_wire_test.go     (new)   R6 b2 cases (internal/b2wiretest is on main once #205 merges;
                                             if it is not available on this stack, use httptest as base tests do)
.changes/unreleased/Added-*.yaml     (new)   R7
```

## Order of work

1. Failing tests first: the R6 base and b2 tests, against literal responses. Confirm they fail (accessors missing,
   then fields empty).
2. R5 and R2 in `base` (fields plus `keyBucketIDs`).
3. R1 through the backend layers to `b2.Key`, with R3 and R4.
4. Update test fakes; run `go test ./...`.
5. Changelog (R7); `make ci`; a fresh-context review of the diff per `AGENTS.md`, answering each finding.

## Risks

- Interface widening breaks a fake somewhere in `b2`'s tests: fix the fake, never the interface.
- If the v4 `b2_list_keys` response uses `bucketIds` while a v3 `b2_create_key` uses `bucketId` for the same key,
  R2's normalization must yield the same `[]string` for both; a test covers it.

## Proof

| Req | Break | Must fail |
| --- | --- | --- |
| R2 | `keyBucketIDs` ignores `BucketID` (v3) | the v3 create test |
| R2 | `keyBucketIDs` ignores `BucketIDs` (v4) | the v4 create and list tests |
| R3 | return `[]string{}` for an unrestricted key | the unrestricted-key test |
| R2/R3 | stop normalizing an empty `bucketIds: []` to nil | the empty-array test |
| R4 | build the copy with `make` plus `copy` | the empty-array test (nil becomes `[]string{}`) |
| R4 | `BucketIDs()` returns the internal slice | the copy test |
| R5 | `ListKeys` stops setting `Prefix` | the list test |

Also record `go test -race -count=5 -shuffle=on ./base ./b2/...` with the B2 credentials unset, and `make ci`.
Run the live test once with credentials if they are available locally; otherwise say so, and it runs in the live CI
job on main (#214).
