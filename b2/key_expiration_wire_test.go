package b2

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

var wireKeyExpirationTime = time.Date(2023, time.November, 14, 22, 13, 20, 123000000, time.UTC)

func newKeyExpirationWire(t *testing.T) (*b2wiretest.Server, *Client) {
	t.Helper()
	srv := b2wiretest.New(t)
	client, err := NewClient(context.Background(), "account", "key", APIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	return srv, client
}

func TestKeyExpirationReachesPublicAPI(t *testing.T) {
	const noExpiration = `{"accountId":"account","applicationKeyId":"no-expiration","applicationKey":"secret","capabilities":["listBuckets"],"keyName":"no-expiration","expirationTimestamp":null}`
	const expiration = `{"accountId":"account","applicationKeyId":"expiration","applicationKey":"secret","capabilities":["listBuckets"],"keyName":"expiration","expirationTimestamp":1700000000123}`

	t.Run("create without expiration", func(t *testing.T) {
		srv, client := newKeyExpirationWire(t)
		srv.Reply("b2_create_key", http.StatusOK, noExpiration)
		key, err := client.CreateKey(context.Background(), "no-expiration", Capabilities("listBuckets"))
		if err != nil {
			t.Fatalf("CreateKey: %v", err)
		}
		if !key.Expires().IsZero() {
			t.Errorf("CreateKey Expires = %v, want zero time", key.Expires())
		}
	})

	t.Run("create with expiration", func(t *testing.T) {
		srv, client := newKeyExpirationWire(t)
		srv.Reply("b2_create_key", http.StatusOK, expiration)
		key, err := client.CreateKey(context.Background(), "expiration", Capabilities("listBuckets"))
		if err != nil {
			t.Fatalf("CreateKey: %v", err)
		}
		if !key.Expires().Equal(wireKeyExpirationTime) {
			t.Errorf("CreateKey Expires = %v, want %v", key.Expires(), wireKeyExpirationTime)
		}
	})

	t.Run("list keys", func(t *testing.T) {
		srv, client := newKeyExpirationWire(t)
		srv.Reply("b2_list_keys", http.StatusOK, `{"keys":[`+noExpiration+`,`+expiration+`],"nextApplicationKeyId":null}`)
		keys, _, err := client.ListKeys(context.Background(), 10, "")
		if err != nil && err != io.EOF {
			t.Fatalf("ListKeys: %v", err)
		}
		if len(keys) != 2 {
			t.Fatalf("ListKeys returned %d keys, want 2", len(keys))
		}
		if !keys[0].Expires().IsZero() {
			t.Errorf("ListKeys no-expiration Expires = %v, want zero time", keys[0].Expires())
		}
		if !keys[1].Expires().Equal(wireKeyExpirationTime) {
			t.Errorf("ListKeys expiration Expires = %v, want %v", keys[1].Expires(), wireKeyExpirationTime)
		}
	})
}
