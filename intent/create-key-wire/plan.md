# Plan: pin the b2_create_key request, response and errors (from intent.md and spec.md)

Branch: `test-issue-192-create-key-wire`, stacked on `test-issue-191-shared-recording-server`. One PR for
issue #192 carries this folder and the tests. Apply the lean-code ladder from `AGENTS.md`: reuse
`internal/b2wiretest`, add only what the tests need.

## Files that change

```
b2/create_key_wire_test.go            (new)  R1-R8
intent/create-key-wire/plan.md        (edit only if the implementation departs from this plan)
```

No production file changes (R9).

## Order of work

0. **Wait for #191's code.** `internal/b2wiretest` must exist on this branch. If it is not here yet, merge
   the #191 branch in (`git merge origin/test-issue-191-shared-recording-server`; never rebase). Read its
   `spec.md` for the API: `New`, `URL`, `Reply`, `Handle`, `Calls`, `Last`, `AssertJSON`, `AssertNoJSON`.
1. **Fixture** (all R). `newCreateKeyWire(t)`: start `b2wiretest.New(t)`, script `b2_authorize_account`
   with a v4 body whose `accountId` is `"acct-1"` and whose URLs are `srv.URL()` (copy the shape of
   `updateAuthJSON` in `b2/update_cors_name_test.go`), and build the client with `APIBase(srv.URL())`.
   Script `b2_create_key` with a minimal valid key by default.
2. **Request shapes** (R1–R4). One table test, `TestCreateKeyRequestBody`, with one row per call shape:
   - account-wide with `Lifetime`;
   - account-wide without `Lifetime`;
   - one bucket with a prefix;
   - two buckets.

   Each row names the expected path (`/b2api/v3/...` or `/b2api/v4/...`), the fields that must be present
   with their literal values, and the fields that must be absent.
3. **Bucket key** (R5). `TestBucketCreateKeySendsTheBucketID`: script `b2_list_buckets` with one bucket,
   `bucketId` `"bucket-from-list"`, then `client.Bucket(ctx, name)` and `bucket.CreateKey(...)`. Assert
   `bucketId` and `namePrefix`.
4. **Response** (R6). `TestCreateKeyReturnsTheResponseFields`: script the response from the spec; assert the
   five accessors against the same literals.
5. **Errors** (R7). `TestCreateKeyReturnsB2Errors`: table over {400, 403} × {account, one bucket, two
   buckets, bucket key}; each returns an error and a nil key.
6. **Checks before the request** (R8). `TestCreateKeyRejectsBadOptionsWithoutARequest`: the two invalid
   combinations return an error, and `srv.Calls("b2_create_key")` is empty.
7. **Check and self-review.** `make ci`; then a fresh-context review of the diff per `AGENTS.md`, with each
   finding answered.

Steps 2 to 6 are independent once step 1 exists.

## Risks

- **A new test fails on current code.** Then it found a real bug. Stop, do not change production code here,
  and report it so a separate fix issue can be opened (spec R9).
- **Retries hide errors.** blazer retries or re-authorizes on some statuses. Use 400 and 403 only (spec
  R7); `base.Action` maps both to `Punt`. Any 401 triggers re-authorization and a replay (#101).
- **Changelog.** Test-only diff: `make changelog-check` passes without an entry because no non-test `.go`
  file changes.

## Proof

Each break from `recorded-breaks` for task T2 must make a test fail. Apply it, run
`go test -count=1 ./b2 -run 'CreateKey'`, record the failing output, revert, record the passing output.

| Req | Break to apply | Test that must fail |
| --- | --- | --- |
| R5 | `b2/key.go:175`: pass `""` instead of `b.b.id()` | `TestBucketCreateKeySendsTheBucketID` |
| R7 | `base/base.go:2081`: `err != nil` to `err == nil` | `TestCreateKeyReturnsB2Errors` |
| R7 | `b2/key.go:128` and `:176`: drop the `if err != nil` return | `TestCreateKeyReturnsB2Errors` |
| R1–R4 | `internal/b2types/b2types.go:443-448`: rename `keyName`, `capabilities`, `namePrefix` or `validDurationInSeconds` (one at a time) | `TestCreateKeyRequestBody` |
| R4 | `internal/b2types/b2types.go:457`: rename `bucketIds` | `TestCreateKeyRequestBody` |
| R4 | `internal/b2types/b2types.go:456`: rename the v4 `validDurationInSeconds` tag | `TestCreateKeyRequestBody` (v4 lifetime and prefix row) |
| R4 | `internal/b2types/b2types.go:458`: rename the v4 `namePrefix` tag | `TestCreateKeyRequestBody` (v4 lifetime and prefix row) |

Also record `go test -race -count=5 -shuffle=on ./b2` passing with `B2_APPLICATION_KEY_ID`,
`B2_APPLICATION_KEY`, `B2_ACCOUNT_ID` and `B2_ACCOUNT_KEY` unset, and `make ci` passing.
