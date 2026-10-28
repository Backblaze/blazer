# Intent: round-trip the notification rule signing secret and suspension fields
Author: Felipe Fumero. Issue: #194. Parent: #97. Depends on: #191. Status: draft.

## Problem

`(*Bucket).SetNotificationRules` and `(*Bucket).NotificationRules` convert rules between the public
`NotificationRule` type and the wire type in `internal/b2types`. Three fields have no test:

- `WebhookNotificationTarget.HMACSHA256SigningSecret`, the secret B2 uses to sign each webhook call so the
  receiver can reject forged ones;
- `NotificationRule.IsSuspended` and `NotificationRule.SuspensionReason`, which B2 sets when it stops
  delivering to a target.

`TestNotificationRulesGetAndReplace` (`b2/notification_test.go:103`) never sets any of the three. Its fake
stores what was set and returns it on the next get, so it compares blazer with itself. Removing all three
fields from both converters (`notificationRulesFromBase` and `notificationRulesToBase`,
`b2/notification.go:78-118`) leaves `go test ./...` green at a5e0d13.

If the secret is dropped on set, B2 delivers unsigned webhook calls, and a receiver that checks signatures
rejects every one, or one that does not check accepts forged calls. If suspension is dropped on get, a
caller cannot tell why notifications stopped.

## Proposed outcome

Credential-free tests through the public `Bucket` API that fail if any of the three fields is lost:

- on set, the secret appears in the `b2_set_bucket_notification_rules` request body;
- on get, all three fields come back from an independent `b2_get_bucket_notification_rules` response,
  not from what blazer itself sent.

## Affected users and APIs

- `(*Bucket).SetNotificationRules` and `(*Bucket).NotificationRules`, as observed on the wire. Behavior
  does not change.
- Test code only.

## Constraints

- **Test-only.** No production code changes. If a new test fails on current code, it found a product bug:
  stop and open a separate fix issue.
- Expected values come from the B2 docs for `b2_set_bucket_notification_rules` and
  `b2_get_bucket_notification_rules`, never from blazer's output. The get response is scripted
  independently of the set request.
- Test values for the secret are obviously fake (32 printable characters such as
  `"abcdefghijklmnopqrstuvwxyz012345"`), never a real secret.
- Credential-free, offline, safe under `go test -race -count=5 -shuffle=on`.
- Built on the shared recording server from #191; no new private fake.

## Open questions

1. Whether `isSuspended` and `suspensionReason` belong in the set request at all. The B2 docs describe
   them as values B2 reports. blazer sends them when set (`omitempty`). This intent only checks them on
   get, and leaves the set behavior unchanged and unasserted. (@goanpeca)
