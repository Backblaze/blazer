# Plan: cover the remaining base request and response mapping edges (from intent.md and spec.md)

Branch: `test-issue-202-base-mapping-edges`, from `main`. One PR for #202 carries this folder and the tests.
Apply the lean-code ladder from `AGENTS.md`: reuse the `httptest` patterns in `base/*_test.go`.

## Files that change

```
base/mapping_edges_test.go   (new)  R1, R2, R3
```

No production file changes (R4).

## Order of work

1. R2 `TestMillitime` (pure function).
2. R3 download test, modeled on `base/download_gzip_test.go`.
3. R1 user-agent test: a server that answers `b2_authorize_account` with the minimal JSON other base tests use,
   recording the request header.
4. `make ci`, then a fresh-context review of the diff per `AGENTS.md`, answering each finding.

## Risks

A new test fails on current code: product bug; stop and report.

## Proof

| Req | Break | Test that must fail |
| --- | --- | --- |
| R1 | `base/base.go:401`: `getUserAgent` returns `DefaultUserAgent` only | `TestUserAgentOptionReachesTheWire` |
| R2 | `base/base.go:368`: `t%1000*1e6` → `t%100*1e6` | `TestMillitime` |
| R3 | `base/base.go:1875`: replace the fallback condition with `false` | `TestDownloadUsesLargeFileSHA1WhenContentSHA1IsNone` |

Also record `go test -race -count=5 -shuffle=on ./base` passing with the B2 credentials unset, and `make ci`.
