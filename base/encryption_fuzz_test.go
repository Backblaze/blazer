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
