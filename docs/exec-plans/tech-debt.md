# Technical debt

These tests use the `knowndefect` build tag. Run `make known-defects` to check them. The test comments name no linked issues.

## Failing today

| Test | Status at this revision | Tracking |
| --- | --- | --- |
| `TestKnownDefect_RejectedUpdateDoesNotLeakIntoNextUpdate` | A rejected visibility update is sent again by a later unrelated update. | no linked issue |
| `TestKnownDefect_UnrelatedUpdateDoesNotSendCachedDefaultEncryption` | An unrelated update sends cached default encryption. | no linked issue |
| `TestKnownDefect_NotFoundErrorUnwrapsToFSErrNotExist` | A not-found error does not match `fs.ErrNotExist`. | no linked issue |
| `TestKnownDefect_OperateStreamAcceptsCallbackReturningItsInput` | An identity callback returns a context-canceled error. | no linked issue |

## Needs review

| Test | Status at this revision | Tracking |
| --- | --- | --- |
| `TestKnownDefect_NonJSONErrorReplyHasMessage` | The test passes on this revision; its known-defect marker needs review. | no linked issue |
| `TestKnownDefect_ChunkOffsetMultipliedIn64Bit` | The test skips on 64-bit hosts. Run the test on a 32-bit host to verify the marker. | no linked issue |

Sources: [bucket tests](../../b2/known_defect_bucket_test.go), [reader test](../../b2/known_defect_reader_test.go), [base test](../../base/known_defect_test.go), and [consistent test](../../x/consistent/known_defect_test.go).
