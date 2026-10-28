# Spec: pin the b2_create_key request, response and errors
From: intent.md (Felipe Fumero). Issue: #192. Status: ready for review.
Skills applied: none beyond `AGENTS.md`, `REVIEW.md` and `docs/testing.md`.

Source of truth for every expected value: the B2 API documentation for `b2_create_key` (request fields
`accountId`, `capabilities`, `keyName`, `validDurationInSeconds`, `bucketId` in v3, `bucketIds` in v4,
`namePrefix`; response fields `applicationKeyId`, `applicationKey`, `keyName`, `capabilities`,
`expirationTimestamp` in milliseconds). Each test links the page in a comment.

## Requirements

- **R1. Account-wide key.** `(*Client).CreateKey(ctx, "k1", Capabilities("listBuckets", "readFiles"),
  Lifetime(time.Hour))` sends one POST to `/b2api/v3/b2_create_key` whose body has `accountId` equal to
  the authorized account, `keyName` `"k1"`, `capabilities` `["listBuckets","readFiles"]` in that order and
  `validDurationInSeconds` `3600`. It has no `bucketId`, `bucketIds` or `namePrefix`.
- **R2. No lifetime.** Without `Lifetime`, the body has no `validDurationInSeconds`.
- **R3. One bucket, with a prefix.** `(*Client).CreateKey(..., BucketIDs("b-1"), Prefix("logs/"))` sends to
  v3 with `bucketId` `"b-1"` and `namePrefix` `"logs/"`, and no `bucketIds`.
- **R4. Several buckets.** `BucketIDs("b-1", "b-2")` sends to `/b2api/v4/b2_create_key` with `bucketIds`
  `["b-1","b-2"]` in that order, and no `bucketId`. With `Lifetime(time.Hour)` and `Prefix("logs/")` added,
  the same v4 body also has `validDurationInSeconds` `3600` and `namePrefix` `"logs/"`. The v4 request type
  has its own JSON tags, and a dropped prefix there creates a key wider than asked for.
- **R5. Bucket key.** For a `*Bucket` obtained with `(*Client).Bucket` from a `b2_list_buckets` response
  whose `bucketId` is `"bucket-from-list"`, `(*Bucket).CreateKey(ctx, "k2", Capabilities("readFiles"),
  Prefix("p/"))` sends to v3 with `bucketId` `"bucket-from-list"` and `namePrefix` `"p/"`. (Kills
  `b2/key.go:175`.)
- **R6. Response.** With a scripted response of `applicationKeyId` `"key-id"`, `applicationKey` `"secret"`,
  `keyName` `"k1"`, `capabilities` `["readFiles"]` and `expirationTimestamp` `1767225600000`, the returned
  `Key` has `ID() == "key-id"`, `Secret() == "secret"`, `Name() == "k1"`, `Capabilities() == ["readFiles"]`
  and `Expires()` equal to `time.UnixMilli(1767225600000)`.
- **R7. Errors.** When B2 answers `b2_create_key` with status 400 (`{"status":400,"code":"bad_request",...}`)
  or 403 (`{"status":403,"code":"access_denied",...}`), each of `(*Client).CreateKey` without buckets, with one bucket, with two buckets,
  and `(*Bucket).CreateKey` returns a non-nil error and a nil `*Key`. (Kills `base/base.go:2081`,
  `b2/key.go:128,176`.) Both statuses map to `Punt` in `base.Action` (`base/base.go:63-101`), so
  no retry hides the error. 401 is out of scope: blazer re-authorizes and replays on any 401 (#101). 5xx
  is out of scope: retry behavior for non-idempotent calls is #172 and #178.
- **R8. Checks before any request.** `(*Client).CreateKey` with `Prefix` and no bucket, and
  `(*Bucket).CreateKey` with `BucketIDs`, return an error, and no `b2_create_key` request is recorded.
- **R9. Test-only.** No file outside `*_test.go` and `intent/create-key-wire/` changes. Existing tests,
  including `base/base_v4_test.go:301,322`, stay as they are.

## Design

One new file, `b2/create_key_wire_test.go` (package `b2`), using `internal/b2wiretest`:

- A small local helper `newCreateKeyWire(t)` returns the `*b2wiretest.Server` and a `*Client` from
  `NewClient(ctx, "account", "key", APIBase(srv.URL()))`. It scripts `b2_authorize_account` with a v4
  response whose `accountId` is `"acct-1"`, so R1 can compare against a value the test set, not the
  helper's default.
- R1 to R4 are one table-driven test over the options, asserting with `AssertJSON`, `AssertNoJSON` and the
  recorded `Path`.
- R5 scripts `b2_list_buckets` with one bucket whose `bucketId` is `"bucket-from-list"`. That makes the id
  come from the listing, independent of the code under test.
- R6 and R7 script `b2_create_key` with `Reply`; R7 is table-driven over status code and call shape.
- R8 asserts `len(srv.Calls("b2_create_key")) == 0`.

Expected values are literals written from the B2 docs, never derived from blazer's output.

## How each constraint in the intent is met

| Constraint | Met by |
| --- | --- |
| Test-only | R9 |
| Expected values from the B2 docs | Literals in R1–R7, each test links the `b2_create_key` page |
| Credential-free, race-safe | `b2wiretest` loopback server; run under `-race -count=5 -shuffle=on` |
| Built on #191 | Design: only `internal/b2wiretest`, no new fake |
| No new dependency | Test file imports only the standard library and `internal/b2wiretest` |

## Areas of concern

1. **Stacked on #191.** This branch starts from #191's branch. #191 merges first; this PR then targets
   `main`. Owner: Felipe.
2. **Overlap with existing `base` tests** (intent open question 1). They are kept; removal can follow if a
   reviewer prefers. Owner: @goanpeca.
