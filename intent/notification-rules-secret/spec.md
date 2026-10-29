# Spec: round-trip the notification rule signing secret and suspension fields
From: intent.md (Felipe Fumero). Issue: #194. Status: ready for review.
Skills applied: none beyond `AGENTS.md`, `REVIEW.md` and `docs/testing.md`.

Sources of truth (each test links the page):
- B2 `b2_set_bucket_notification_rules`: request `eventNotificationRules[]`, each with `targetConfiguration`
  holding `targetType`, `url`, `customHeaders`, `hmacSha256SigningSecret` and `maxEventsPerBatch`.
- B2 `b2_get_bucket_notification_rules`: response `eventNotificationRules[]` with the same target fields,
  plus `isSuspended` and `suspensionReason`.

Fixed test values: secret `"abcdefghijklmnopqrstuvwxyz012345"`, suspension reason
`"webhook returned 410"`, URL `"https://example.com/notify"`, rule name `"uploads"`.

## Requirements

- **R1. Secret sent on set.** `SetNotificationRules` with one webhook rule whose
  `HMACSHA256SigningSecret` is the fixed secret sends one `b2_set_bucket_notification_rules` request whose
  body has `eventNotificationRules.0.targetConfiguration.hmacSha256SigningSecret` equal to the secret, next
  to `url` and `targetType` `"webhook"`.
- **R2. No secret, no field.** A webhook rule without a secret sends no `hmacSha256SigningSecret` field.
- **R3. Fields read on get.** With `b2_get_bucket_notification_rules` scripted to return one rule with
  `isSuspended` `true`, `suspensionReason` `"webhook returned 410"` and
  `targetConfiguration.hmacSha256SigningSecret` equal to the secret, `NotificationRules` returns one rule
  with `IsSuspended == true`, `SuspensionReason == "webhook returned 410"` and
  `TargetConfiguration.Webhook.HMACSHA256SigningSecret` equal to the secret.
- **R4. Get is independent of set.** The R3 response is scripted on its own, so the test never relies on
  what blazer sent.
- **R5. Test-only.** No file outside `*_test.go` and `intent/notification-rules-secret/` changes.
  `TestNotificationRulesGetAndReplace` stays as it is.

## Design

One new file, `b2/notification_rules_wire_test.go` (package `b2`), using `internal/b2wiretest`:

- Fixture: authorization (the `updateAuthJSON` shape from `b2/update_cors_name_test.go`), one bucket from
  `b2_list_buckets`, and `b2_set_bucket_notification_rules` scripted with `Reply` and a fixed body.
- `TestSetNotificationRulesSendsSigningSecret` (R1, R2): table over {secret, no secret}. Assert with
  `AssertJSON`/`AssertNoJSON` on `Calls("b2_set_bucket_notification_rules")`.
- `TestNotificationRulesReadsSecretAndSuspension` (R3, R4): `Reply` a literal get response; compare the
  three fields of the returned rule.
- The get call uses a query string (`?bucketId=...`); the helper records it under
  `Call == "b2_get_bucket_notification_rules"`, since it routes on the path.

## How each constraint in the intent is met

| Constraint | Met by |
| --- | --- |
| Test-only | R5 |
| Expected values from the B2 docs | Literals above; field paths from the two docs pages |
| Independent oracle | R4: get is scripted, not echoed |
| Fake secret only | Fixed value above |
| Credential-free, race-safe | `b2wiretest`; `-race -count=5 -shuffle=on` |

## Areas of concern

1. **Stacked on #191.** This branch starts from #191's branch; #191 merges first. Owner: Felipe.
2. **Suspension fields on set.** Closed: the
   [`b2_set_bucket_notification_rules` docs](https://www.backblaze.com/apidocs/b2-set-bucket-notification-rules)
   say `isSuspended` and `suspensionReason` are "ignored by b2_set_bucket_notification_rules", so they
   are asserted on get only.
