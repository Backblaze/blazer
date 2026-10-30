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
| R2 | `TestZeroByteObject`; `TestZeroByteObjectWire` | Done: live-only, compile-checked; catches nonzero size metadata for an empty upload; cheap offline twin M1 |
| R3 | `TestDeleteWithoutName`; `TestWriteEmpty`; `TestDeleteWithoutNameWire`; `TestZeroByteObjectWire` | Done: live-only, compile-checked; catches deletion that reports success while leaving the object readable, and nonempty content after an empty write; cheap offline twins M2/M1 |
| R4 | `TestListUnfinishedLargeFiles` | Done: live-only, compile-checked; catches wrong names, missing/extra entries and iterator errors; valid part sizes and writer finalization after listing |
| R5 | `TestFileBufferLive` | Done: live-only, compile-checked; catches altered/truncated bytes uploaded through the file buffer |
| R6 | `TestCopyLive` | Done: live-only, compile-checked; catches a corrupt copy even when its own reported SHA1 matches its corrupt content |
| R7 | `TestReauthPreservesOptions`; `TestReauthPreservesOptionsWire` | Done: live-only, compile-checked; catches a refreshed client that cannot access the original bucket; M3/M4 pin user agents, test modes, transport, endpoint, refreshed token and account on real HTTP requests |
| R8 | `TestNewBucket`; `TestBucketDefaultEncryption`; `TestNewBucketAttributesWire`; `TestNewBucketDefaultEncryptionWire` | Done: live-only, compile-checked; catches a wrong bucket name or explicit SSE-B2 setting; M5/M6/M7 pin create fields, omitted/explicit SSE and decoded service settings without an account default assumption |

| Break | Twin / production behavior | Temporary mutation | Failing output (exit 1) | Restored output (exit 0) |
| --- | --- | --- | --- | --- |
| M1 (R2/R3) | `TestZeroByteObjectWire`; `b2/b2.go:attrsFromFileInfo` | Map `Size` to `size + 1` | `empty object size = 1, want 0` (both chunk-size cases); `FAIL .../b2 0.724s` | `ok .../b2 0.586s` |
| M2 (R3) | `TestDeleteWithoutNameWire`; `b2/b2.go:Object.Delete` | Return nil instead of calling `deleteFileVersion` | `Attrs after Delete = <nil>, want not-exist`; `delete calls = 0, want 1`; `FAIL .../b2 0.707s` | `ok .../b2 0.438s` |
| M3 (R7) | `TestReauthPreservesOptionsWire`; `b2/backend.go:beRoot.reauthorizeAccount` | Clear `userAgents` in the options passed to reauthorization | Requests 2/3: `User-Agent = "blazer/0.9.0", want both custom agents before blazer/`; `FAIL .../b2 0.467s` | `ok .../b2 0.688s` |
| M4 (R7) | `TestReauthPreservesOptionsWire`; `b2/backend.go:beRoot.reauthorizeAccount` | Clear `transport` in the options passed to reauthorization | `configured transport calls = 2, want 4`; `FAIL .../b2 0.676s` | `ok .../b2 0.285s` |
| M5 (R8) | `TestNewBucketAttributesWire`; `b2/b2.go:Client.NewBucket` | Pass nil lifecycle rules to `createBucket` | `b2_create_bucket body lifecycleRules: got null` instead of the two literal rules; `FAIL .../b2 0.312s` | `ok .../b2 0.276s` |
| M6 (R8) | `TestNewBucketDefaultEncryptionWire`; `base/base.go:B2.CreateBucket` | Always send SSE-B2/AES256, even when omitted | `defaultServerSideEncryption: got {"algorithm":"AES256","mode":"SSE-B2"}, want missing` (both omitted cases); `FAIL .../b2 0.670s` | `ok .../b2 0.272s` |
| M7 (R8) | `TestNewBucketDefaultEncryptionWire`; `b2/baseline.go:b2Bucket.attrs` | Decode the returned SSE mode as `broken` | `default encryption = &{Mode:broken Algorithm:AES256}, want &{Mode:SSE-B2 Algorithm:AES256}`; `FAIL .../b2 0.715s` | `ok .../b2 0.273s` |

| Check | Result |
| --- | --- |
| `go vet ./...` (final) | PASS (exit 0, no output); all live assertions compile; live B2 execution NOT RUN |
| `go test ./b2/...` (baseline) | PASS: b2 9.242s; b2/sync 2.759s |
| Object twins with `-race -count=1` | PASS: b2 1.781s |
| `go test -race -count=5 -shuffle=on ./b2/...` (final, after cleanup lint fix) | PASS (exit 0): `ok github.com/Backblaze/blazer/b2 63.896s`; `ok github.com/Backblaze/blazer/b2/sync 17.194s` |
| Scoped live-test invocation with credentials unset | All ten scoped tests SKIP; live-only, compile-checked; `ok .../b2 0.266s` is not evidence of live B2 execution |
| `make ci` (first attempt) | FAIL at lint: unchecked deferred `reader.Close()` in the empty-object twin; fixed by checking the deferred close error |
| `make ci` (final) | PASS (exit 0): lint `0 issues.`; b2 race tests 13.665s; b2/sync 4.337s; base 6.811s; module/build/format checks, remaining package tests, b2keys, all five cross-compiles, changelog-check and all five script checks passed |
| Scope audit | All temporary production mutations restored; protected sibling files unchanged; no B2 credentials used |
| SSE contract | [b2_create_bucket](https://www.backblaze.com/apidocs/b2-create-bucket): omit the request field to leave encryption to the service; twin fixtures include the documented SSE-B2/AES256 response and legacy null metadata |

```sh
unset B2_APPLICATION_KEY_ID B2_APPLICATION_KEY B2_ACCOUNT_ID B2_ACCOUNT_KEY
go vet ./...
go test ./b2/...
go test ./b2 -run '^TestZeroByteObjectWire$' -count=1 -timeout=30s
go test ./b2 -run '^TestDeleteWithoutNameWire$' -count=1 -timeout=30s
go test ./b2 -run '^TestReauthPreservesOptionsWire$' -count=1 -timeout=30s
go test ./b2 -run '^TestNewBucketAttributesWire$' -count=1 -timeout=30s
go test ./b2 -run '^TestNewBucketDefaultEncryptionWire$' -count=1 -timeout=30s
go test -race -count=5 -shuffle=on ./b2/...
make ci
```
