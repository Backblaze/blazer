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
	if err := VerifySignature(body, signature[:3]+strings.ToUpper(signature[3:]), secret); err != nil {
		t.Fatalf("VerifySignature() with uppercase hex = %v, want nil", err)
	}
	if err := VerifySignature(body, signature, "wrong secret"); err == nil {
		t.Fatal("VerifySignature() with wrong secret = nil, want error")
	}
	if err := VerifySignature([]byte("{\"events\":[]}"), signature, secret); err == nil {
		t.Fatal("VerifySignature() with tampered body = nil, want error")
	}
}

func TestVerifySignatureRejectsMalformedHeader(t *testing.T) {
	body := []byte("body")
	for _, header := range []string{"", "deadbeef", "v2=deadbeef", "v1=not-hex"} {
		t.Run(header, func(t *testing.T) {
			if err := VerifySignature(body, header, "secret"); err == nil {
				t.Fatalf("VerifySignature(%q) = nil, want error", header)
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
