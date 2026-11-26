---
type: product-spec
title: "Spec: Application key management contract"
description: "Numbered requirements, code citations and pinning tests for: Application key management contract."
status: ready-for-review
owner: ["@goanpeca", "@sophiecarreras", "@ffumero2003", "@david-santacruz-backblaze"]
stale_after: 2027-05-26T00:00:00+00:00
---
# Spec: Application key management contract
From: [intent.md](intent.md) (Felipe Fumero). Status: ready for review.

This standing spec describes `286e90d`. Code citations refer to that revision.
A named test supplies evidence only for its assertions. This document does not claim that tests ran against live B2.

## Requirements

- **R1. Purpose and creation.** An application key grants access to the account or selected buckets. `Client.CreateKey` accepts a name and options. `Bucket.CreateKey` requests a key scoped to that bucket.
  Source: `b2/key.go:24`, `b2/key.go:131`, `b2/key.go:201`. Tests: `TestCreateKeyRequestBody`, `TestBucketCreateKeySendsTheBucketRestriction`.

- **R2. Scope and wire version.** Without `BucketIDs`, `Client.CreateKey` requests an account-wide key using v3. One bucket ID uses v3. Two or more IDs use v4; those keys cannot authorize with v3-only clients.
  Source: `b2/key.go:117`, `b2/key.go:156`, `base/base.go:2446`. Test: `TestCreateKeyRequestBody`.

- **R3. Capabilities and prefix.** `Capabilities` appends the requested capability names. `Prefix` requests a file-name prefix restriction. A nonempty prefix on `Client.CreateKey` needs at least one bucket ID; `Bucket.CreateKey` supplies its bucket ID.
  Source: `b2/key.go:101`, `b2/key.go:108`, `b2/key.go:149`, `b2/key.go:211`. Tests: `TestCreateKeyRequestBody`, `TestBucketCreateKeyForwardsFields`, `TestCreateKeyRejectsBadOptionsWithoutARequest`.

- **R4. Invalid scopes.** An explicitly empty combined `BucketIDs` scope or an empty ID returns `ErrInvalidBucketIDs` before a create-key request. `Bucket.CreateKey` rejects any `BucketIDs` option with `ErrBucketIDsOnBucket` before a create-key request. Callers can match either sentinel with `errors.Is`.
  Source: `b2/key.go:75`, `b2/key.go:141`, `b2/key.go:208`. Tests: `TestCreateKeyRejectsEmptyBucketIDsWithoutARequest`, `TestCreateKeyRejectsBadOptionsWithoutARequest`.

- **R5. Lifetime.** Zero lifetime requests a non-expiring key. Other lifetimes must be at least one second and less than 1,000 days. Longer fractional durations truncate to whole seconds. Invalid lifetimes return an error matching `base.ErrInvalidDuration` with `errors.Is` before the request. `Deadline` computes the duration when called.
  Source: `b2/key.go:83`, `b2/key.go:93`, `base/base.go:1985`. Tests: `TestCreateKeyRequestBody`, `TestCredentialLifetimeValidationWire`.

- **R6. Returned fields.** The created `Key` exposes its ID, name, capabilities, expiration and secret. `Expires` returns zero time when the response has no expiration. `BucketIDs` returns a copy; `Prefix` exposes the restriction.
  Source: `b2/key.go:31`, `b2/key.go:35`, `b2/key.go:48`, `b2/key.go:55`, `base/base.go:2478`. Tests: `TestCreateKeyReturnsTheResponseFields`, `TestKeyExpirationReachesPublicAPI`, `TestKeyRestrictionsWire`.

- **R7. Secret availability.** `Secret` exposes the application key value on the returned newly created key. B2 returns it at creation; `ListKeys` does not populate the secret field. The SDK retains the value on the created `Key`, so repeated `Secret` calls return it; it is not a consume-once accessor. No test pins this. The gap is the listed key's empty secret and repeated access.
  Source: `b2/key.go:55`, `b2/baseline.go:929`, `base/base.go:2478`, `base/base.go:2517`. Test: `TestCreateKeyReturnsTheResponseFields` pins the created key's secret.

- **R8. Listing.** `ListKeys` takes a maximum count and a cursor, initially empty. It returns a page and the next cursor. When the next cursor is empty, it returns `io.EOF`, possibly with the final page. An empty page with a nonempty cursor is not the end. Listed keys expose capabilities and restrictions.
  Source: `b2/key.go:175`, `b2/key.go:194`, `base/base.go:2517`. Tests: `TestClientListKeysAndDeleteWire`, `TestRegression_ListKeysContinuesPastEmptyPageWithCursor`, `TestListKeysReturnsCapabilities`, `TestKeyRestrictionsWire`.

- **R9. Deletion.** `Key.Delete` sends `b2_delete_key` with the key ID and returns the request error, if any. Matching deletion errors with `errors.Is` or `errors.As` is not specified in code or godoc.
  Source: `b2/key.go:52`, `b2/baseline.go:923`, `base/base.go:2490`. Test: `TestClientListKeysAndDeleteWire`.

- **R10. Errors.** A nonempty prefix without a bucket returns a local error. Matching that error with `errors.Is` or `errors.As` is not specified in code or godoc. A B2 create failure returns a nil key and the request error; `base.Code` and `base.MsgCode` expose the status and B2 code. Matching B2 create or listing errors with `errors.Is` or `errors.As` is not specified in code or godoc.
  Source: `b2/key.go:149`, `b2/key.go:166`, `base/base.go:2475`. Tests: `TestCreateKeyRejectsBadOptionsWithoutARequest`, `TestCreateKeyReturnsB2Errors`.

- **R11. Command-line creation.** `b2keys create` requires `B2_APPLICATION_KEY_ID` and `B2_APPLICATION_KEY`. If either is empty, the tool uses `B2_ACCOUNT_ID` or `B2_SECRET_KEY`, respectively, as a legacy fallback. If either credential is still missing, the tool returns a usage error before creating a key. The tool takes a key name and at least one capability as positional arguments. Fewer than two positional arguments return a usage error; it does not check that they are nonempty. It accepts optional bucket, duration and prefix flags. The prefix flag requires the bucket flag: without it, the tool authorizes first, then `Client.CreateKey` returns a local error and the tool exits with a failure status, not a usage error. The duration defaults to zero. A negative duration returns a usage error before any request. On success the command prints the new ID and secret.
  Source: `bin/b2keys/b2keys.go:16`, `bin/b2keys/b2keys.go:37`, `bin/b2keys/b2keys.go:41`, `bin/b2keys/b2keys.go:47`, `bin/b2keys/b2keys.go:57`, `bin/b2keys/b2keys.go:62`, `bin/b2keys/b2keys.go:70`, `bin/b2keys/b2keys.go:81`, `bin/b2keys/b2keys.go:94`, `bin/b2keys/b2keys.go:103`, `b2/key.go:149`. Tests: `TestRegression_MissingCredentialsMessageEndsWithNewline` checks both credentials missing; `TestCreateRejectsNegativeDurationBeforeRequest` and `TestCreateDurationOnWire` cover duration. No test pins this: either credential missing individually, legacy fallbacks, fewer than two positional arguments, a prefix without a bucket, or output contents. Search: `rg -n 'B2_APPLICATION_KEY_ID|B2_APPLICATION_KEY|B2_ACCOUNT_ID|B2_SECRET_KEY|ExitUsageError|runCreate|fs.Parse|Execute\(' bin/b2keys --glob '*_test.go'`.

## Design

The public `b2` API delegates to the `base` B2 wire layer. The `b2keys` command uses the public API.

## How each constraint is met

| Constraint | Met by |
| --- | --- |
| Current behavior | Code citations identify the base revision and implementation. |
| Evidence and gaps | Requirements name tests and identify unpinned behavior. |
| Separate change records | The intent links to the existing change specs. |

## Areas of concern

1. The listed-secret and repeated-access behavior and the CLI output lack the test pins identified in R7 and R11. Owner: @ffumero2003.
2. The local prefix error has no specified matching contract. Owner: @ffumero2003.
