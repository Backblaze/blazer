package base

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestCredentialContainersRedactFormatting(t *testing.T) {
	const secret = "synthetic-credential-287"
	account := NewAuthorizedAccount(AuthData{AuthToken: secret})
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"account", account}, {"account_value", reflect.ValueOf(account).Elem().Interface()},
		{"session", AuthData{AuthToken: secret}}, {"key", Key{Secret: secret}}, {"key_pointer", &Key{Secret: secret}},
		{"upload_url", URL{token: secret}}, {"upload_url_pointer", &URL{token: secret}},
		{"part", FileChunk{token: secret}}, {"part_pointer", &FileChunk{token: secret}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d"} {
				got := fmt.Sprintf(verb, tc.value)
				if strings.Contains(got, secret) || strings.Contains(strings.ToLower(got), fmt.Sprintf("%x", secret)) {
					t.Errorf("%s exposes credential", verb)
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
	if account.AuthData().AuthToken != secret {
		t.Fatal("formatting changed usable session")
	}
}
