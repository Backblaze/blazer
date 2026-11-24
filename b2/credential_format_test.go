package b2

import (
	"encoding/json"
	"fmt"
	"github.com/Backblaze/blazer/x/b2sim"
	"reflect"
	"strings"
	"testing"
)

func TestCredentialContainersRedactFormatting(t *testing.T) {
	const secret = "synthetic-credential-287"
	data := AuthData{AuthToken: secret}
	store := &InMemoryAccountInfo{}
	store.Set(data)
	sim := b2sim.New()
	sim.Seed("audit")
	client, err := NewClient(t.Context(), "id", secret, Transport(sim))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.Bucket(t.Context(), "audit")
	if err != nil {
		t.Fatal(err)
	}
	target := WebhookNotificationTarget{HMACSHA256SigningSecret: secret, CustomHeaders: []NotificationCustomHeader{{Name: "X-Access", Value: secret}}}
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"client", client}, {"bucket", bucket}, {"object", bucket.Object("item")},
		{"session", data}, {"session_pointer", &data}, {"store", store}, {"store_value", reflect.ValueOf(store).Elem().Interface()},
		{"webhook", target}, {"webhook_pointer", &target}, {"header", target.CustomHeaders[0]},
		{"nested_exported", struct{ Session AuthData }{data}},
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
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var restored AuthData
	if err := json.Unmarshal(raw, &restored); err != nil || restored.AuthToken != secret {
		t.Fatal("session JSON persistence changed")
	}
	raw, err = json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	var restoredTarget WebhookNotificationTarget
	if err := json.Unmarshal(raw, &restoredTarget); err != nil || restoredTarget.HMACSHA256SigningSecret != secret || restoredTarget.CustomHeaders[0].Value != secret {
		t.Fatal("webhook JSON persistence changed")
	}
	snapshot, ok := store.AuthData()
	if !ok || snapshot.AuthToken != secret {
		t.Fatal("store explicit access changed")
	}
}
