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

package b2

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

type expiredDeleteKey struct {
	b2KeyInterface
	root     *testRoot
	attempts int
}

func (k *expiredDeleteKey) del(context.Context) error {
	k.attempts++
	if k.root.auths == 0 {
		return testError{reauth: true}
	}
	return nil
}

func TestFindingC35_DeleteKeyReauthorizesAfterTokenExpiry(t *testing.T) {
	fake := &testRoot{}
	root := &beRoot{b2i: fake}
	backendKey := &expiredDeleteKey{root: fake}
	key := &Key{k: &beKey{b2i: root, k: backendKey}}

	if err := key.Delete(context.Background()); err != nil {
		t.Errorf("Delete after token expiry = %v, want success after reauthorization", err)
	}
	if fake.auths != 1 {
		t.Errorf("reauthorizations = %d, want 1", fake.auths)
	}
	if backendKey.attempts != 2 {
		t.Errorf("delete attempts = %d, want 2", backendKey.attempts)
	}
}

// Deleting a key after the sign-in token expired must sign in
// again and succeed, like every other call.
func TestRegression_DeleteKeyReauthorizesAfterTokenExpiry(t *testing.T) {
	srv, bucket := newSHA1WireBucket(t)
	srv.Reply("b2_list_keys", http.StatusOK, `{"keys":[{"accountId":"a","applicationKeyId":"kid","bucketId":null,"capabilities":["listFiles"],"expirationTimestamp":null,"keyName":"k","namePrefix":null,"options":[]}],"nextApplicationKeyId":null}`)
	ctx := newTestCtx(t)
	keys, _, err := bucket.c.ListKeys(ctx, 10, "")
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("listed %d keys, want 1", len(keys))
	}
	srv.ReplySequence("b2_delete_key",
		b2wiretest.Response{Status: http.StatusUnauthorized, Body: `{"status":401,"code":"expired_auth_token","message":"expired"}`},
		b2wiretest.Response{Body: `{"accountId":"a","applicationKeyId":"kid","bucketId":null,"capabilities":["listFiles"],"expirationTimestamp":null,"keyName":"k","namePrefix":null,"options":[]}`})
	authBefore := len(srv.Calls("b2_authorize_account"))
	if err := keys[0].Delete(ctx); err != nil {
		t.Errorf("Delete after token expiry failed instead of re-authorizing: %v", err)
	}
	if got := len(srv.Calls("b2_authorize_account")) - authBefore; got != 1 {
		t.Errorf("%d re-authorizations after the 401, want 1", got)
	}
	calls := srv.Calls("b2_delete_key")
	if len(calls) != 2 {
		t.Errorf("delete requests = %d, want 2", len(calls))
	}
	for _, call := range calls {
		b2wiretest.AssertJSON(t, call, "applicationKeyId", "kid")
	}
}
