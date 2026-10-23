// Copyright 2026, the Blazer authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package b2

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func webhookTestSignature(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature(t *testing.T) {
	body := []byte("{\"events\":[{\"eventId\":\"event-1\"}]}")
	secret := "raw signing secret"
	signature := webhookTestSignature(body, secret)

	if err := VerifySignature(body, signature, secret); err != nil {
		t.Fatalf("VerifySignature() = %v, want nil", err)
	}
	if err := VerifySignature(body, signature[:3]+strings.ToUpper(signature[3:]), secret); err == nil {
		t.Fatal("VerifySignature() accepted uppercase hex")
	}
	if err := VerifySignature(body, signature[:8]+strings.ToUpper(signature[8:]), secret); err == nil {
		t.Fatal("VerifySignature() accepted mixed-case hex")
	}
	if err := VerifySignature(body, signature, "wrong secret"); err == nil {
		t.Fatal("VerifySignature() with wrong secret = nil, want error")
	}
	if err := VerifySignature([]byte("{\"events\":[]}"), signature, secret); err == nil {
		t.Fatal("VerifySignature() with tampered body = nil, want error")
	}
}

func TestVerifySignatureKnownSHA256Vector(t *testing.T) {
	// RFC 4231 test case 1: a fixed vector independent of the test signer.
	key := strings.Repeat("\x0b", 20)
	header := "v1=b0344c61d8db38535ca8afceaf0bf12b881dc200c9833da726e9376c2e32cff7"
	if err := VerifySignature([]byte("Hi There"), header, key); err != nil {
		t.Fatalf("VerifySignature() for RFC 4231 vector = %v, want nil", err)
	}
}

func TestVerifySignatureUsesRawBodyAndSecret(t *testing.T) {
	body := []byte("{\n  \"events\": []\n}")
	secret := "c2VjcmV0" // Looks like base64 for "secret", but is the HMAC key verbatim.
	header := webhookTestSignature(body, secret)
	if err := VerifySignature(body, header, secret); err != nil {
		t.Fatalf("VerifySignature() with literal secret = %v, want nil", err)
	}
	for _, tc := range []struct {
		name   string
		body   []byte
		secret string
	}{
		{name: "equivalent JSON with different whitespace", body: []byte(`{"events":[]}`), secret: secret},
		{name: "decoded secret", body: body, secret: "secret"},
		{name: "secret with one extra byte", body: body, secret: secret + "x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := VerifySignature(tc.body, header, tc.secret); err == nil {
				t.Fatal("VerifySignature() = nil, want signature mismatch")
			}
		})
	}
	// The raw byte contract also applies to an empty body.
	if err := VerifySignature(nil, webhookTestSignature(nil, secret), secret); err != nil {
		t.Fatalf("VerifySignature() with empty body = %v, want nil", err)
	}
}

func TestVerifySignatureRejectsMalformedHeader(t *testing.T) {
	body := []byte("body")
	valid := webhookTestSignature(body, "secret")
	for _, tc := range []struct {
		name   string
		header string
		want   string
	}{
		{name: "missing", header: "", want: "v1="},
		{name: "no version", header: valid[3:], want: "v1="},
		{name: "wrong version", header: "v2=" + valid[3:], want: "v1="},
		{name: "wrong version case", header: "V1=" + valid[3:], want: "v1="},
		{name: "leading whitespace", header: " " + valid, want: "v1="},
		{name: "empty hex", header: "v1=", want: "does not match"},
		{name: "nonhex", header: "v1=not-hex", want: "decode"},
		{name: "odd hex length", header: "v1=abc", want: "decode"},
		{name: "short digest", header: valid[:len(valid)-2], want: "does not match"},
		{name: "long digest", header: valid + "00", want: "does not match"},
		{name: "trailing whitespace", header: valid + " ", want: "decode"},
		{name: "multiple signatures", header: valid + ", " + valid, want: "decode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifySignature(body, tc.header, "secret")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("VerifySignature(%q) = %v, want error containing %q", tc.header, err, tc.want)
			}
		})
	}
}

func TestParseEvent(t *testing.T) {
	body := []byte(`{"events":[{"accountId":"account","bucketId":"bucket-id","bucketName":"bucket","eventId":"event-id","eventTimestamp":1720000000123,"eventType":"b2:ObjectCreated:Upload","eventVersion":1,"matchedRuleName":"uploads","objectName":"logs/file.txt","objectSize":12,"objectVersionId":"version-id"}]}`)
	got, err := ParseEvent(body)
	if err != nil {
		t.Fatal(err)
	}
	size := int64(12)
	want := &EventNotification{Events: []Event{{
		AccountID:       "account",
		BucketID:        "bucket-id",
		BucketName:      "bucket",
		EventID:         "event-id",
		EventTimestamp:  1720000000123,
		EventType:       "b2:ObjectCreated:Upload",
		EventVersion:    1,
		MatchedRuleName: "uploads",
		ObjectName:      "logs/file.txt",
		ObjectSize:      &size,
		ObjectVersionID: "version-id",
	}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseEvent() = %#v, want %#v", got, want)
	}
}

func TestParseEventDeleteWithoutObjectSize(t *testing.T) {
	got, err := ParseEvent([]byte(`{"events":[{"accountId":"account","bucketId":"bucket-id","bucketName":"bucket","eventId":"event-id","eventTimestamp":1720000000123,"eventType":"b2:ObjectDeleted:Delete","eventVersion":1,"matchedRuleName":"deletes","objectName":"logs/file.txt"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 1 || got.Events[0].ObjectSize != nil {
		t.Fatalf("delete event = %#v, want nil ObjectSize", got)
	}
}

func TestParseEventMultipleEventsAndOptionalFields(t *testing.T) {
	body := []byte(`{"events":[{"eventId":"first","objectSize":0},{"eventId":"second","objectSize":null},{"eventId":"third"}]}`)
	got, err := ParseEvent(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 3 || got.Events[0].EventID != "first" || got.Events[1].EventID != "second" || got.Events[2].EventID != "third" {
		t.Fatalf("ParseEvent() events = %+v, want three events in order", got.Events)
	}
	if got.Events[0].ObjectSize == nil || *got.Events[0].ObjectSize != 0 {
		t.Fatalf("explicit zero objectSize = %v, want pointer to zero", got.Events[0].ObjectSize)
	}
	if got.Events[1].ObjectSize != nil || got.Events[2].ObjectSize != nil {
		t.Fatalf("null and absent objectSize = %v, %v, want nil", got.Events[1].ObjectSize, got.Events[2].ObjectSize)
	}
}

func TestEventOptionalFieldsMarshal(t *testing.T) {
	for _, tc := range []struct {
		name          string
		event         Event
		wantSize      bool
		wantVersionID bool
	}{
		{name: "delete without optional fields", event: Event{}},
		{name: "zero sized object", event: Event{ObjectSize: ptrToInt64(0), ObjectVersionID: "version-id"}, wantSize: true, wantVersionID: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(tc.event)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(body, &fields); err != nil {
				t.Fatal(err)
			}
			_, hasSize := fields["objectSize"]
			_, hasVersionID := fields["objectVersionId"]
			if hasSize != tc.wantSize || hasVersionID != tc.wantVersionID {
				t.Fatalf("optional fields in %s: size=%t versionID=%t, want size=%t versionID=%t", body, hasSize, hasVersionID, tc.wantSize, tc.wantVersionID)
			}
			if tc.wantSize && string(fields["objectSize"]) != "0" {
				t.Fatalf("objectSize = %s, want 0", fields["objectSize"])
			}
		})
	}
}

func ptrToInt64(n int64) *int64 { return &n }

func TestParseEventRejectsInvalidJSON(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "empty", body: ""},
		{name: "truncated", body: `{"events":[`},
		{name: "trailing data", body: `{"events":[]} {}`},
		{name: "wrong top-level type", body: `[]`},
		{name: "wrong events type", body: `{"events":{}}`},
		{name: "wrong timestamp type", body: `{"events":[{"eventTimestamp":"today"}]}`},
		{name: "timestamp overflow", body: `{"events":[{"eventTimestamp":9223372036854775808}]}`},
		{name: "wrong object size type", body: `{"events":[{"objectSize":"12"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseEvent([]byte(tc.body))
			if err == nil || got != nil {
				t.Fatalf("ParseEvent(%q) = %#v, %v; want nil result and error", tc.body, got, err)
			}
		})
	}
}

func TestParseEventEmptyAndUnknownFields(t *testing.T) {
	got, err := ParseEvent([]byte(`{"events":[],"futureField":"ignored"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Events == nil || len(got.Events) != 0 {
		t.Fatalf("ParseEvent() = %#v, want empty events slice", got)
	}
}
