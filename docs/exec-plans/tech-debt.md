---
type: exec-plan
title: "Technical debt"
description: "make known-defects runs tagged Go tests and documentation checks."
status: active
owner: ["@goanpeca", "@sophiecarreras", "@ffumero2003", "@david-santacruz-backblaze"]
stale_after: 2027-05-26T00:00:00+00:00
---
# Technical debt

`make known-defects` runs tagged Go tests and documentation checks. Each listed item links its issue.

## Failing today

No tagged test fails now.

## Failing documentation checks

| Check | Script message | Tracking |
| --- | --- | --- |
| `changelog-base-functions` | CHANGELOG Added lists the new public base functions; missing from the 0.10.0 Added list: `NewAuthorizedAccount` | [#536](https://github.com/Backblaze/blazer/issues/536) |

## Needs review

| Test | Status at this revision | Tracking |
| --- | --- | --- |
| `TestKnownDefect_NonJSONErrorReplyHasMessage` | The test passes on this revision; its known-defect marker needs review. | [#539](https://github.com/Backblaze/blazer/issues/539) |
| `TestKnownDefect_ChunkOffsetMultipliedIn64Bit` | The test skips on 64-bit hosts. Run `make known-defects-32bit` on a Linux amd64 host to verify the marker. That target cannot run on macOS or arm64. | [#539](https://github.com/Backblaze/blazer/issues/539) |
| `readme-examples` | The check passes at this revision; its known-defect marker needs review. | [#539](https://github.com/Backblaze/blazer/issues/539) |
| `upgrade-sync-before` | The check passes at this revision; its known-defect marker needs review. | [#539](https://github.com/Backblaze/blazer/issues/539) |
| `upgrade-breaking-claim` | The check passes at this revision; its known-defect marker needs review. | [#539](https://github.com/Backblaze/blazer/issues/539) |

Sources: [reader test](../../b2/known_defect_reader_test.go) and [base test](../../base/known_defect_test.go).
