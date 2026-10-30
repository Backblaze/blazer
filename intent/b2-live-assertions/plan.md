| Change | Value |
| --- | --- |
| Issue / slice | #204 / b2 core C2 |
| Scope | Test-only; `b2/integration_test.go`, new wire twins and this proof table |
| Baseline | `9b89b88`; clean worktree; `go test ./b2/...` passed with credentials unset |
| Test levels | Wire twins run locally; live tests require B2 credentials in the main-branch live CI job |
| Plan / self-review | Assert observable results and recorded requests; avoid cached deletion checks, destination-derived copy expectations and account-dependent SSE expectations |
| Constraints | No changes to production, `b2/b2_test.go`, `b2/writer.go` or `b2/reader.go`; no GitHub writes |

| Item | Tests | Status / assertion catches |
| --- | --- | --- |
| R1 | `TestResumeWriterWithoutExtantFile` | Done: live-only, compile-checked; catches missing finalization or altered/truncated content after resume without an existing upload |
| R2 | `TestZeroByteObject` | Done: live-only, compile-checked; catches nonzero size metadata for an empty upload; wire twin pending |
| R3 | `TestDeleteWithoutName`; `TestWriteEmpty` | Done: live-only, compile-checked; catches deletion that reports success while leaving the object readable, and nonempty content after an empty write; wire twins pending |
| R4 | `TestListUnfinishedLargeFiles` | Done: live-only, compile-checked; catches wrong names, missing/extra entries and iterator errors; valid part sizes and writer finalization after listing |
| R5 | `TestFileBufferLive` | Done: live-only, compile-checked; catches altered/truncated bytes uploaded through the file buffer |
| R6 | `TestCopyLive` | Done: live-only, compile-checked; catches a corrupt copy even when its own reported SHA1 matches its corrupt content |
| R7 | `TestReauthPreservesOptions` | Live-only, compile-checked: catches a refreshed client that cannot access the original bucket; behavioral wire twin pending |
| R8 | `TestNewBucket`; `TestBucketDefaultEncryption` | Live-only, compile-checked: catches a wrong bucket name or explicit SSE-B2 setting; omitted SSE moves to a deterministic wire twin (pending) |

| Check | Result |
| --- | --- |
| `go vet ./...` | PASS (exit 0); all live assertions compile; live B2 execution NOT RUN |
| `go test ./b2/...` (baseline) | PASS: b2 9.242s; b2/sync 2.759s |

```sh
unset B2_APPLICATION_KEY_ID B2_APPLICATION_KEY B2_ACCOUNT_ID B2_ACCOUNT_KEY
go vet ./...
go test ./b2/...
```
