# Technical debt

`make known-defects` runs tagged Go tests and documentation checks. Each failing item links its issue.

## Failing today

| Test | Status at this revision | Tracking |
| --- | --- | --- |
| `TestKnownDefect_RejectedUpdateDoesNotLeakIntoNextUpdate` | A rejected visibility update is sent again by a later unrelated update. | [#532](https://github.com/Backblaze/blazer/issues/532) |
| `TestKnownDefect_UnrelatedUpdateDoesNotSendCachedDefaultEncryption` | An unrelated update sends cached default encryption. | [#533](https://github.com/Backblaze/blazer/issues/533) |
| `TestKnownDefect_NotFoundErrorUnwrapsToFSErrNotExist` | A not-found error does not match `fs.ErrNotExist`. | [#534](https://github.com/Backblaze/blazer/issues/534) |
| `TestKnownDefect_OperateStreamAcceptsCallbackReturningItsInput` | An identity callback returns a context-canceled error. | [#535](https://github.com/Backblaze/blazer/issues/535) |

## Failing documentation checks

| Check | Script message | Tracking |
| --- | --- | --- |
| `changelog-base-functions` | CHANGELOG Added lists the new public base functions; missing from the 0.10.0 Added list: `NewAuthorizedAccount` | [#536](https://github.com/Backblaze/blazer/issues/536) |
| `example-exit-status` | example program exits non-zero on failure and matches the key tool's variables; legacy secret variable differs: example 'B2_ACCOUNT_KEY' vs b2keys 'B2_SECRET_KEY' | [#537](https://github.com/Backblaze/blazer/issues/537) |

## Needs review

| Test | Status at this revision | Tracking |
| --- | --- | --- |
| `TestKnownDefect_NonJSONErrorReplyHasMessage` | The test passes on this revision; its known-defect marker needs review. | no linked issue |
| `TestKnownDefect_ChunkOffsetMultipliedIn64Bit` | The test skips on 64-bit hosts. Run `make known-defects-32bit` to verify the marker. | no linked issue |
| `readme-examples` | The check passes at this revision; its known-defect marker needs review. | no linked issue |
| `upgrade-sync-before` | The check passes at this revision; its known-defect marker needs review. | no linked issue |
| `upgrade-breaking-claim` | The check passes at this revision; its known-defect marker needs review. | no linked issue |

Sources: [bucket tests](../../b2/known_defect_bucket_test.go), [reader test](../../b2/known_defect_reader_test.go), [base test](../../base/known_defect_test.go), and [consistent test](../../x/consistent/known_defect_test.go).
