# Intent: credential-free tests for calls only covered by live tests
Author: Felipe Fumero. Issue: #203. Parent: #97. Status: ready for review.

## Problem

These calls run only in live tests that skip without credentials, so `go test ./...` and CI never execute
them:

- hide and reveal (`b2/b2.go:881,891`, `base/base.go:1889`);
- download authorization, including `b2ContentDisposition` (`base/base.go:1739`, `b2/backend.go:729`,
  `Bucket.AuthToken` and `Object.AuthURL` at `b2/b2.go:932,939`);
- list and delete keys (`b2/key.go:144`, `base/base.go:2095,2107`);
- cancel large file (`Object.Cancel`, `b2/b2.go:861`), upload after a connection hang, context cancellation
  and deadline, and an unknown certificate authority (`base/integration_test.go:56,432,502,549,592`).

`x/transport` has no test file. Its fault helpers are used by blazer's own tests and by downstream users, and
`go test ./...` reports `[no test files]`.

## Proposed outcome

Each call has a credential-free test of its request and response. `x/transport` has a test per helper. The
live tests stay as they are. Blazer's credential-free statement coverage is measured and recorded.

## Affected users and APIs

Test code only.

## Constraints

- Test-only. If a new test fails on current code, it found a product bug: stop and open a fix issue.
- Expected values from the B2 docs and blazer's doc comments, never from blazer's output.
- Credential-free and safe under `go test -race -count=5 -shuffle=on`.
- `transport.Trigger` has a known data race (#176). A `Trigger` test that fails under `-race` is #176's bug:
  leave it out and say so, do not fix it here.

## Open questions

None.
