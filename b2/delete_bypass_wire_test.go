package b2

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Backblaze/blazer/base"
)

// Object.Delete must carry the BypassGovernance option all the way to the
// b2_delete_file_version request, through the backend and baseline layers.
func TestObjectDeleteBypassGovernanceReachesTheWire(t *testing.T) {
	var sent map[string]any
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/b2_authorize_account"):
			fmt.Fprintf(w, updateAuthJSON, srv.URL)
		case strings.HasSuffix(r.URL.Path, "/b2_create_bucket"):
			fmt.Fprint(w, noReplicationBucketJSON)
		case strings.HasSuffix(r.URL.Path, "/b2_delete_file_version"):
			body, _ := io.ReadAll(r.Body)
			sent = map[string]any{}
			if err := json.Unmarshal(body, &sent); err != nil {
				t.Errorf("decode body: %v", err)
			}
			fmt.Fprint(w, `{"fileId":"id","fileName":"name"}`)
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx := context.Background()
	account, err := base.AuthorizeAccount(ctx, "a", "k", base.SetAPIBase(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := account.CreateBucket(ctx, "n", "allPrivate", nil, nil, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	root := &beRoot{b2i: &testRoot{errs: &errCont{}, bucketMap: make(map[string]map[string]string)}}
	newObject := func() *Object {
		return &Object{name: "name", f: &beFile{b2file: &b2File{b: bucket.File("id", "name")}, ri: root}}
	}

	if err := newObject().Delete(ctx); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if v, ok := sent["bypassGovernance"]; ok {
		t.Errorf("plain Delete sent bypassGovernance = %v, want the field omitted", v)
	}

	sent = nil
	if err := newObject().Delete(ctx, BypassGovernance()); err != nil {
		t.Fatalf("Delete(BypassGovernance): %v", err)
	}
	if sent["bypassGovernance"] != true {
		t.Errorf("Delete(BypassGovernance) sent bypassGovernance = %v, want true", sent["bypassGovernance"])
	}
}
