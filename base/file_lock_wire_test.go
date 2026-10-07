package base

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type lockRecorder struct {
	path    string
	headers http.Header
	body    map[string]any
}

func newLockServer(t *testing.T) (*httptest.Server, *lockRecorder) {
	t.Helper()
	rec := &lockRecorder{}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/b2api/v4/b2_authorize_account" {
			_, _ = fmt.Fprint(w, v4AuthJSON(srv.URL, nil, nil, ""))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		rec.path, rec.headers, rec.body = r.URL.Path, r.Header.Clone(), nil
		if strings.HasPrefix(r.URL.Path, "/b2api/") {
			rec.body = map[string]any{}
			if err := json.Unmarshal(raw, &rec.body); err != nil {
				t.Errorf("decode %s body: %v", r.URL.Path, err)
			}
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/b2_start_large_file"):
			_, _ = fmt.Fprint(w, `{"fileId":"large"}`)
		case r.URL.Path == "/upload":
			_, _ = fmt.Fprint(w, `{"fileId":"small","action":"upload"}`)
		default:
			_, _ = fmt.Fprint(w, `{}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func lockFixture(t *testing.T) (*B2, *lockRecorder) {
	t.Helper()
	srv, rec := newLockServer(t)
	b, err := AuthorizeAccount(context.Background(), "account-id", "application-key", SetAPIBase(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	return b, rec
}

func TestUploadFileSendsObjectLockHeaders(t *testing.T) {
	b, rec := lockFixture(t)
	url := &URL{uri: b.apiURI + "/upload", token: "tok", b2: b}
	ctx := context.Background()

	if _, err := url.UploadFile(ctx, strings.NewReader("x"), 1, "n", "text/plain", "sha", nil,
		FileLock{Retention: &FileRetention{Mode: "compliance", RetainUntilTimestamp: 1735689600000}, LegalHold: "on"}); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"X-Bz-File-Retention-Mode":                   "compliance",
		"X-Bz-File-Retention-Retain-Until-Timestamp": "1735689600000",
		"X-Bz-File-Legal-Hold":                       "on",
	} {
		if got := rec.headers.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}

	if _, err := url.UploadFile(ctx, strings.NewReader("x"), 1, "n", "text/plain", "sha", nil); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"X-Bz-File-Retention-Mode", "X-Bz-File-Retention-Retain-Until-Timestamp", "X-Bz-File-Legal-Hold"} {
		if _, ok := rec.headers[k]; ok {
			t.Errorf("%s sent without a FileLock", k)
		}
	}
}

func TestStartLargeFileSendsObjectLockFields(t *testing.T) {
	b, rec := lockFixture(t)
	bucket := &Bucket{ID: "bid", b2: b}
	ctx := context.Background()

	if _, err := bucket.StartLargeFile(ctx, "n", "text/plain", nil,
		FileLock{Retention: &FileRetention{Mode: "governance", RetainUntilTimestamp: 1735689600000}, LegalHold: "off"}); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"mode": "governance", "retainUntilTimestamp": float64(1735689600000)}
	if got, _ := rec.body["fileRetention"].(map[string]any); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("fileRetention = %v, want %v", rec.body["fileRetention"], want)
	}
	if rec.body["legalHold"] != "off" {
		t.Errorf("legalHold = %v, want off", rec.body["legalHold"])
	}

	if _, err := bucket.StartLargeFile(ctx, "n", "text/plain", nil); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"fileRetention", "legalHold"} {
		if v, ok := rec.body[k]; ok {
			t.Errorf("%s = %v sent without a FileLock, want it omitted", k, v)
		}
	}
}

func TestUpdateFileRetentionOnTheWire(t *testing.T) {
	b, rec := lockFixture(t)
	file := (&Bucket{ID: "bid", b2: b}).File("id", "name")
	ctx := context.Background()

	if err := file.UpdateFileRetention(ctx, &FileRetention{Mode: "governance", RetainUntilTimestamp: 1735689600000}, true); err != nil {
		t.Fatal(err)
	}
	if rec.path != "/b2api/v4/b2_update_file_retention" {
		t.Errorf("path = %s, want the v4 endpoint", rec.path)
	}
	if got, _ := rec.body["fileRetention"].(map[string]any); got["mode"] != "governance" || got["retainUntilTimestamp"] != float64(1735689600000) {
		t.Errorf("fileRetention = %v, want governance through 1735689600000", rec.body["fileRetention"])
	}
	if rec.body["bypassGovernance"] != true || rec.body["fileId"] != "id" || rec.body["fileName"] != "name" {
		t.Errorf("request = %v, want fileId, fileName and bypassGovernance true", rec.body)
	}

	// Removing retention sends both fields as explicit nulls.
	if err := file.UpdateFileRetention(ctx, nil, true); err != nil {
		t.Fatal(err)
	}
	got, ok := rec.body["fileRetention"].(map[string]any)
	if !ok {
		t.Fatalf("fileRetention = %v, want an object with null fields", rec.body["fileRetention"])
	}
	for _, k := range []string{"mode", "retainUntilTimestamp"} {
		if v, present := got[k]; !present || v != nil {
			t.Errorf("fileRetention.%s = %v (present=%v), want an explicit null", k, v, present)
		}
	}
}

func TestUpdateFileLegalHoldOnTheWire(t *testing.T) {
	b, rec := lockFixture(t)
	file := (&Bucket{ID: "bid", b2: b}).File("id", "name")

	if err := file.UpdateFileLegalHold(context.Background(), "on"); err != nil {
		t.Fatal(err)
	}
	if rec.path != "/b2api/v4/b2_update_file_legal_hold" {
		t.Errorf("path = %s, want the v4 endpoint", rec.path)
	}
	if rec.body["legalHold"] != "on" || rec.body["fileId"] != "id" || rec.body["fileName"] != "name" {
		t.Errorf("request = %v, want fileId, fileName and legalHold on", rec.body)
	}
}

func TestListedFilesReportObjectLock(t *testing.T) {
	const entry = `{"fileId":"id","fileName":"n","action":"upload",` +
		`"fileRetention":{"isClientAuthorizedToRead":true,"value":{"mode":"compliance","retainUntilTimestamp":1735689600000}},` +
		`"legalHold":{"isClientAuthorizedToRead":true,"value":"on"}}`
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/b2api/v4/b2_authorize_account" {
			_, _ = fmt.Fprint(w, v4AuthJSON(srv.URL, nil, nil, ""))
			return
		}
		_, _ = fmt.Fprintf(w, `{"files":[%s]}`, entry)
	}))
	defer srv.Close()
	b, err := AuthorizeAccount(context.Background(), "account-id", "application-key", SetAPIBase(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	bucket := &Bucket{ID: "bid", b2: b}
	ctx := context.Background()

	names, _, err := bucket.ListFileNames(ctx, 1, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	versions, _, _, err := bucket.ListFileVersions(ctx, 1, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	unfinished, _, err := bucket.ListUnfinishedLargeFiles(ctx, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	for name, files := range map[string][]*File{"ListFileNames": names, "ListFileVersions": versions, "ListUnfinishedLargeFiles": unfinished} {
		if len(files) != 1 {
			t.Fatalf("%s returned %d files, want 1", name, len(files))
		}
		info := files[0].Info
		if info.Retention == nil || *info.Retention != (FileRetention{Mode: "compliance", RetainUntilTimestamp: 1735689600000}) || info.LegalHold != "on" {
			t.Errorf("%s: lock = (%#v, %q), want compliance through 1735689600000 and on", name, info.Retention, info.LegalHold)
		}
	}
}
