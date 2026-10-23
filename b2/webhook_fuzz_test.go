package b2

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"
)

func FuzzVerifySignature(f *testing.F) {
	f.Add([]byte(`{"events":[]}`), "secret", "v1=not-hex")
	f.Add([]byte{}, "", "")
	f.Fuzz(func(t *testing.T, body []byte, secret, header string) {
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write(body)
		signed := "v1=" + hex.EncodeToString(mac.Sum(nil))
		if err := VerifySignature(body, signed, secret); err != nil {
			t.Fatal(err)
		}
		if VerifySignature(body, header, secret) == nil {
			if len(header) != len(signed) {
				t.Fatal("accepted wrong signature length")
			}
			decoded, err := hex.DecodeString(header[3:])
			if header[:3] != "v1=" || err != nil || !hmac.Equal(decoded, mac.Sum(nil)) {
				t.Fatal("accepted invalid signature")
			}
		}
		mutated := append(append([]byte(nil), body...), 0)
		if VerifySignature(mutated, signed, secret) == nil {
			t.Fatal("accepted tampered body")
		}
	})
}

func FuzzParseEvent(f *testing.F) {
	for _, seed := range []string{`{"events":[]}`, `null`, `{"events":[{"objectSize":null}]}`, `{"events":[{"eventVersion":2147483648,"objectSize":9223372036854775807}]}`, "\r\n{}\r\n", `{"events":[`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		event, err := ParseEvent(body)
		if err != nil {
			return
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		again, err := ParseEvent(encoded)
		if err != nil || !reflect.DeepEqual(event, again) {
			t.Fatalf("unstable parse round trip: %v", err)
		}
	})
}
