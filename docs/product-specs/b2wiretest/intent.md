# Intent: a shared recording B2 server for wire tests
Author: Felipe Fumero. Issue: #191. Parent: #97. Status: draft.

## Problem

Tests that exercise blazer against a fake B2 server each build their own server. The `b2` package has
at least eight of them (`newComposeWire`, `newLockWire`, `newSSECPublicWire`, `newSSEB2Wire`,
`newProgressWire`, `newReplicationCreateWire`, `newDownloadByIDWire`, `newAccountInfoWire`), and `base`
tests start their own `httptest` servers too. Each one re-implements authorization, routing and bookkeeping, and most of them
accept any request body.

As a result, several tests pass while blazer sends the wrong request to B2. A mutation and test-claim
review at a5e0d13 recorded concrete cases, each filed as an issue under #97. In each case the production
code was changed and `go test ./...` stayed green:

- `b2/key.go:175`: a bucket key created with an empty bucket id (#192).
- `b2/writer.go:369,417`: retention and legal hold dropped on upload (#193).
- `b2/notification.go:78-118`: the rule signing secret dropped (#194).
- `base/base.go:1534`: a constant upload-part SHA1 (#195).
- `base/base.go:1643,1692` and `b2/iterator.go:254,292,325`: the listing prefix or cursor lost (#196).
- `base/base.go:1192` and `b2/writer.go:355,414`: the SSE-B2 or Content-Type header wrong (#197).

Each of those issues needs a test that asserts what is sent to B2 and what is parsed back. Writing a
new private fake for each of them would repeat the cause.

## Proposed outcome

One small test helper that every new wire test can use. It plays B2 over HTTP, records exactly what
blazer sent on each call (method, path, headers, JSON body), and serves the responses a test scripts.
A test can then say "the `b2_create_key` body had `bucketId` X" and get a "got X, want Y" failure when it
does not.

Success looks like:

- #192 to #198 are written against the helper, and each recorded break listed in those issues makes a
  test fail.
- A contributor adding a new B2 call can write its wire test without building a server from scratch.
- No change to blazer's public API or its runtime behavior.

## Affected users and APIs

- Contributors and reviewers of blazer tests. Not end users: nothing is exported from the module.
- Test code in `base`, `b2` and `b2/sync`. Production code is not touched.

## Constraints

- **No new dependencies.** blazer has none beyond the standard library; `go.mod` and `go.sum` stay as they
  are.
- **Go 1.26.10**, the module's declared minimum (see #190).
- **Not part of the public API.** It must not be importable by users of the module.
- **Credential-free and offline**: no B2 account, no network beyond loopback, safe under
  `go test -race -count=5 -shuffle=on`.
- **Expected values in tests come from the B2 API docs**, not from blazer's own output. The helper
  records and serves; it must not compute expected values for the tests that use it.
- **Existing tests keep working unchanged.** Migrating the existing per-feature fakes is not part of this
  change.
- Reviewable in one small PR, per `AGENTS.md`.

## Open questions

1. Where it lives: a package under `internal/` (shared by `base`, `b2` and `b2/sync`) or a file per
   package. A `_test.go` file cannot be shared across packages. (For the spec.)
2. Whether it should also cover the download and upload URLs that B2 hands out after authorization, or
   only the API calls, with those added by the tests that need them. (For the spec.)
3. Whether the changelog needs an entry for a test-only internal package, or the PR takes the
   `skip-changelog` label. (@goanpeca)
4. Whether `x/b2sim` should be reused instead. It answers a different question (does blazer agree with the
   simulator), and its future is still open, so this intent assumes a separate helper. (@goanpeca)
