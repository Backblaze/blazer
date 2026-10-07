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
	if v := got.Retention.Value; v.Mode == nil || *v.Mode != "governance" || v.RetainUntilTimestamp == nil || *v.RetainUntilTimestamp != 1735689600000 {
		t.Fatalf("fileRetention value = %#v, want governance through 1735689600000", got.Retention.Value)
	}
	if !got.LegalHold.IsClientAuthorizedToRead || got.LegalHold.Value != "on" {
		t.Fatalf("legalHold = %#v, want an authorized on value", got.LegalHold)
	}
}

func TestUpdateFileRetentionRequestClearsWithNulls(t *testing.T) {
	got, err := json.Marshal(UpdateFileRetentionRequest{FileID: "id", Name: "n", BypassGovernance: true})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"fileId":"id","fileName":"n","fileRetention":{"mode":null,"retainUntilTimestamp":null},"bypassGovernance":true}`
	if string(got) != want {
		t.Errorf("request = %s, want %s", got, want)
	}
}
