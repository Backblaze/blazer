# Intent: pin the b2_create_key request, response and errors
Author: Felipe Fumero. Issue: #192. Parent: #97. Depends on: #191. Status: ready for review.

## Problem

No credential-free test checks what `CreateKey` sends to B2 or what it does with the answer. Each of these
changes leaves `go test ./...` green at a5e0d13:

- `b2/key.go:175`: `(*Bucket).CreateKey` passes `""` instead of the bucket id, so a key meant for one bucket
  is created for the whole account.
- `base/base.go:2081`: `err != nil` becomes `err == nil` in `doCreateKeyWithState`, so a failed call
  returns an empty key and no error.
- `internal/b2types/b2types.go:443-448`: a renamed JSON tag (`keyName`, `capabilities`, `namePrefix`,
  `validDurationInSeconds`) drops that field from the request.
- `b2/key.go:128,176`: the error from the backend is ignored and a key is returned.

What exists today:

- `TestCreateKeyDispatch` (`b2/b2_test.go:1910`) checks that `(*Bucket).CreateKey` routes to `createKey`,
  but its fake bucket id is `""`, so the id is never compared.
- `TestCreateKeyUsesV3Endpoint` and `TestCreateKeyMultiBucketUsesV4Endpoint` (`base/base_v4_test.go:301,322`)
  check the path, the method and `bucketId`/`bucketIds` only, and drop the returned key.
- `TestCreateDeleteKey` (`b2/integration_test.go:1265`) is a live test that skips without credentials and
  checks only that no error came back.

Keys control access to user data. A key that is wider than asked for, or a failure reported as success,
is a security problem, and today CI would not notice either.

## Proposed outcome

Credential-free tests, through the public `Client` and `Bucket` API, that fail on each change listed
above:

- what `b2_create_key` receives for an account-wide key, a single-bucket key, a single-bucket key with a
  prefix, and a multi-bucket key;
- what the returned `Key` exposes, taken from the response;
- that a B2 error comes back as an error with no key;
- that the existing argument checks reject a bad combination before any request is sent.

## Affected users and APIs

- `(*Client).CreateKey`, `(*Bucket).CreateKey` and the `KeyOption`s `Capabilities`, `Lifetime`, `Prefix`
  and `BucketIDs`, as observed on the wire. Their behavior does not change.
- Test code only.

## Constraints

- **Test-only.** No production code changes. If a new test fails on current code, that is a product bug:
  stop and open a separate fix issue.
- Expected values come from the B2 `b2_create_key` documentation, never from blazer's output.
- Credential-free, offline, and safe under `go test -race -count=5 -shuffle=on`.
- Built on the shared recording server from #191 (`internal/b2wiretest`); no new private fake.
- No new dependency; Go 1.26.10.

## Open questions

1. Whether the older `base` tests at `base/base_v4_test.go:301,322` should be removed once the new tests
   cover the same paths more strictly, or kept. This intent keeps them. Owner: @goanpeca.
2. `Deadline` turns a time into a lifetime using the current clock, so its exact `validDurationInSeconds`
   is not stable. This intent covers `Lifetime` and leaves `Deadline` out. Owner: @ffumero2003.
