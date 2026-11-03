# Plan: round-trip the notification rule signing secret and suspension fields (from intent.md and spec.md)

Branch: `test-issue-194-notification-rules-secret`, stacked on `test-issue-191-shared-recording-server`.
One PR for issue #194 carries this folder and the tests. Apply the lean-code ladder from `AGENTS.md`:
reuse `internal/b2wiretest` and existing fixtures, add only what the tests need.

## Files that change

```
b2/notification_rules_wire_test.go            (new)  R1-R4
intent/notification-rules-secret/plan.md      (edit only if the implementation departs from this plan)
```

No production file changes (R5).

## Order of work

0. **Wait for #191's code.** Run `git merge test-issue-191-shared-recording-server` (merge, never rebase).
   If `internal/b2wiretest/server.go` is still missing, stop: blocked on #191. Read
   `intent/b2wiretest/spec.md` for the helper API.
1. **Fixture.** Script the authorization (the `updateAuthJSON` shape) and one bucket. Reuse an existing
   bucket JSON constant from `b2/*_test.go` (for example `noReplicationBucketJSON`). Script
   `b2_set_bucket_notification_rules` with a fixed valid response (`{"bucketId":"...","eventNotificationRules":[]}`).
2. **Set** (R1, R2). `TestSetNotificationRulesSendsSigningSecret`, a table with two rows:
   - with the secret: `AssertJSON` on `eventNotificationRules.0.targetConfiguration.hmacSha256SigningSecret`,
     `.url` and `.targetType`;
   - without: `AssertNoJSON` on the same secret path.
3. **Get** (R3, R4). `TestNotificationRulesReadsSecretAndSuspension`: `Reply` to
   `b2_get_bucket_notification_rules` with a literal JSON rule carrying the secret, `isSuspended: true` and
   the reason. Assert the three fields on the returned rule, plus `Name` and `Webhook.URL` as sanity.
4. **Check and self-review.** `make ci`; then a fresh-context review of the diff per `AGENTS.md`, answering
   each finding.

## Risks

- **A new test fails on current code.** Then it found a product bug. Stop and report; do not fix it here.
- **Secret in test output.** Use only the fake value. On failure, `AssertJSON` prints it, which is fine
  for a fake value.
- **Changelog.** Test-only diff: `make changelog-check` passes without an entry.

## Proof

Apply each break, run `go test -count=1 ./b2 -run 'NotificationRules(Reads|.*Sends)'`, record the failing
output, revert, record the passing output.

| Req | Break to apply | Test that must fail |
| --- | --- | --- |
| R1 | `notificationRulesToBase` (`b2/notification.go`): drop `HMACSHA256SigningSecret` from the webhook copy | `TestSetNotificationRulesSendsSigningSecret` |
| R1 | `EventNotificationTargetConfiguration.MarshalJSON` (`internal/b2types/b2types.go`): drop the secret assignment | `TestSetNotificationRulesSendsSigningSecret` |
| R3 | `notificationRulesFromBase`: drop `HMACSHA256SigningSecret` | `TestNotificationRulesReadsSecretAndSuspension` |
| R3 | `notificationRulesFromBase`: drop `IsSuspended` | `TestNotificationRulesReadsSecretAndSuspension` |
| R3 | `notificationRulesFromBase`: drop `SuspensionReason` | `TestNotificationRulesReadsSecretAndSuspension` |

Also record `go test -race -count=5 -shuffle=on ./b2` passing with `B2_APPLICATION_KEY_ID`,
`B2_APPLICATION_KEY`, `B2_ACCOUNT_ID` and `B2_ACCOUNT_KEY` unset, and `make ci` passing.
