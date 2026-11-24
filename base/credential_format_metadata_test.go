package base

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Backblaze/blazer/internal/b2types"
)

func TestCredentialContainersPreserveNestedEncryptionMetadata(t *testing.T) {
	const key = "synthetic-credential-287"
	const token = "b2sim-token"
	encryption := &b2types.ServerSideEncryption{Mode: "SSE-C", Algorithm: "AES256", CustomerKey: key, CustomerKeyMD5: key}
	account := NewAuthorizedAccount(AuthData{AuthToken: token})
	file := &File{Name: "cat.jpg", ID: "file-287", Size: 42, b2: account, Info: &FileInfo{ServerSideEncryption: encryption}}
	bucket := &Bucket{Name: "photos", ID: "bucket-287", b2: account, DefaultServerSideEncryption: encryption}
	for _, value := range []any{file, *file, bucket, *bucket} {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d"} {
			got := fmt.Sprintf(verb, value)
			for _, want := range []string{"Mode:SSE-C", "Algorithm:AES256"} {
				if !strings.Contains(got, want) {
					t.Errorf("%s lost %q: %s", verb, want, got)
				}
			}
			for _, secret := range []string{key, token} {
				if strings.Contains(got, secret) || strings.Contains(strings.ToLower(got), fmt.Sprintf("%x", secret)) {
					t.Errorf("%s exposes credential", verb)
				}
			}
		}
	}
	for _, large := range []*LargeFile{file.AsLargeFile(), file.CompileParts(0, nil)} {
		for _, value := range []any{large, *large} {
			for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d"} {
				got := fmt.Sprintf(verb, value)
				if !strings.Contains(got, "ID:file-287") {
					t.Errorf("%s lost upload ID: %s", verb, got)
				}
				if strings.Contains(got, token) || strings.Contains(strings.ToLower(got), fmt.Sprintf("%x", token)) {
					t.Errorf("%s exposes token", verb)
				}
			}
		}
	}
}
