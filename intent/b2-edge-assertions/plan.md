| Item | Mutation | Failing output | Passing after revert |
| --- | --- | --- | --- |
| 1 · session secret | Already covered by `TestAccountInfoFileResumeAndClearOn401`; removed the vacuous check from `TestAccountInfoFileRoundTripAndPermissions` | — | — |
| 2 · retention cache | `baseline.go` `err == nil` → `err != nil` | `post-update marker="before" calls=2 (want 3)` | `TestObjectLockUpdateInvalidatesCachedInfo/retention`: PASS |
| 2 · retention cache | Delete the invalidation guard and body | `post-update marker="before" calls=2 (want 3)` | `TestObjectLockUpdateInvalidatesCachedInfo/retention`: PASS |
| 2 · legal-hold cache | `baseline.go` `err == nil` → `err != nil` | `post-update marker="before" calls=2 (want 3)` | `TestObjectLockUpdateInvalidatesCachedInfo/legal_hold`: PASS |
| 2 · legal-hold cache | Delete the invalidation guard and body | `post-update marker="before" calls=2 (want 3)` | `TestObjectLockUpdateInvalidatesCachedInfo/legal_hold`: PASS |
| 3 · write cap | `accountinfo.go` `>` → `>=` | `Set 1048576-byte session: account info: session file exceeds 1048576 bytes` | `TestAccountInfoWriteSizeBoundary`: PASS |
| 3 · write cap | Delete the size guard | `Set 1048577-byte session error = <nil>, want size rejection` | `TestAccountInfoWriteSizeBoundary`: PASS |
| 4 · read cap | `accountinfo.go` `>` → `>=` | `AuthData() for 1048576-byte session: ok=false ... want loaded` | `TestAccountInfoReadSizeBoundary`: PASS |
| 4 · read cap | Delete the size guard | `AuthData() for 1048577-byte session: ok=true err=<nil>, want size rejection` | `TestAccountInfoReadSizeBoundary`: PASS |
| 5 · temp cleanup | Stopped: needs a hook after `CreateTemp` and before deferred `os.Remove` to make cleanup fail deterministically; pre-existing read-only permissions fail at `CreateTemp` instead | Not run | Not run |
| 6 · file sync | Stopped: needs an injectable temporary-file handle or `Sync` hook to observe the call | Not run | Not run |
| 7 · folder delete | Negate the regular-file/hide/copy status guard | `Delete(folder) error = <nil>, want non-regular-file rejection` | `TestObjectDeleteRejectsFolderStatus`: PASS |
| 7 · folder delete | Delete the status guard | `Delete(folder) error = <nil>, want non-regular-file rejection` | `TestObjectDeleteRejectsFolderStatus`: PASS |
| 8 · buffer close | `mb.buf == nil` → `mb.buf != nil` | `Close retained the buffer` | `TestMemoryBufferCloseIsIdempotent`: PASS |
| 8 · buffer close | Delete the nil guard | `panic: runtime error: invalid memory address or nil pointer dereference` at second `Close` | `TestMemoryBufferCloseIsIdempotent`: PASS |
| 8 · pool return | Stopped: `sync.Pool.Get` may discard a just-put value, including under `-race`; proving `Put` needs an injectable pool interface or hook | Not run | Not run |
| 9 · zero wake | `n > 0` → `n <= 0` | `progress callbacks = [0 0], want one nonzero update and one terminal call` | `TestProgressZeroWakeDoesNotDuplicateCallback`: PASS |
| 9 · zero wake | Delete the `n > 0` guard | `progress callbacks = [1 1 1], want one nonzero update and one terminal call` | `TestProgressZeroWakeDoesNotDuplicateCallback`: PASS |

```sh
unset B2_APPLICATION_KEY_ID B2_APPLICATION_KEY B2_ACCOUNT_ID B2_ACCOUNT_KEY
go test -count=1 ./b2 -run 'TestObjectLockUpdateInvalidatesCachedInfo/(retention|legal_hold)$'
go test -count=1 ./b2 -run '^TestAccountInfo(Write|Read)SizeBoundary$'
go test -count=1 ./b2 -run '^TestObjectDeleteRejectsFolderStatus$'
go test -count=1 ./b2 -run '^TestMemoryBufferCloseIsIdempotent$'
go test -count=1 ./b2 -run '^TestProgressZeroWakeDoesNotDuplicateCallback$'
go test -race -count=5 -shuffle=on ./b2/...
go test -race -count=30 ./b2 -run 'Test(MemoryBufferCloseIsIdempotent|ProgressZeroWakeDoesNotDuplicateCallback)$'
go vet ./...
GOOS=windows go vet ./b2/...
make ci
```
