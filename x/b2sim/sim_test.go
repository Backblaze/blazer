package b2sim

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Backblaze/blazer/b2"
)

type recordedRequest struct {
	method, path, authorization, fileName, contentSHA string
	body                                              []byte
}

type recordingTransport struct {
	sim      *Simulator
	mu       sync.Mutex
	requests []recordedRequest
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	r.mu.Lock()
	r.requests = append(r.requests, recordedRequest{method: req.Method, path: req.URL.Path, authorization: req.Header.Get("Authorization"), fileName: req.Header.Get("X-Bz-File-Name"), contentSHA: req.Header.Get("X-Bz-Content-Sha1"), body: body})
	r.mu.Unlock()
	return r.sim.RoundTrip(req)
}

func (r *recordingTransport) snapshot() []recordedRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedRequest(nil), r.requests...)
}

func responseJSON(t *testing.T, response *http.Response) json.RawMessage {
	t.Helper()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, body)
	}
	return body
}

func requireJSONFields(t *testing.T, body json.RawMessage, names ...string) map[string]json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if _, ok := fields[name]; !ok {
			t.Errorf("response omitted documented field %q: %s", name, body)
		}
	}
	return fields
}

func TestPublicClientSendsB2WireRequests(t *testing.T) {
	ctx := context.Background()
	recorder := &recordingTransport{sim: New()}
	client, err := b2.NewClient(ctx, "id", "key", b2.Transport(recorder))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "wire", nil)
	if err != nil {
		t.Fatal(err)
	}
	object, err := writeObject(ctx, bucket, "object.txt", []byte("content"), 0)
	if err != nil {
		t.Fatal(err)
	}
	got, err := readObject(ctx, object, 0, -1)
	if err != nil || string(got) != "content" {
		t.Fatalf("read = %q, %v", got, err)
	}
	want := map[string]bool{"GET /b2api/v4/b2_authorize_account": false, "POST /b2api/v4/b2_create_bucket": false, "POST /b2api/v4/b2_get_upload_url": false, "POST /upload/file/": false, "GET /file/wire/object.txt": false}
	for _, request := range recorder.snapshot() {
		path := request.path
		if strings.HasPrefix(path, "/upload/file/") {
			path = "/upload/file/"
		}
		key := request.method + " " + path
		if _, ok := want[key]; !ok {
			continue
		}
		want[key] = true
		if strings.HasSuffix(key, "b2_authorize_account") {
			if !strings.HasPrefix(request.authorization, "Basic ") {
				t.Errorf("authorize was not Basic-authenticated")
			}
		} else if request.authorization != "b2sim-token" {
			t.Errorf("%s authorization = %q", key, request.authorization)
		}
		if strings.HasSuffix(key, "b2_create_bucket") && !bytes.Contains(request.body, []byte(`"bucketName":"wire"`)) {
			t.Errorf("create body omitted bucketName: %q", request.body)
		}
		if path == "/upload/file/" {
			sha := sha1.Sum(request.body)
			if request.fileName != "object.txt" || request.authorization != "b2sim-token" || request.contentSHA != hex.EncodeToString(sha[:]) {
				t.Errorf("upload wire fields missing or mismatched")
			}
		}
	}
	for request, seen := range want {
		if !seen {
			t.Errorf("missing actual request %s", request)
		}
	}
}

func writeObject(ctx context.Context, bucket *b2.Bucket, name string, data []byte, chunkSize int) (*b2.Object, error) {
	object := bucket.Object(name)
	w := object.NewWriter(ctx)
	if chunkSize != 0 {
		w.ChunkSize = chunkSize
		w.ConcurrentUploads = 3
	}
	if n, err := w.Write(data); err != nil || n != len(data) {
		return nil, fmt.Errorf("write %s: n=%d err=%v", name, n, err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close %s: %w", name, err)
	}
	return object, nil
}

func readObject(ctx context.Context, object *b2.Object, offset, length int64) ([]byte, error) {
	r := object.NewRangeReader(ctx, offset, length)
	data, readErr := io.ReadAll(r)
	closeErr := r.Close()
	if readErr != nil {
		return nil, readErr
	}
	return data, closeErr
}

func TestGoldenPath(t *testing.T) {
	ctx := context.Background()
	sim := New()
	client, err := b2.NewClient(ctx, "test-id", "test-key", b2.Transport(sim))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "golden", &b2.BucketAttrs{
		Type: b2.Private, Info: map[string]string{"purpose": "offline"},
		FileLockEnabled: true, DefaultServerSideEncryption: b2.SSEB2WithAES256(),
	})
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := bucket.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if attrs.Info["purpose"] != "offline" || !attrs.FileLockEnabled || attrs.DefaultServerSideEncryption == nil || attrs.DefaultServerSideEncryption.Mode != "SSE-B2" {
		t.Fatalf("bucket attributes did not round-trip: %+v", attrs)
	}
	listedBuckets, err := client.ListBuckets(ctx)
	if err != nil || len(listedBuckets) != 1 || listedBuckets[0].ID() != bucket.ID() {
		t.Fatalf("list buckets: count=%d err=%v", len(listedBuckets), err)
	}
	small := []byte("simulated simple upload")
	smallObject, err := writeObject(ctx, bucket, "small.txt", small, 0)
	if err != nil {
		t.Fatal(err)
	}
	got, err := readObject(ctx, smallObject, 0, -1)
	if err != nil || !bytes.Equal(got, small) {
		t.Fatalf("simple download: bytes=%q err=%v", got, err)
	}
	got, err = readObject(ctx, smallObject, 5, 6)
	if err != nil || !bytes.Equal(got, small[5:11]) {
		t.Fatalf("range download: bytes=%q err=%v", got, err)
	}
	// b2_get_file_info documents these response names:
	// https://www.backblaze.com/apidocs/b2-get-file-info
	info := responseJSON(t, postSimulatorJSON(t, sim, "b2_get_file_info", map[string]any{"fileId": smallObject.ID()}))
	infoFields := requireJSONFields(t, info, "accountId", "action", "bucketId", "contentLength", "contentSha1", "contentType", "fileId", "fileInfo", "fileName", "fileRetention", "legalHold", "serverSideEncryption", "uploadTimestamp")
	sse := requireJSONFields(t, infoFields["serverSideEncryption"], "algorithm", "mode")
	if string(sse["mode"]) != `"SSE-B2"` && string(sse["mode"]) != "null" {
		t.Errorf("server-side encryption mode = %s", sse["mode"])
	}
	retention := requireJSONFields(t, infoFields["fileRetention"], "isClientAuthorizedToRead", "value")
	requireJSONFields(t, retention["value"], "mode", "retainUntilTimestamp")
	requireJSONFields(t, infoFields["legalHold"], "isClientAuthorizedToRead", "value")
	large := bytes.Repeat([]byte("multipart!"), 1_100_001) // over two 5 MB parts
	largeObject, err := writeObject(ctx, bucket, "large.txt", large, 5_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if calls := sim.Calls(); calls["b2_start_large_file"] != 1 || calls["b2_upload_part"] < 2 || calls["b2_finish_large_file"] != 1 {
		t.Fatalf("large upload did not use multipart wire path: %v", calls)
	}
	got, err = readObject(ctx, largeObject, 0, -1)
	if err != nil || !bytes.Equal(got, large) {
		t.Fatalf("multipart download: bytes=%d err=%v", len(got), err)
	}
	iter := bucket.List(ctx)
	var names []string
	for iter.Next() {
		names = append(names, iter.Object().Name())
	}
	if err := iter.Err(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"large.txt", "small.txt"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("listed names = %q, want %q", names, want)
	}
	for _, object := range []*b2.Object{smallObject, largeObject} {
		if err := object.Delete(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := bucket.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if got := sim.Buckets(); len(got) != 0 {
		t.Fatalf("buckets after delete = %q", got)
	}
}

func TestSeedAndConcurrentUploads(t *testing.T) {
	ctx := context.Background()
	sim := New()
	id := sim.Seed("seeded")
	if sim.Seed("seeded") != id || !reflect.DeepEqual(sim.Buckets(), []string{"seeded"}) {
		t.Fatal("seed was not idempotent")
	}
	client, err := b2.NewClient(ctx, "id", "key", b2.Transport(sim))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.Bucket(ctx, "seeded")
	if err != nil || bucket.ID() != id {
		t.Fatalf("seeded bucket: %v", err)
	}
	const count = 8
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("concurrent-%02d.txt", i)
			payload := []byte(name)
			object, err := writeObject(ctx, bucket, name, payload, 0)
			if err == nil {
				var got []byte
				got, err = readObject(ctx, object, 0, -1)
				if err == nil && !bytes.Equal(got, payload) {
					err = fmt.Errorf("%s: download mismatch", name)
				}
			}
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	iter := bucket.List(ctx)
	var objects []*b2.Object
	for iter.Next() {
		objects = append(objects, iter.Object())
	}
	if err := iter.Err(); err != nil {
		t.Fatal(err)
	}
	if len(objects) != count {
		t.Fatalf("listed %d objects, want %d", len(objects), count)
	}
	for _, object := range objects {
		if err := object.Delete(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := bucket.Delete(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestEscapedNameAndVersionPagination(t *testing.T) {
	ctx := context.Background()
	sim := New()
	client, err := b2.NewClient(ctx, "id", "key", b2.Transport(sim))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "versions", nil)
	if err != nil {
		t.Fatal(err)
	}
	const name = "folder/100% space+name.txt"
	first, err := writeObject(ctx, bucket, name, []byte("first"), 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := writeObject(ctx, bucket, name, []byte("second"), 0)
	if err != nil {
		t.Fatal(err)
	}
	got, err := readObject(ctx, second, 0, -1)
	if err != nil || string(got) != "second" {
		t.Fatalf("escaped-name download = %q, %v", got, err)
	}
	infoResp := postSimulatorJSON(t, sim, "b2_get_file_info", map[string]any{"fileId": first.ID()})
	var historical struct {
		FileID     string `json:"fileId"`
		FileName   string `json:"fileName"`
		ContentSHA string `json:"contentSha1"`
	}
	if err := json.NewDecoder(infoResp.Body).Decode(&historical); err != nil {
		t.Fatal(err)
	}
	if err := infoResp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if historical.FileID != first.ID() || historical.FileName != name || historical.ContentSHA != digest([]byte("first")) {
		t.Fatalf("historical by-ID metadata = %+v, want first version", historical)
	}
	iter := bucket.List(ctx, b2.ListHidden(), b2.ListPageSize(1))
	var ids []string
	for iter.Next() {
		ids = append(ids, iter.Object().ID())
	}
	if err := iter.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []string{second.ID(), first.ID()}) {
		t.Fatalf("paginated versions = %q, want newest then oldest", ids)
	}
	if err := first.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if err := second.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if err := bucket.Delete(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	ctx := context.Background()
	sim := New()
	client, err := b2.NewClient(ctx, "id", "key", b2.Transport(sim))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "settings", &b2.BucketAttrs{
		Type: b2.Private, FileLockEnabled: true,
		DefaultServerSideEncryption: b2.SSEB2WithAES256(),
		LifecycleRules:              []b2.LifecycleRule{{Prefix: "old/", DaysNewUntilHidden: 2}},
		CORSRules:                   []b2.CORSRule{{Name: "test", AllowedOrigins: []string{"https://example.test"}, AllowedOperations: []string{"b2_download_file_by_name"}, MaxAgeSeconds: 60}},
	})
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := bucket.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !attrs.FileLockEnabled || attrs.DefaultServerSideEncryption == nil || attrs.DefaultServerSideEncryption.Algorithm != "AES256" {
		t.Fatalf("file lock/SSE not returned: %+v", attrs)
	}
	if len(attrs.LifecycleRules) != 1 || attrs.LifecycleRules[0].Prefix != "old/" || len(attrs.CORSRules) != 1 || attrs.CORSRules[0].Name != "test" {
		t.Fatalf("lifecycle/CORS not returned: %+v", attrs)
	}
	config := &b2.ReplicationConfiguration{AsReplicationSource: b2.AsReplicationSource{SourceApplicationKeyID: "key-id", ReplicationRules: []b2.ReplicationRules{{ReplicationRuleName: "rule", DestinationBucketID: "dest-id", Priority: 1, IsEnabled: true}}}}
	if err := bucket.Update(ctx, &b2.BucketAttrs{ReplicationConfig: config, DefaultRetention: &b2.Retention{Mode: "governance", Period: &b2.RetentionPeriod{Duration: 1, Unit: "days"}}}); err != nil {
		t.Fatal(err)
	}
	attrs, err = bucket.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if attrs.ReplicationConfig == nil || attrs.ReplicationConfig.AsReplicationSource.SourceApplicationKeyID != "key-id" || len(attrs.ReplicationConfig.AsReplicationSource.ReplicationRules) != 1 {
		t.Fatalf("replication not returned: %+v", attrs.ReplicationConfig)
	}
	if attrs.DefaultRetention == nil || attrs.DefaultRetention.Mode != "governance" || attrs.DefaultRetention.Period == nil || attrs.DefaultRetention.Period.Duration != 1 {
		t.Fatalf("retention not returned: %+v", attrs.DefaultRetention)
	}
	// b2_list_buckets documents these bucket response names:
	// https://www.backblaze.com/apidocs/b2-list-buckets
	var listed struct {
		Buckets []json.RawMessage `json:"buckets"`
	}
	if err := json.Unmarshal(responseJSON(t, postSimulatorJSON(t, sim, "b2_list_buckets", map[string]any{"accountId": "b2sim-account", "bucketId": bucket.ID()})), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Buckets) != 1 {
		t.Fatalf("listed buckets = %d, want 1", len(listed.Buckets))
	}
	bucketFields := requireJSONFields(t, listed.Buckets[0], "bucketId", "bucketName", "bucketType", "bucketInfo", "corsRules", "defaultServerSideEncryption", "fileLockConfiguration", "lifecycleRules", "replicationConfiguration", "revision")
	sseFields := requireJSONFields(t, bucketFields["defaultServerSideEncryption"], "isClientAuthorizedToRead", "value")
	fileLockFields := requireJSONFields(t, bucketFields["fileLockConfiguration"], "isClientAuthorizedToRead", "value")
	replicationFields := requireJSONFields(t, bucketFields["replicationConfiguration"], "isClientAuthorizedToRead", "value")
	requireJSONFields(t, sseFields["value"], "algorithm", "mode")
	fileLockValue := requireJSONFields(t, fileLockFields["value"], "defaultRetention", "isFileLockEnabled")
	retentionFields := requireJSONFields(t, fileLockValue["defaultRetention"], "mode", "period")
	requireJSONFields(t, retentionFields["period"], "duration", "unit")
	replicationValue := requireJSONFields(t, replicationFields["value"], "asReplicationSource")
	source := requireJSONFields(t, replicationValue["asReplicationSource"], "replicationRules", "sourceApplicationKeyId")
	var replicationRules []json.RawMessage
	if err := json.Unmarshal(source["replicationRules"], &replicationRules); err != nil {
		t.Fatal(err)
	}
	if len(replicationRules) != 1 {
		t.Fatalf("replication rules = %d, want 1", len(replicationRules))
	}
	requireJSONFields(t, replicationRules[0], "destinationBucketId", "isEnabled", "priority", "replicationRuleName")
	var cors, rules []json.RawMessage
	if err := json.Unmarshal(bucketFields["corsRules"], &cors); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(bucketFields["lifecycleRules"], &rules); err != nil {
		t.Fatal(err)
	}
	if len(cors) != 1 || len(rules) != 1 {
		t.Fatalf("CORS/lifecycle response lengths = %d/%d, want 1/1", len(cors), len(rules))
	}
	requireJSONFields(t, cors[0], "allowedOperations", "allowedOrigins", "corsRuleName", "maxAgeSeconds")
	requireJSONFields(t, rules[0], "daysFromUploadingToHiding", "fileNamePrefix")
	object := bucket.Object("locked.txt")
	until := time.Unix(2_000_000_000, 0)
	w := object.NewWriter(ctx, b2.WithFileRetention(&b2.FileRetention{Mode: b2.RetentionGovernance, RetainUntil: until}), b2.WithLegalHold(b2.LegalHoldOn))
	if _, err := w.Write([]byte("lock data")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	fileAttrs, err := object.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fileAttrs.Retention == nil || fileAttrs.Retention.Mode != b2.RetentionGovernance || !fileAttrs.Retention.RetainUntil.Equal(until) || fileAttrs.LegalHold != b2.LegalHoldOn {
		t.Fatalf("file lock metadata not returned: %+v", fileAttrs)
	}
}

func TestRangeBeyond32BitAndClippedEnd(t *testing.T) {
	sim := New()
	ctx := context.Background()
	client, err := b2.NewClient(ctx, "id", "key", b2.Transport(sim))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "range", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeObject(ctx, bucket, "tiny", []byte("small"), 0); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, value string
		status      int
		body        string
	}{
		{"start above MaxInt32", fmt.Sprintf("bytes=%d-", int64(math.MaxInt32)+1), http.StatusRequestedRangeNotSatisfiable, ""},
		{"start wraps 32-bit", "bytes=4294967296-", http.StatusRequestedRangeNotSatisfiable, ""},
		{"end above MaxInt32", fmt.Sprintf("bytes=0-%d", int64(math.MaxInt32)+1), http.StatusPartialContent, "small"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/file/range/tiny", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "b2sim-token")
			req.Header.Set("Range", tc.value)
			resp, err := sim.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Error(err)
				}
			}()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tc.status {
				t.Fatalf("status=%d body=%q, want %d", resp.StatusCode, body, tc.status)
			}
			if tc.body != "" && string(body) != tc.body {
				t.Fatalf("body=%q, want %q", body, tc.body)
			}
		})
	}
}

func TestBucketUpdateRejectsStaleRevision(t *testing.T) {
	ctx := context.Background()
	sim := New()
	client, err := b2.NewClient(ctx, "id", "key", b2.Transport(sim))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.NewBucket(ctx, "revision", nil); err != nil {
		t.Fatal(err)
	}
	first, err := client.Bucket(ctx, "revision")
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.Bucket(ctx, "revision")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Update(ctx, &b2.BucketAttrs{Info: map[string]string{"owner": "first"}}); err != nil {
		t.Fatal(err)
	}
	if err := second.Update(ctx, &b2.BucketAttrs{Info: map[string]string{"owner": "second"}}); !b2.IsUpdateConflict(err) {
		t.Fatalf("stale update error = %v, want IsUpdateConflict", err)
	}
	attrs, err := first.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if attrs.Info["owner"] != "first" {
		t.Fatalf("stale update overwrote bucket info: %v", attrs.Info)
	}
}

func TestFinishRejectsMissingPartWithoutLosingUpload(t *testing.T) {
	sim := New()
	id := sim.Seed("missing-part")
	started := postSimulatorJSON(t, sim, "b2_start_large_file", map[string]any{"bucketId": id, "fileName": "large", "contentType": "application/octet-stream"})
	var body struct {
		FileID string `json:"fileId"`
	}
	if err := json.NewDecoder(started.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if err := started.Body.Close(); err != nil {
		t.Fatal(err)
	}
	part := []byte("second")
	req, err := http.NewRequest(http.MethodPost, origin+"/upload/part/"+body.FileID, bytes.NewReader(part))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "b2sim-token")
	req.Header.Set("X-Bz-Part-Number", "2")
	req.Header.Set("X-Bz-Content-Sha1", digest(part))
	resp, err := sim.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("part upload status = %d", resp.StatusCode)
	}
	finish := postSimulatorJSON(t, sim, "b2_finish_large_file", map[string]any{"fileId": body.FileID, "partSha1Array": []string{""}})
	if err := finish.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if finish.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing part finish status = %d, want 400", finish.StatusCode)
	}
	if sim.large[body.FileID] == nil || len(sim.buckets[id].versions) != 0 {
		t.Fatal("failed finish removed unfinished file or stored a version")
	}
}

func TestMultipartLimitsLeaveUploadUnfinished(t *testing.T) {
	for _, tc := range []struct {
		name  string
		parts [][]byte
	}{
		{name: "one part", parts: [][]byte{bytes.Repeat([]byte("a"), 5_000_000)}},
		{name: "short non-final part", parts: [][]byte{[]byte("short"), []byte("last")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sim := New()
			bucketID := sim.Seed("multipart-limit")
			start := postSimulatorJSON(t, sim, "b2_start_large_file", map[string]any{"bucketId": bucketID, "fileName": "large", "contentType": "application/octet-stream"})
			var started struct {
				FileID string `json:"fileId"`
			}
			if err := json.NewDecoder(start.Body).Decode(&started); err != nil {
				t.Fatal(err)
			}
			if err := start.Body.Close(); err != nil {
				t.Fatal(err)
			}
			var hashes []string
			for i, part := range tc.parts {
				req, err := http.NewRequest(http.MethodPost, origin+"/upload/part/"+started.FileID, bytes.NewReader(part))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Authorization", "b2sim-token")
				req.Header.Set("X-Bz-Part-Number", fmt.Sprint(i+1))
				req.Header.Set("X-Bz-Content-Sha1", digest(part))
				resp, err := sim.RoundTrip(req)
				if err != nil {
					t.Fatal(err)
				}
				if err := resp.Body.Close(); err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != 200 {
					t.Fatalf("upload part %d status = %d", i+1, resp.StatusCode)
				}
				hashes = append(hashes, digest(part))
			}
			finish := postSimulatorJSON(t, sim, "b2_finish_large_file", map[string]any{"fileId": started.FileID, "partSha1Array": hashes})
			var failureBody struct {
				Code string `json:"code"`
			}
			if err := json.NewDecoder(finish.Body).Decode(&failureBody); err != nil {
				t.Fatal(err)
			}
			if err := finish.Body.Close(); err != nil {
				t.Fatal(err)
			}
			if finish.StatusCode != 400 || failureBody.Code != "bad_request" || sim.large[started.FileID] == nil {
				t.Fatalf("finish status=%d code=%q unfinished=%t, want 400 bad_request and retained upload", finish.StatusCode, failureBody.Code, sim.large[started.FileID] != nil)
			}
		})
	}
}

func TestAuthorizeAdvertisesB2PartSizes(t *testing.T) {
	sim := New()
	req, err := http.NewRequest(http.MethodGet, origin+"/b2api/v4/b2_authorize_account", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth("id", "key")
	resp, err := sim.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		APIInfo struct {
			StorageAPI struct {
				AbsoluteMinimumPartSize int `json:"absoluteMinimumPartSize"`
				RecommendedPartSize     int `json:"recommendedPartSize"`
			} `json:"storageApi"`
		} `json:"apiInfo"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	got := body.APIInfo.StorageAPI
	if got.AbsoluteMinimumPartSize != 5_000_000 || got.RecommendedPartSize != 100_000_000 {
		t.Fatalf("part sizes = minimum %d recommended %d, want 5000000 and 100000000", got.AbsoluteMinimumPartSize, got.RecommendedPartSize)
	}
}

func TestUploadPartRejectsOutOfRangeNumbers(t *testing.T) {
	sim := New()
	bucketID := sim.Seed("part-number")
	start := postSimulatorJSON(t, sim, "b2_start_large_file", map[string]any{"bucketId": bucketID, "fileName": "large", "contentType": "application/octet-stream"})
	var started struct {
		FileID string `json:"fileId"`
	}
	if err := json.NewDecoder(start.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	if err := start.Body.Close(); err != nil {
		t.Fatal(err)
	}
	for _, number := range []int{0, 10001} {
		part := []byte("part")
		req, err := http.NewRequest(http.MethodPost, origin+"/upload/part/"+started.FileID, bytes.NewReader(part))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "b2sim-token")
		req.Header.Set("X-Bz-Part-Number", fmt.Sprint(number))
		req.Header.Set("X-Bz-Content-Sha1", digest(part))
		resp, err := sim.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		var failureBody struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&failureBody); err != nil {
			t.Fatal(err)
		}
		if err := resp.Body.Close(); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 400 || failureBody.Code != "bad_request" || len(sim.large[started.FileID].parts) != 0 {
			t.Fatalf("part %d status=%d code=%q stored=%d, want 400 bad_request and no part", number, resp.StatusCode, failureBody.Code, len(sim.large[started.FileID].parts))
		}
	}
}

func TestDownloadFileNameHeaderIsEncoded(t *testing.T) {
	ctx := context.Background()
	sim := New()
	client, err := b2.NewClient(ctx, "id", "key", b2.Transport(sim))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "encoded-name", nil)
	if err != nil {
		t.Fatal(err)
	}
	name := "folder/100% space+name.txt"
	if _, err := writeObject(ctx, bucket, name, []byte("data"), 0); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodGet, origin+"/file/encoded-name/folder/100%25%20space%2Bname.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "b2sim-token")
	resp, err := sim.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download status = %d", resp.StatusCode)
	}
	encoded := resp.Header.Get("X-Bz-File-Name")
	decoded, err := url.QueryUnescape(encoded)
	if err != nil || decoded != name || encoded == name {
		t.Fatalf("file name header = %q, decoded = %q, err = %v", encoded, decoded, err)
	}
}

func TestUnsupportedOperationFailsLoudly(t *testing.T) {
	sim := New()
	for _, tc := range []struct{ method, operation string }{
		{http.MethodPost, "b2_copy_file"},
		{http.MethodGet, "b2_download_file_by_id"},
	} {
		t.Run(tc.operation, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, origin+"/b2api/v4/"+tc.operation, strings.NewReader(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "b2sim-token")
			resp, err := sim.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Error(err)
				}
			}()
			var failure struct{ Code, Message string }
			if err := json.NewDecoder(resp.Body).Decode(&failure); err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusNotImplemented || failure.Code != "unsupported_operation" || !strings.Contains(failure.Message, tc.operation) {
				t.Fatalf("unsupported response = status %d code %q message %q", resp.StatusCode, failure.Code, failure.Message)
			}
		})
	}
}

func TestFileResponseReportsBucketDefaultEncryption(t *testing.T) {
	ctx := context.Background()
	sim := New()
	client, err := b2.NewClient(ctx, "id", "key", b2.Transport(sim))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "encrypted", &b2.BucketAttrs{DefaultServerSideEncryption: b2.SSEB2WithAES256()})
	if err != nil {
		t.Fatal(err)
	}
	obj, err := writeObject(ctx, bucket, "one", []byte("content"), 0)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, origin+"/b2api/v4/b2_get_file_info", strings.NewReader(fmt.Sprintf(`{"fileId":%q}`, obj.ID())))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "b2sim-token")
	resp, err := sim.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Error(err)
		}
	}()
	var got struct {
		ServerSideEncryption struct {
			Mode      string `json:"mode"`
			Algorithm string `json:"algorithm"`
		} `json:"serverSideEncryption"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ServerSideEncryption.Mode != "SSE-B2" || got.ServerSideEncryption.Algorithm != "AES256" {
		t.Fatalf("file encryption = %s/%s, want SSE-B2/AES256", got.ServerSideEncryption.Mode, got.ServerSideEncryption.Algorithm)
	}
}

func TestConflictingUploadEncryptionHeadersFailClosed(t *testing.T) {
	sim := New()
	bucketID := sim.Seed("conflict")
	payload := []byte("content")
	sha := sha1.Sum(payload)
	req, err := http.NewRequest(http.MethodPost, origin+"/upload/file/"+bucketID, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "b2sim-token")
	req.Header.Set("X-Bz-File-Name", "conflict.txt")
	req.Header.Set("X-Bz-Content-Sha1", hex.EncodeToString(sha[:]))
	req.Header.Set("X-Bz-Server-Side-Encryption", "AES256")
	req.Header.Set("X-Bz-Server-Side-Encryption-Customer-Algorithm", "AES256")
	resp, err := sim.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Error(err)
		}
	}()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("conflicting encryption status = %d, want 400", resp.StatusCode)
	}
	if len(sim.buckets[bucketID].versions) != 0 {
		t.Fatal("conflicting encryption nevertheless stored a version")
	}
}

func postSimulatorJSON(t *testing.T, sim *Simulator, method string, body any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, origin+"/b2api/v4/"+method, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "b2sim-token")
	resp, err := sim.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestFailedMultipartPartCanBeCancelledWithoutRetainingSSECKey(t *testing.T) {
	sim := New()
	bucketID := sim.Seed("multipart-error")
	const secret = "a-customer-key-that-must-not-be-retained"
	resp := postSimulatorJSON(t, sim, "b2_start_large_file", map[string]any{
		"bucketId": bucketID, "fileName": "failure.bin", "contentType": "application/octet-stream",
		"serverSideEncryption": map[string]any{"mode": "SSE-C", "algorithm": "AES256", "customerKey": secret, "customerKeyMd5": "digest"},
	})
	var started struct {
		FileID string `json:"fileId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || started.FileID == "" {
		t.Fatalf("start = %d, ID=%q", resp.StatusCode, started.FileID)
	}
	stored, err := json.Marshal(sim.large[started.FileID].file.response())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte(secret)) || bytes.Contains(stored, []byte("digest")) {
		t.Fatal("SSE-C key material retained or echoed in file response")
	}
	if !bytes.Contains(stored, []byte(`"mode":"SSE-C"`)) {
		t.Fatalf("SSE-C mode absent from response: %q", stored)
	}
	partReq, err := http.NewRequest(http.MethodPost, origin+"/upload/part/"+started.FileID, strings.NewReader("bad part"))
	if err != nil {
		t.Fatal(err)
	}
	partReq.Header.Set("Authorization", "b2sim-token")
	partReq.Header.Set("X-Bz-Part-Number", "1")
	partReq.Header.Set("X-Bz-Content-Sha1", "wrong")
	partResp, err := sim.RoundTrip(partReq)
	if err != nil {
		t.Fatal(err)
	}
	if err := partResp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if partResp.StatusCode != http.StatusBadRequest || len(sim.large[started.FileID].parts) != 0 {
		t.Fatalf("bad part status=%d or part was stored", partResp.StatusCode)
	}
	cancelResp := postSimulatorJSON(t, sim, "b2_cancel_large_file", map[string]any{"fileId": started.FileID})
	if err := cancelResp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if cancelResp.StatusCode != 200 || sim.large[started.FileID] != nil {
		t.Fatalf("cancel status=%d, unfinished upload remains", cancelResp.StatusCode)
	}
}

func TestTerminalListCursorAndEmptyLockJSON(t *testing.T) {
	ctx := context.Background()
	sim := New()
	client, err := b2.NewClient(ctx, "id", "key", b2.Transport(sim))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "shape", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeObject(ctx, bucket, "one", []byte("x"), 0); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"b2_list_file_names", "b2_list_file_versions"} {
		resp := postSimulatorJSON(t, sim, method, map[string]any{"bucketId": bucket.ID()})
		var body map[string]json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if err := resp.Body.Close(); err != nil {
			t.Fatal(err)
		}
		if string(body["nextFileName"]) != "null" {
			t.Errorf("%s nextFileName = %s, want null", method, body["nextFileName"])
		}
		if method == "b2_list_file_versions" && string(body["nextFileId"]) != "null" {
			t.Errorf("%s nextFileId = %s, want null", method, body["nextFileId"])
		}
		var files []map[string]json.RawMessage
		if err := json.Unmarshal(body["files"], &files); err != nil {
			t.Fatal(err)
		}
		if len(files) != 1 {
			t.Fatalf("%s files = %d, want 1", method, len(files))
		}
		var retention struct {
			Value map[string]json.RawMessage `json:"value"`
		}
		if err := json.Unmarshal(files[0]["fileRetention"], &retention); err != nil {
			t.Fatal(err)
		}
		if string(retention.Value["mode"]) != "null" || string(retention.Value["retainUntilTimestamp"]) != "null" {
			t.Errorf("%s empty retention = %s", method, files[0]["fileRetention"])
		}
		var hold struct {
			Value *string `json:"value"`
		}
		if err := json.Unmarshal(files[0]["legalHold"], &hold); err != nil {
			t.Fatal(err)
		}
		if hold.Value != nil {
			t.Errorf("%s empty legalHold = %s", method, files[0]["legalHold"])
		}
	}
}

func TestCreateBucketReturnsReplicationConfiguration(t *testing.T) {
	sim := New()
	rule := map[string]any{"asReplicationSource": map[string]any{"sourceApplicationKeyId": "source-key", "replicationRules": []any{map[string]any{"replicationRuleName": "rule", "destinationBucketId": "destination"}}}}
	resp := postSimulatorJSON(t, sim, "b2_create_bucket", map[string]any{"accountId": "b2sim-account", "bucketName": "replicated", "bucketType": "allPrivate", "replicationConfiguration": rule})
	var body struct {
		ReplicationConfiguration struct {
			Value map[string]any `json:"value"`
		} `json:"replicationConfiguration"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || !reflect.DeepEqual(body.ReplicationConfiguration.Value, rule) {
		t.Fatalf("create replication readback = status %d value %#v", resp.StatusCode, body.ReplicationConfiguration.Value)
	}
}

type seekableSource struct {
	io.ReadSeeker
	io.ReaderAt
}

type blockingBody struct {
	entered chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (b *blockingBody) Read([]byte) (int, error) {
	close(b.entered)
	<-b.closed
	return 0, io.ErrClosedPipe
}
func (b *blockingBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

func TestRoundTripCancelsBlockedBodyRead(t *testing.T) {
	sim := New()
	body := &blockingBody{entered: make(chan struct{}), closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deadline, cancelDeadline := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelDeadline()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+"/b2api/v4/b2_create_bucket", body)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := sim.RoundTrip(req); result <- err }()
	select {
	case <-body.entered:
	case <-deadline.Done():
		t.Fatal("body read did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("RoundTrip error = %v, want cancellation", err)
		}
	case <-deadline.Done():
		_ = body.Close()
		t.Fatal("RoundTrip remained blocked after cancellation")
	}
	select {
	case <-body.closed:
	case <-deadline.Done():
		t.Fatal("body reader did not exit")
	}
}

func TestReadFromMultipartTrailingSHA1(t *testing.T) {
	ctx := context.Background()
	sim := New()
	recorder := &recordingTransport{sim: sim}
	client, err := b2.NewClient(ctx, "id", "key", b2.Transport(recorder))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "read-from", nil)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("read-from multipart!"), 750000) // 15 MB
	obj := bucket.Object("large.bin")
	w := obj.NewWriter(ctx)
	w.ChunkSize = 5_000_000
	source := bytes.NewReader(payload)
	n, err := io.Copy(w, seekableSource{source, source})
	if err != nil || n != int64(len(payload)) {
		t.Fatalf("ReadFrom copied %d: %v", n, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := readObject(ctx, obj, 0, -1)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("ReadFrom round-trip len=%d: %v", len(got), err)
	}
	if sim.Calls()["b2_upload_part"] < 2 {
		t.Fatal("multipart part path was not exercised")
	}
	partRequests := 0
	for _, request := range recorder.snapshot() {
		if !strings.HasPrefix(request.path, "/upload/part/") {
			continue
		}
		partRequests++
		if request.contentSHA != "hex_digits_at_end" {
			t.Errorf("part SHA1 header = %q, want hex_digits_at_end", request.contentSHA)
			continue
		}
		if len(request.body) < sha1.Size*2 {
			t.Errorf("part body = %d bytes, want data plus SHA1 trailer", len(request.body))
			continue
		}
		trailer := request.body[len(request.body)-sha1.Size*2:]
		if got, want := string(trailer), digest(request.body[:len(request.body)-sha1.Size*2]); got != want {
			t.Errorf("part SHA1 trailer = %q, want %q", got, want)
		}
	}
	if partRequests < 2 {
		t.Fatalf("multipart requests = %d, want at least 2", partRequests)
	}
}

func TestListBucketsFiltersTypes(t *testing.T) {
	ctx := context.Background()
	sim := New()
	client, err := b2.NewClient(ctx, "id", "key", b2.Transport(sim))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.NewBucket(ctx, "private", &b2.BucketAttrs{Type: b2.Private}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.NewBucket(ctx, "public", &b2.BucketAttrs{Type: b2.Public}); err != nil {
		t.Fatal(err)
	}
	buckets, err := client.ListBuckets(ctx, "allPublic")
	if err != nil || len(buckets) != 1 || buckets[0].Name() != "public" {
		t.Fatalf("public buckets = %v, %v", buckets, err)
	}
}

func TestDeleteBucketRejectsUnfinishedLargeFile(t *testing.T) {
	sim := New()
	bucketID := sim.Seed("unfinished")
	start := postSimulatorJSON(t, sim, "b2_start_large_file", map[string]any{"bucketId": bucketID, "fileName": "large.bin", "contentType": "application/octet-stream"})
	var body struct {
		FileID string `json:"fileId"`
	}
	if err := json.NewDecoder(start.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if err := start.Body.Close(); err != nil {
		t.Fatal(err)
	}
	resp := postSimulatorJSON(t, sim, "b2_delete_bucket", map[string]any{"bucketId": bucketID})
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("delete with unfinished upload = %d, want 400", resp.StatusCode)
	}
	resp = postSimulatorJSON(t, sim, "b2_cancel_large_file", map[string]any{"fileId": body.FileID})
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	resp = postSimulatorJSON(t, sim, "b2_delete_bucket", map[string]any{"bucketId": bucketID})
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete after cancel = %d, want 200", resp.StatusCode)
	}
}

func TestUploadInfoPreservesLastModified(t *testing.T) {
	ctx := context.Background()
	sim := New()
	client, err := b2.NewClient(ctx, "id", "key", b2.Transport(sim))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "metadata", nil)
	if err != nil {
		t.Fatal(err)
	}
	mtime := time.UnixMilli(1_700_000_000_123)
	w := bucket.Object("file").NewWriter(ctx, b2.WithAttrsOption(&b2.Attrs{LastModified: mtime}))
	if _, err := w.Write([]byte("content")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	attrs, err := bucket.Object("file").Attrs(ctx)
	if err != nil || !attrs.LastModified.Equal(mtime) {
		t.Fatalf("LastModified = %v, %v; want %v", attrs.LastModified, err, mtime)
	}
}

func TestDelimiterGroupsAcrossPages(t *testing.T) {
	ctx := context.Background()
	sim := New()
	client, err := b2.NewClient(ctx, "id", "key", b2.Transport(sim))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "folders", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a/one", "a/two", "b/one", "b/two", "root.txt", "z.txt"} {
		if _, err := writeObject(ctx, bucket, name, []byte(name), 0); err != nil {
			t.Fatal(err)
		}
	}
	iter := bucket.List(ctx, b2.ListDelimiter("/"), b2.ListPageSize(1))
	var names []string
	for iter.Next() {
		names = append(names, iter.Object().Name())
	}
	if err := iter.Err(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"a/", "b/", "root.txt", "z.txt"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("grouped names = %v, want %v", names, want)
	}
	iter = bucket.List(ctx, b2.ListPrefix("a/"), b2.ListDelimiter("/"), b2.ListPageSize(1))
	names = nil
	for iter.Next() {
		names = append(names, iter.Object().Name())
	}
	if err := iter.Err(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"a/one", "a/two"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("prefix + delimiter names = %v, want %v", names, want)
	}
	resp := postSimulatorJSON(t, sim, "b2_list_file_names", map[string]any{"bucketId": bucket.ID(), "delimiter": "/", "maxFileCount": 1})
	var page struct {
		Files []struct {
			Name   string `json:"fileName"`
			Action string `json:"action"`
		} `json:"files"`
		Next string `json:"nextFileName"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if len(page.Files) != 1 || page.Files[0].Name != "a/" || page.Files[0].Action != "folder" || page.Next != "b/" {
		t.Fatalf("first grouped wire page = %+v", page)
	}
}

func TestFinishOrderMatchesListingAndDownload(t *testing.T) {
	ctx := context.Background()
	sim := New()
	client, err := b2.NewClient(ctx, "id", "key", b2.Transport(sim))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "order", nil)
	if err != nil {
		t.Fatal(err)
	}
	start := postSimulatorJSON(t, sim, "b2_start_large_file", map[string]any{"bucketId": bucket.ID(), "fileName": "same", "contentType": "application/octet-stream"})
	var started struct {
		FileID string `json:"fileId"`
	}
	if err := json.NewDecoder(start.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	if err := start.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := writeObject(ctx, bucket, "same", []byte("simple"), 0); err != nil {
		t.Fatal(err)
	}
	parts := [][]byte{bytes.Repeat([]byte("large"), 1_000_000), []byte("last")}
	for i, part := range parts {
		req, err := http.NewRequest(http.MethodPost, origin+"/upload/part/"+started.FileID, bytes.NewReader(part))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "b2sim-token")
		req.Header.Set("X-Bz-Part-Number", fmt.Sprint(i+1))
		req.Header.Set("X-Bz-Content-Sha1", digest(part))
		partResp, err := sim.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		if err := partResp.Body.Close(); err != nil {
			t.Fatal(err)
		}
		if partResp.StatusCode != 200 {
			t.Fatalf("part %d status = %d", i+1, partResp.StatusCode)
		}
	}
	resp := postSimulatorJSON(t, sim, "b2_finish_large_file", map[string]any{"fileId": started.FileID, "partSha1Array": []string{digest(parts[0]), digest(parts[1])}})
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("finish status = %d", resp.StatusCode)
	}
	iter := bucket.List(ctx)
	if !iter.Next() {
		t.Fatalf("list: %v", iter.Err())
	}
	if iter.Object().ID() != started.FileID {
		t.Fatalf("listed version = %s, want %s", iter.Object().ID(), started.FileID)
	}
	got, err := readObject(ctx, bucket.Object("same"), 0, -1)
	if err != nil || !bytes.Equal(got, append(parts[0], parts[1]...)) {
		t.Fatalf("downloaded %d bytes, %v; want %d", len(got), err, len(parts[0])+len(parts[1]))
	}
}

func TestDownloadInfoHeadersAreEscaped(t *testing.T) {
	ctx := context.Background()
	sim := New()
	client, err := b2.NewClient(ctx, "id", "key", b2.Transport(sim))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "info-header", nil)
	if err != nil {
		t.Fatal(err)
	}
	obj := bucket.Object("file")
	w := obj.NewWriter(ctx, b2.WithAttrsOption(&b2.Attrs{Info: map[string]string{"tag": "a+b 100%"}}))
	if _, err := w.Write([]byte("content")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := readObject(ctx, obj, 0, -1)
	if err != nil || string(got) != "content" {
		t.Fatalf("download = %q, %v", got, err)
	}
	// The header itself must be encoded so the real client's decoder recovers the exact value.
	req, err := http.NewRequest(http.MethodGet, origin+"/file/info-header/file", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "b2sim-token")
	resp, err := sim.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Error(err)
		}
	}()
	if value := resp.Header.Get("X-Bz-Info-tag"); value != "a%2Bb+100%25" {
		t.Fatalf("encoded info header = %q", value)
	}
}

func TestPartTrailingDigestRejectsInvalid(t *testing.T) {
	sim := New()
	bucketID := sim.Seed("bad-digest")
	start := postSimulatorJSON(t, sim, "b2_start_large_file", map[string]any{"bucketId": bucketID, "fileName": "large", "contentType": "application/octet-stream"})
	var body struct {
		FileID string `json:"fileId"`
	}
	if err := json.NewDecoder(start.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if err := start.Body.Close(); err != nil {
		t.Fatal(err)
	}
	for _, trailer := range []string{"not-hex", strings.Repeat("0", 40)} {
		req, err := http.NewRequest(http.MethodPost, origin+"/upload/part/"+body.FileID, bytes.NewReader(append([]byte("part"), []byte(trailer)...)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "b2sim-token")
		req.Header.Set("X-Bz-Part-Number", "1")
		req.Header.Set("X-Bz-Content-Sha1", "hex_digits_at_end")
		resp, err := sim.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		if err := resp.Body.Close(); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 400 {
			t.Errorf("invalid trailer %q status = %d, want 400", trailer, resp.StatusCode)
		}
	}
}
