// Copyright 2026, the Blazer authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package base

import (
	"context"
	"testing"
	"time"
)

var keyExpirationTime = time.Date(2023, time.November, 14, 22, 13, 20, 123000000, time.UTC)

func TestKeyExpiration(t *testing.T) {
	const noExpirationCreate = `{"accountId":"account-id","applicationKeyId":"no-expiration","applicationKey":"secret","capabilities":["listBuckets"],"keyName":"no-expiration","expirationTimestamp":null}`
	const missingExpirationCreate = `{"accountId":"account-id","applicationKeyId":"missing-expiration","applicationKey":"secret","capabilities":["listBuckets"],"keyName":"missing-expiration"}`
	const expirationCreate = `{"accountId":"account-id","applicationKeyId":"expiration","applicationKey":"secret","capabilities":["listBuckets"],"keyName":"expiration","expirationTimestamp":1700000000123}`
	const listResponse = `{"keys":[` + noExpirationCreate + `,` + missingExpirationCreate + `,` + expirationCreate + `],"nextApplicationKeyId":null}`

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

	t.Run("create with missing expiration", func(t *testing.T) {
		b := newKeyRestrictionsB2(t, missingExpirationCreate, `{"keys":[],"nextApplicationKeyId":null}`)
		key, err := b.CreateKey(context.Background(), "missing-expiration", []string{"listBuckets"}, 0, "", "")
		if err != nil {
			t.Fatalf("CreateKey: %v", err)
		}
		if !key.Expires.IsZero() {
			t.Errorf("CreateKey missing expiration Expires = %v, want zero time", key.Expires)
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
		if len(keys) != 3 {
			t.Fatalf("ListKeys returned %d keys, want 3", len(keys))
		}
		if !keys[0].Expires.IsZero() {
			t.Errorf("ListKeys no-expiration Expires = %v, want zero time", keys[0].Expires)
		}
		if !keys[1].Expires.IsZero() {
			t.Errorf("ListKeys missing expiration Expires = %v, want zero time", keys[1].Expires)
		}
		if !keys[2].Expires.Equal(keyExpirationTime) {
			t.Errorf("ListKeys expiration Expires = %v, want %v", keys[2].Expires, keyExpirationTime)
		}
	})
}
