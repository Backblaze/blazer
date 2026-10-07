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
	"time"

	"github.com/Backblaze/blazer/base"
)

type lockWire struct {
	srv     *httptest.Server
	path    string
	headers http.Header
	body    map[string]any
	calls   int
	info    string // response for b2_get_file_info
}

func newLockWire(t *testing.T) *lockWire {
	t.Helper()
	w := &lockWire{}
	w.srv = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		switch {
		case strings.HasSuffix(r.URL.Path, "/b2_authorize_account"):
			_, _ = fmt.Fprintf(rw, updateAuthJSON, w.srv.URL)
			return
		case strings.HasSuffix(r.URL.Path, "/b2_create_bucket"):
			_, _ = fmt.Fprint(rw, noReplicationBucketJSON)
			return
		}
		w.calls++
		w.path, w.headers, w.body = r.URL.Path, r.Header.Clone(), nil
		if strings.HasPrefix(r.URL.Path, "/b2api/") {
			w.body = map[string]any{}
			if err := json.Unmarshal(raw, &w.body); err != nil {
				t.Errorf("decode %s: %v", r.URL.Path, err)
			}
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/b2_get_upload_url"):
			_, _ = fmt.Fprintf(rw, `{"uploadUrl":"%s/upload","authorizationToken":"tok"}`, w.srv.URL)
		case strings.HasSuffix(r.URL.Path, "/b2_start_large_file"):
			_, _ = fmt.Fprint(rw, `{"fileId":"large"}`)
		case strings.HasSuffix(r.URL.Path, "/b2_get_file_info"):
			_, _ = fmt.Fprint(rw, w.info)
		case r.URL.Path == "/upload":
			_, _ = fmt.Fprint(rw, `{"fileId":"small","action":"upload"}`)
		default:
			_, _ = fmt.Fprint(rw, `{}`)
		}
	}))
	t.Cleanup(w.srv.Close)
	return w
}

func (w *lockWire) bucket(t *testing.T) *b2Bucket {
	t.Helper()
	ctx := context.Background()
	account, err := base.AuthorizeAccount(ctx, "a", "k", base.SetAPIBase(w.srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	bb, err := account.CreateBucket(ctx, "n", "allPrivate", nil, nil, nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	return &b2Bucket{b: bb}
}

func (w *lockWire) object(t *testing.T) *Object {
	t.Helper()
	root := &beRoot{b2i: &testRoot{errs: &errCont{}, bucketMap: make(map[string]map[string]string)}}
	return &Object{name: "name", f: &beFile{b2file: &b2File{b: w.bucket(t).b.File("id", "name")}, ri: root}}
}

var lockUntil = time.UnixMilli(1735689600000)

func TestUploadAndStartLargeFileCarryObjectLock(t *testing.T) {
	w := newLockWire(t)
	b, ctx := w.bucket(t), context.Background()
	rt := &FileRetention{Mode: RetentionCompliance, RetainUntil: lockUntil}

	url, err := b.getUploadURL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := url.uploadFile(ctx, strings.NewReader("x"), 1, "n", "text/plain", "sha", nil, rt, LegalHoldOn); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"X-Bz-File-Retention-Mode":                   "compliance",
		"X-Bz-File-Retention-Retain-Until-Timestamp": "1735689600000",
		"X-Bz-File-Legal-Hold":                       "on",
	} {
		if got := w.headers.Get(k); got != want {
			t.Errorf("upload %s = %q, want %q", k, got, want)
		}
	}

	if _, err := url.uploadFile(ctx, strings.NewReader("x"), 1, "n", "text/plain", "sha", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := w.headers["X-Bz-File-Retention-Mode"]; ok {
		t.Error("retention header sent without a retention")
	}

	if _, err := b.startLargeFile(ctx, "n", "text/plain", nil, rt, LegalHoldOff); err != nil {
		t.Fatal(err)
	}
	got, _ := w.body["fileRetention"].(map[string]any)
	if got["mode"] != "compliance" || got["retainUntilTimestamp"] != float64(1735689600000) || w.body["legalHold"] != "off" {
		t.Errorf("start_large_file body = %v, want compliance through 1735689600000 and legalHold off", w.body)
	}
}

func TestObjectUpdateFileRetentionReachesTheWire(t *testing.T) {
	w := newLockWire(t)
	ctx := context.Background()

	if err := w.object(t).UpdateFileRetention(ctx, &FileRetention{Mode: RetentionGovernance, RetainUntil: lockUntil}, true); err != nil {
		t.Fatal(err)
	}
	got, _ := w.body["fileRetention"].(map[string]any)
	if got["mode"] != "governance" || got["retainUntilTimestamp"] != float64(1735689600000) || w.body["bypassGovernance"] != true {
		t.Errorf("body = %v, want governance through 1735689600000 with bypassGovernance", w.body)
	}

	if err := w.object(t).UpdateFileRetention(ctx, nil, true); err != nil {
		t.Fatal(err)
	}
	cleared, ok := w.body["fileRetention"].(map[string]any)
	if !ok || len(cleared) != 2 || cleared["mode"] != nil || cleared["retainUntilTimestamp"] != nil {
		t.Errorf("fileRetention = %v, want {mode: null, retainUntilTimestamp: null}", w.body["fileRetention"])
	}

	if err := w.object(t).UpdateFileLegalHold(ctx, LegalHoldOn); err != nil {
		t.Fatal(err)
	}
	if w.body["legalHold"] != "on" {
		t.Errorf("legalHold = %v, want on", w.body["legalHold"])
	}
}

func TestObjectLockValidation(t *testing.T) {
	w := newLockWire(t)
	o := w.object(t)
	ctx := context.Background()
	before := w.calls

	for name, rt := range map[string]*FileRetention{
		"no mode":           {RetainUntil: lockUntil},
		"unknown mode":      {Mode: "strict", RetainUntil: lockUntil},
		"period-only style": {Mode: RetentionGovernance},
	} {
		if err := o.UpdateFileRetention(ctx, rt, false); err == nil {
			t.Errorf("UpdateFileRetention(%s) succeeded, want an error", name)
		}
	}
	for _, lh := range []LegalHold{"", "maybe"} {
		if err := o.UpdateFileLegalHold(ctx, lh); err == nil {
			t.Errorf("UpdateFileLegalHold(%q) succeeded, want an error", lh)
		}
	}
	if w.calls != before {
		t.Errorf("%d request(s) sent for invalid input, want none", w.calls-before)
	}
}

func TestWriterRejectsInvalidObjectLock(t *testing.T) {
	root := &testRoot{bucketMap: make(map[string]map[string]string), errs: &errCont{}}
	bucket, err := (&Client{backend: &beRoot{b2i: root}}).NewBucket(context.Background(), bucketName, &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	for name, opt := range map[string]WriterOption{
		"retention without a time": WithFileRetention(&FileRetention{Mode: RetentionGovernance}),
		"unknown legal hold":       WithLegalHold("maybe"),
	} {
		wr := bucket.Object("obj").NewWriter(context.Background(), opt)
		if _, err := wr.Write([]byte("x")); err == nil {
			t.Errorf("%s: Write succeeded, want an error", name)
		}
		_ = wr.Close()
	}
}

func TestFileInfoReportsObjectLock(t *testing.T) {
	w := newLockWire(t)
	w.info = `{"fileId":"id","fileName":"name","action":"upload",` +
		`"fileRetention":{"isClientAuthorizedToRead":true,"value":{"mode":"governance","retainUntilTimestamp":1735689600000}},` +
		`"legalHold":{"isClientAuthorizedToRead":true,"value":"on"}}`
	fi, err := (&b2File{b: w.bucket(t).b.File("id", "name")}).getFileInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rt, lh := fi.fileLock()
	if rt == nil || rt.Mode != RetentionGovernance || !rt.RetainUntil.Equal(lockUntil) || lh != LegalHoldOn {
		t.Errorf("fileLock() = (%#v, %q), want governance through %v and on", rt, lh, lockUntil)
	}
}

func TestResumeRefusesAnUnfinishedUploadWithoutTheRequestedLock(t *testing.T) {
	w := newLockWire(t)
	w.info = `{"fileId":"id","fileName":"name","action":"start",` +
		`"fileRetention":{"isClientAuthorizedToRead":true,"value":{"mode":"governance","retainUntilTimestamp":1735689600000}},` +
		`"legalHold":{"isClientAuthorizedToRead":true,"value":"off"}}`
	root := &beRoot{b2i: &testRoot{errs: &errCont{}, bucketMap: make(map[string]map[string]string)}}
	fi := &beFile{b2file: &b2File{b: w.bucket(t).b.File("id", "name")}, ri: root}
	ctx := context.Background()

	for name, tc := range map[string]struct {
		w       *Writer
		wantErr bool
	}{
		"no lock requested":    {&Writer{ctx: ctx, name: "name"}, false},
		"matching retention":   {&Writer{ctx: ctx, name: "name", retention: &FileRetention{Mode: RetentionGovernance, RetainUntil: lockUntil}}, false},
		"sub-millisecond time": {&Writer{ctx: ctx, name: "name", retention: &FileRetention{Mode: RetentionGovernance, RetainUntil: lockUntil.Add(500 * time.Microsecond)}}, false},
		"different time":       {&Writer{ctx: ctx, name: "name", retention: &FileRetention{Mode: RetentionGovernance, RetainUntil: lockUntil.Add(time.Hour)}}, true},
		"different mode":       {&Writer{ctx: ctx, name: "name", retention: &FileRetention{Mode: RetentionCompliance, RetainUntil: lockUntil}}, true},
		"legal hold not set":   {&Writer{ctx: ctx, name: "name", legalHold: LegalHoldOn}, true},
		"matching legal hold":  {&Writer{ctx: ctx, name: "name", legalHold: LegalHoldOff}, false},
	} {
		if err := tc.w.checkResumedLock(fi); (err != nil) != tc.wantErr {
			t.Errorf("%s: error = %v, want error %v", name, err, tc.wantErr)
		}
	}
}

// setErr cancels the large file when WithCancelOnError is set, but a writer
// that fails validation has no large file yet.
func TestInvalidObjectLockWithCancelOnErrorDoesNotPanic(t *testing.T) {
	root := &testRoot{bucketMap: make(map[string]map[string]string), errs: &errCont{}}
	bucket, err := (&Client{backend: &beRoot{b2i: root}}).NewBucket(context.Background(), bucketName, &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	cancelOnError := WithCancelOnError(context.Background, func(error) {})
	invalid := WithFileRetention(&FileRetention{Mode: RetentionGovernance})

	wr := bucket.Object("written").NewWriter(context.Background(), invalid, cancelOnError)
	if _, err := wr.Write([]byte("x")); err == nil {
		t.Error("Write succeeded, want an error")
	}
	if err := wr.Close(); err == nil {
		t.Error("Close after a failed Write succeeded, want an error")
	}

	empty := bucket.Object("empty").NewWriter(context.Background(), invalid, cancelOnError)
	if err := empty.Close(); err == nil {
		t.Error("Close of an empty writer succeeded, want an error")
	}
}

func TestFileRetentionMillisecondConversionPastYear2262(t *testing.T) {
	far := time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC)
	rt := &FileRetention{Mode: RetentionGovernance, RetainUntil: far}
	wire := rt.toBase()
	if want := int64(32503680000000); wire.RetainUntilTimestamp != want {
		t.Fatalf("RetainUntilTimestamp = %d, want %d", wire.RetainUntilTimestamp, want)
	}
	if back := retentionFromBase(wire); !back.RetainUntil.Equal(far) {
		t.Errorf("round trip = %v, want %v", back.RetainUntil, far)
	}
}
