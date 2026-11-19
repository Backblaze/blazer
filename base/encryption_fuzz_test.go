package base

import (
	"reflect"
	"testing"

	"github.com/Backblaze/blazer/internal/b2types"
)

func FuzzEncryptionHeaders(f *testing.F) {
	f.Add("SSE-B2", "AES256", "", "")
	f.Add("SSE-C", "AES256", "key", "digest")
	f.Add("", "", "", "")
	f.Fuzz(func(t *testing.T, mode, algorithm, key, digest string) {
		headers := map[string]string{"existing": "preserved"}
		err := addServerSideEncryptionHeaders(headers, &b2types.ServerSideEncryption{Mode: mode, Algorithm: algorithm, CustomerKey: key, CustomerKeyMD5: digest})
		if err != nil {
			if !reflect.DeepEqual(headers, map[string]string{"existing": "preserved"}) {
				t.Fatal("invalid settings changed headers")
			}
			return
		}
		if algorithm != "AES256" {
			t.Fatal("invalid algorithm accepted")
		}
		if headers["X-Bz-Server-Side-Encryption"] != "" && headers["X-Bz-Server-Side-Encryption-Customer-Key"] != "" {
			t.Fatal("conflicting encryption headers")
		}
		if headers["existing"] != "preserved" {
			t.Fatal("unrelated header changed")
		}
	})
}

func TestServerSideEncryptionHeaderTable(t *testing.T) {
	cases := []struct {
		name string
		sse  *b2types.ServerSideEncryption
		want map[string]string
	}{
		{"nil adds nothing", nil, map[string]string{}},
		{"SSE-B2", &b2types.ServerSideEncryption{Mode: "SSE-B2", Algorithm: "AES256"}, map[string]string{
			"X-Bz-Server-Side-Encryption": "AES256",
		}},
		{"SSE-C", &b2types.ServerSideEncryption{Mode: "SSE-C", Algorithm: "AES256", CustomerKey: "a2V5", CustomerKeyMD5: "ZGlnZXN0"}, map[string]string{
			"X-Bz-Server-Side-Encryption-Customer-Algorithm": "AES256",
			"X-Bz-Server-Side-Encryption-Customer-Key":       "a2V5",
			"X-Bz-Server-Side-Encryption-Customer-Key-Md5":   "ZGlnZXN0",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			headers := map[string]string{}
			if err := addServerSideEncryptionHeaders(headers, tc.sse); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(headers, tc.want) {
				t.Fatalf("headers = %v, want exactly %v", headers, tc.want)
			}
		})
	}
	rejected := map[string]*b2types.ServerSideEncryption{
		"SSE-C without key":    {Mode: "SSE-C", Algorithm: "AES256", CustomerKeyMD5: "d"},
		"SSE-C without digest": {Mode: "SSE-C", Algorithm: "AES256", CustomerKey: "k"},
		"SSE-B2 with key":      {Mode: "SSE-B2", Algorithm: "AES256", CustomerKey: "k"},
		"wrong algorithm":      {Mode: "SSE-B2", Algorithm: "AES128"},
		"unknown mode":         {Mode: "other", Algorithm: "AES256"},
	}
	for name, sse := range rejected {
		t.Run("rejects "+name, func(t *testing.T) {
			headers := map[string]string{}
			if err := addServerSideEncryptionHeaders(headers, sse); err == nil || len(headers) != 0 {
				t.Fatalf("err = %v, headers = %v, want an error and no headers", err, headers)
			}
		})
	}
}
