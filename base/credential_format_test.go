package base

import (
	"encoding/json"
	"fmt"
	"github.com/Backblaze/blazer/internal/b2types"
	"reflect"
	"strings"
	"testing"
)

func TestCredentialContainersRedactFormatting(t *testing.T) {
	const secret = "synthetic-credential-287"
	const token = "b2sim-token"
	account := NewAuthorizedAccount(AuthData{AuthToken: token})
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"wire_webhook", b2types.WebhookNotificationTarget{HMACSHA256SigningSecret: secret}},
		{"wire_header", b2types.NotificationCustomHeader{Name: "X-Access", Value: secret}},
		{"wire_key", b2types.Key{Secret: secret}},
		{"wire_created_key", b2types.CreateKeyResponse{Secret: secret}},
		{"wire_authorize", b2types.AuthorizeAccountResponse{AuthToken: secret}},
		{"wire_upload_url", b2types.GetUploadURLResponse{Token: secret}},
		{"wire_part_url", getUploadPartURLResponse{Token: secret}},
		{"wire_download_auth", b2types.GetDownloadAuthorizationResponse{Token: secret}},
		{"wire_ssec", b2types.ServerSideEncryption{CustomerKey: secret, CustomerKeyMD5: secret}},
		{"bucket", &Bucket{b2: account}}, {"bucket_value", Bucket{b2: account}},
		{"file", &File{b2: account}}, {"file_value", File{b2: account}},
		{"large_file", &LargeFile{b2: account}}, {"large_file_value", reflect.ValueOf(&LargeFile{b2: account}).Elem().Interface()},
		{"account", account}, {"account_value", reflect.ValueOf(account).Elem().Interface()},
		{"session", AuthData{AuthToken: secret}}, {"key", Key{Secret: secret}}, {"key_pointer", &Key{Secret: secret}},
		{"upload_url", URL{token: secret}}, {"upload_url_pointer", &URL{token: secret}},
		{"part", FileChunk{token: secret}}, {"part_pointer", &FileChunk{token: secret}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if strings.HasPrefix(tc.name, "wire_") {
				raw, err := json.Marshal(tc.value)
				if err != nil || !strings.Contains(string(raw), secret) {
					t.Fatal("wire JSON lost explicit credential material")
				}
			}
			for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d"} {
				got := fmt.Sprintf(verb, tc.value)
				for _, credential := range []string{secret, token} {
					if strings.Contains(got, credential) || strings.Contains(strings.ToLower(got), fmt.Sprintf("%x", credential)) {
						t.Errorf("%s exposes credential", verb)
					}
				}
			}
		})
	}
	key := Key{Secret: secret}
	raw, err := json.Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	var restored Key
	if err := json.Unmarshal(raw, &restored); err != nil || restored.Secret != secret {
		t.Fatal("key JSON persistence changed")
	}
	if account.AuthData().AuthToken != token {
		t.Fatal("formatting changed usable session")
	}
}

func TestCredentialContainersPreserveMetadata(t *testing.T) {
	account := NewAuthorizedAccount(AuthData{AuthToken: "b2sim-token"})
	bucket := Bucket{Name: "photos", Type: "allPrivate", ID: "bucket-287", Info: map[string]string{"purpose": "backups"}, b2: account}
	file := File{Name: "cat.jpg", Size: 42, ID: "file-287", Status: "upload", b2: account}
	large := &LargeFile{ID: "large-287", b2: account}
	encryption := b2types.ServerSideEncryption{Mode: "SSE-C", Algorithm: "AES256", CustomerKey: "synthetic-credential-287"}
	for _, tc := range []struct {
		name  string
		value any
		want  []string
	}{
		{"bucket", &bucket, []string{"base.Bucket", "Name:photos", "Type:allPrivate", "ID:bucket-287", "purpose:backups"}},
		{"bucket_value", bucket, []string{"base.Bucket", "Name:photos", "ID:bucket-287"}},
		{"file", &file, []string{"base.File", "Name:cat.jpg", "Size:42", "ID:file-287", "Status:upload"}},
		{"file_value", file, []string{"base.File", "Name:cat.jpg", "Size:42", "ID:file-287"}},
		{"large_file", large, []string{"base.LargeFile", "ID:large-287"}},
		{"encryption", encryption, []string{"Mode:SSE-C", "Algorithm:AES256"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d"} {
				got := fmt.Sprintf(verb, tc.value)
				for _, want := range tc.want {
					if !strings.Contains(got, want) {
						t.Errorf("%s lost %q: %s", verb, want, got)
					}
				}
				for _, secret := range []string{"b2sim-token", "synthetic-credential-287"} {
					if strings.Contains(got, secret) || strings.Contains(strings.ToLower(got), fmt.Sprintf("%x", secret)) {
						t.Errorf("%s exposes credential", verb)
					}
				}
			}
		})
	}
}
