package b2

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Backblaze/blazer/x/b2sim"
)

func TestCredentialContainersRedactFormatting(t *testing.T) {
	const secret = "synthetic-credential-287"
	const token = "b2sim-token"
	data := AuthData{AuthToken: token}
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

	for _, tc := range []struct {
		name  string
		value any
		want  []string
	}{
		{"client", client, []string{"b2.Client", "redacted"}},
		{"bucket", bucket, []string{"b2.Bucket", "Name:audit", "ID:" + bucket.ID()}},
		{"bucket_value", *bucket, []string{"b2.Bucket", "Name:audit", "ID:" + bucket.ID()}},
		{"object", bucket.Object("item"), []string{"item"}},
	} {
		t.Run("metadata_"+tc.name, func(t *testing.T) {
			for _, verb := range []string{"%v", "%+v"} {
				got := fmt.Sprintf(verb, tc.value)
				for _, want := range tc.want {
					if !strings.Contains(got, want) {
						t.Errorf("%s lost %q: %s", verb, want, got)
					}
				}
			}
		})
	}
	target := WebhookNotificationTarget{HMACSHA256SigningSecret: secret, CustomHeaders: []NotificationCustomHeader{{Name: "X-Access", Value: secret}}}
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"client", client}, {"client_value", reflect.ValueOf(client).Elem().Interface()},
		{"bucket", bucket}, {"bucket_value", *bucket}, {"object", bucket.Object("item")},
		{"session", data}, {"session_pointer", &data}, {"store", store}, {"store_value", reflect.ValueOf(store).Elem().Interface()},
		{"webhook", target}, {"webhook_pointer", &target}, {"header", target.CustomHeaders[0]},
		{"nested_exported", struct{ Session AuthData }{data}},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var restored AuthData
	if err := json.Unmarshal(raw, &restored); err != nil || restored.AuthToken != token {
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
	if !ok || snapshot.AuthToken != token {
		t.Fatal("store explicit access changed")
	}
}
