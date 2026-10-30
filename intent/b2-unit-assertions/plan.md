This file is slice C1's proof record, standing in for a spec and plan. It follows #204's checklist rather than R-numbered requirements.

Item 1 moved to #245: the stronger assertion fails on main because of a product bug.

| Item | Status | Test / proof rows |
| --- | --- | --- |
| 1. Writer error identity | Moved to #245 | — |
| 2. Upload attempt cap | Done: one worker, five reuploads, six attempts | `TestWriterLimitNewUploadAttempts`; 2a–2c |
| 3. Read after double Close | Done: close before EOF, then explicitly require zero bytes and an error | `TestReaderDoubleClose`; 3 |
| 4. Fake-echo bucket tests | Done: CORS/file-lock create and per-bucket reads use the recording server; redundant replication and per-bucket fake tests removed | `TestNewBucketCORSAndFileLock`; 4a–4d; existing `TestNewBucketReplicationConfigurationWireRoundTrip`; 4e |
| 5. Delete bypass fake | Done: redundant fake test and helper removed; wire twin unchanged (also avoids overlap with #240) | Already covered by `TestObjectDeleteBypassGovernanceReachesTheWire`; 5a–5b |
| 6. Bucket ID | Done: literal IDs from create and list responses; ID lookup makes no request | `TestBucketID`; 6 |
| 7. Invalid bucket encryption | Done: exact validation errors and no create/update request for rejected settings; existing-bucket and valid cases retained | `TestBucketDefaultEncryptionRejected`; 7a–7d |
| 8. Backoff durations | Done: existing wait hook captures durations; literal Retry-After, jitter bounds, and cap bounds | `TestBackoff`, `TestBackoffWithoutRetryAfter`, `TestBackoffWithMaxRetriesReached`; 8a–8i |

| Proof environment | Value |
| --- | --- |
| Baseline | `9b89b88` (`origin/main`); no branch changes |
| Mutation isolation | Archive of HEAD plus the changed test file; each break applied alone, target test run, original file restored, target rerun; production files in the worktree never edited |
| Expectations | Literals only; no call to the implementation's backoff function to derive bounds |
| Live / simulator | NOT RUN: test-only assertions and fixtures; no SDK or harness production behavior changed |

| Item | Break | Failing output | Passing after revert |
| --- | --- | --- | --- |
| 2a | Reupload limit `+1` → `+2` | `uploadPart attempts = 7, want 6 (initial attempt plus five reuploads)` | `ok github.com/Backblaze/blazer/b2 0.764s` |
| 2b | Initial-attempt guard flipped | `uploadPart attempts = 1, want 6 (initial attempt plus five reuploads)` | `ok github.com/Backblaze/blazer/b2 0.284s` |
| 2c | Initial-attempt guard deleted | `uploadPart attempts = 1, want 6 (initial attempt plus five reuploads)` | `ok github.com/Backblaze/blazer/b2 0.287s` |
| 3 | Delete `Reader.Close` cancellation | `panic: test timed out after 15s`, `TestReaderDoubleClose`, blocked in `Reader.Close` / `WaitGroup.Wait` | `ok github.com/Backblaze/blazer/b2 0.270s` |
| 4a | Omit CORS in create mapping | `b2_create_bucket body corsRules: missing, want [{"allowedHeaders":["authorization"],"allowedOperations":["b2_download_file_by_name"],"allowedOrigins":["https://example.com"],"corsRuleName":"browser","exposeHeaders":["x-bz-file-name"],"maxAgeSeconds":60}]` | `ok github.com/Backblaze/blazer/b2 0.292s` |
| 4b | Omit file lock in create mapping | `b2_create_bucket body fileLockEnabled: missing, want true` | `ok github.com/Backblaze/blazer/b2 0.271s` |
| 4c | Bucket-name guard flipped | `Bucket.Attrs().FileLockEnabled = false, want true` | `ok github.com/Backblaze/blazer/b2 0.269s` |
| 4d | Bucket-name guard deleted (first bucket returned unconditionally) | `plain bucket attrs = … FileLockEnabled:true …, want no CORS rules and no file lock` | `ok github.com/Backblaze/blazer/b2 0.284s` |
| 4e | Omit replication in create mapping | `create omitted replicationConfiguration` | `ok github.com/Backblaze/blazer/b2 0.276s` |
| 5a | Bypass option assignment flipped to false | `Delete(BypassGovernance) sent bypassGovernance = <nil>, want true` | `ok github.com/Backblaze/blazer/b2 0.275s` |
| 5b | Bypass option assignment deleted | `Delete(BypassGovernance) sent bypassGovernance = <nil>, want true` | `ok github.com/Backblaze/blazer/b2 0.288s` |
| 6 | Read bucket name instead of response ID | `created Bucket.ID() = "n", want bid` | `ok github.com/Backblaze/blazer/b2 0.284s` |
| 7a | NewBucket encryption guard flipped | `invalid default sent 1 create requests, want 0` | `ok github.com/Backblaze/blazer/b2 0.310s` |
| 7b | NewBucket encryption guard deleted | `invalid default sent 1 create requests, want 0` | `ok github.com/Backblaze/blazer/b2 0.462s` |
| 7c | Update encryption guard flipped | `invalid default sent 1 update requests, want 0` | `ok github.com/Backblaze/blazer/b2 0.307s` |
| 7d | Update encryption guard deleted | `invalid default sent 1 update requests, want 0` | `ok github.com/Backblaze/blazer/b2 0.293s` |
| 8a | Force backend delay to 1ns; `TestBackoff` | `wait 1 = 1ns, want [1s, 1s]`; `wait 2 = 1ns, want [2s, 2s]`; `wait 1 = 1ns, want [1.01s, 1.03s]` | `ok github.com/Backblaze/blazer/b2 0.323s` |
| 8b | Force backend delay to 1ns; `TestBackoffWithoutRetryAfter` | `wait 1 = 1ns, want [1.01s, 1.03s]`; `wait 7 = 1ns, want [30s, 32s]` | `ok github.com/Backblaze/blazer/b2 0.278s` |
| 8c | Force backend delay to 1ns; `TestBackoffWithMaxRetriesReached` | `wait 1 = 1ns, want [1.01s, 1.03s]`; `wait 2 = 1ns, want [2.04s, 2.122s]` | `ok github.com/Backblaze/blazer/b2 0.294s` |
| 8d | First-delay guard flipped | `wait 1 = 0s, want [1.01s, 1.03s]` | `ok github.com/Backblaze/blazer/b2 0.568s` |
| 8e | First-delay guard deleted | `wait 1 = 0s, want [1.01s, 1.03s]` | `ok github.com/Backblaze/blazer/b2 0.273s` |
| 8f | Retry-After guard flipped | `wait 1 = 1.011091425s, want [1s, 1s]` | `ok github.com/Backblaze/blazer/b2 0.450s` |
| 8g | Retry-After guard deleted | `wait 1 = 1.02398841s, want [1s, 1s]` | `ok github.com/Backblaze/blazer/b2 0.280s` |
| 8h | 30-second cap guard flipped | `wait 1 = 30.010863997s, want [1.01s, 1.03s]` | `ok github.com/Backblaze/blazer/b2 0.268s` |
| 8i | 30-second cap guard deleted | `wait 7 = 1m11.547314088s, want [30s, 32s]`; `wait 8 = 2m26.982487425s, want [30s, 32s]` | `ok github.com/Backblaze/blazer/b2 0.269s` |

```sh
unset B2_APPLICATION_KEY_ID B2_APPLICATION_KEY B2_ACCOUNT_ID B2_ACCOUNT_KEY
go test ./b2 -run '^TEST_NAME$' -count=1 -timeout=15s
go test ./b2 -run '^(TestWriterLimitNewUploadAttempts|TestReaderDoubleClose|TestBackoff.*|TestBucketDefaultEncryptionRejected|TestNewBucketCORSAndFileLock|TestNewBucketReplicationConfigurationWireRoundTrip|TestObjectDeleteBypassGovernanceReachesTheWire|TestBucketID)$' -count=1
go test -race -count=5 -shuffle=on ./b2/...
go test -race -count=30 ./b2 -run '^(TestWriterLimitNewUploadAttempts|TestReaderDoubleClose|TestBackoff|TestBackoffWithoutRetryAfter|TestBackoffWithMaxRetriesReached)$'
go vet ./...
make ci
make b2keys crosscompile changelog-check scripts-test
```

| Check | Actual output / result |
| --- | --- |
| C1 targeted run before item 1 moved to #245 | `ok github.com/Backblaze/blazer/b2 0.678s` |
| Changed concurrency/timing tests, 30 race-enabled repetitions (item 1 excluded) | `ok github.com/Backblaze/blazer/b2 1.377s` |
| Full b2 race suite, count 5, shuffled | PASS: `ok github.com/Backblaze/blazer/b2 63.318s`; `ok github.com/Backblaze/blazer/b2/sync 15.414s` |
| Concurrency/timing tests, count 30 | `ok github.com/Backblaze/blazer/b2 1.377s` |
| `go vet ./...` | Exit 0, no output |
| `make ci` at committed head | PASS: build, vet, format, tidy, verify (`all modules verified`), lint (`0 issues.`), race tests, b2keys, five cross-builds, changelog-check and scripts-test all passed; `b2` completed in `14.099s` |
| Remaining CI targets run separately | Exit 0: b2keys (`[no test files]`), Linux amd64/386/arm, Darwin arm64 and Windows amd64 cross-builds; changelog-check passes; five shell script cases report `ok` |
