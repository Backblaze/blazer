package b2types

import (
	"encoding/json"
	"testing"
)

func TestGetFileInfoResponseDecodesObjectLockEnvelopes(t *testing.T) {
	const response = `{
		"fileRetention": {
			"isClientAuthorizedToRead": true,
			"value": {"mode": "governance", "retainUntilTimestamp": 1735689600000}
		},
		"legalHold": {
			"isClientAuthorizedToRead": true,
			"value": "on"
		}
	}`

	var got GetFileInfoResponse
	if err := json.Unmarshal([]byte(response), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Retention.IsClientAuthorizedToRead || got.Retention.Value == nil {
		t.Fatalf("fileRetention = %#v, want an authorized value", got.Retention)
	}
	if got.Retention.Value.Mode != "governance" || got.Retention.Value.RetainUntilTimestamp != 1735689600000 {
		t.Fatalf("fileRetention value = %#v, want governance through 1735689600000", got.Retention.Value)
	}
	if !got.LegalHold.IsClientAuthorizedToRead || got.LegalHold.Value != "on" {
		t.Fatalf("legalHold = %#v, want an authorized on value", got.LegalHold)
	}
}
