package base

import (
	"context"
	"testing"
	"time"
)

var keyExpirationTime = time.Date(2023, time.November, 14, 22, 13, 20, 123000000, time.UTC)

func TestKeyExpiration(t *testing.T) {
	const noExpirationCreate = `{"accountId":"account-id","applicationKeyId":"no-expiration","applicationKey":"secret","capabilities":["listBuckets"],"keyName":"no-expiration","expirationTimestamp":null}`
	const expirationCreate = `{"accountId":"account-id","applicationKeyId":"expiration","applicationKey":"secret","capabilities":["listBuckets"],"keyName":"expiration","expirationTimestamp":1700000000123}`
	const listResponse = `{"keys":[` + noExpirationCreate + `,` + expirationCreate + `],"nextApplicationKeyId":null}`

	t.Run("create without expiration", func(t *testing.T) {
		b := newKeyRestrictionsB2(t, noExpirationCreate, `{"keys":[],"nextApplicationKeyId":null}`)
		key, err := b.CreateKey(context.Background(), "no-expiration", []string{"listBuckets"}, 0, "", "")
		if err != nil {
			t.Fatalf("CreateKey: %v", err)
		}
		if !key.Expires.IsZero() {
			t.Errorf("CreateKey Expires = %v, want zero time", key.Expires)
		}
	})

	t.Run("create with expiration", func(t *testing.T) {
		b := newKeyRestrictionsB2(t, expirationCreate, `{"keys":[],"nextApplicationKeyId":null}`)
		key, err := b.CreateKey(context.Background(), "expiration", []string{"listBuckets"}, 0, "", "")
		if err != nil {
			t.Fatalf("CreateKey: %v", err)
		}
		if !key.Expires.Equal(keyExpirationTime) {
			t.Errorf("CreateKey Expires = %v, want %v", key.Expires, keyExpirationTime)
		}
	})

	t.Run("list keys", func(t *testing.T) {
		b := newKeyRestrictionsB2(t, `{}`, listResponse)
		keys, _, err := b.ListKeys(context.Background(), 10, "")
		if err != nil {
			t.Fatalf("ListKeys: %v", err)
		}
		if len(keys) != 2 {
			t.Fatalf("ListKeys returned %d keys, want 2", len(keys))
		}
		if !keys[0].Expires.IsZero() {
			t.Errorf("ListKeys no-expiration Expires = %v, want zero time", keys[0].Expires)
		}
		if !keys[1].Expires.Equal(keyExpirationTime) {
			t.Errorf("ListKeys expiration Expires = %v, want %v", keys[1].Expires, keyExpirationTime)
		}
	})
}
