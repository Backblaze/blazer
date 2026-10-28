package b2wiretest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

type recordingTB struct {
	testing.TB
	mu       sync.Mutex
	messages []string
}

func (t *recordingTB) Errorf(format string, args ...any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.messages = append(t.messages, fmt.Sprintf(format, args...))
}

func (t *recordingTB) Fatalf(format string, args ...any) { t.Errorf(format, args...) }

func (t *recordingTB) errors() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.messages...)
}

func get(t *testing.T, url string) *http.Response {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close response body: %v", err)
		}
	})
	return response
}

func TestRecordsMethodPathHeadersAndJSON(t *testing.T) {
	s := New(t)
	s.Reply("b2_create_key", http.StatusOK, `{}`)
	request, err := http.NewRequest(http.MethodPost, s.URL()+"/b2api/v4/b2_create_key", strings.NewReader(`{"bucketIds":["bid"],"count":5}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Test", "value")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	records := s.Requests()
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	r := records[0]
	if r.Seq != 1 || r.Method != http.MethodPost || r.Path != "/b2api/v4/b2_create_key" || r.Call != "b2_create_key" {
		t.Errorf("record = %+v, want seq=1 POST /b2api/v4/b2_create_key", r)
	}
	AssertHeader(t, r, "X-Test", "value")
	if string(r.Body) != `{"bucketIds":["bid"],"count":5}` {
		t.Errorf("body = %q", r.Body)
	}
	AssertJSON(t, r, "bucketIds.0", "bid")
	AssertJSON(t, r, "count", int64(5))
	if len(s.Calls("b2_create_key")) != 1 || s.Last(t, "b2_create_key").Seq != 1 {
		t.Error("Calls or Last did not return the recorded request")
	}
	// A caller must not be able to mutate the server's record through the returned slice.
	r.Header.Set("X-Test", "changed")
	r.Body[0] = 'x'
	r.JSON["bucketIds"].([]any)[0] = "changed"
	if got := s.Last(t, "b2_create_key"); got.Header.Get("X-Test") != "value" || got.Body[0] != '{' || got.JSON["bucketIds"].([]any)[0] != "bid" {
		t.Errorf("record changed through returned snapshot: %+v", got)
	}
	// The server must copy the incoming header, not retain the request's map.
	serverRequest := httptest.NewRequest(http.MethodPost, "/b2api/v4/b2_create_key", nil)
	serverRequest.Header.Set("X-Test", "before")
	s.serve(httptest.NewRecorder(), serverRequest)
	serverRequest.Header.Set("X-Test", "after")
	if got := s.Last(t, "b2_create_key").Header.Get("X-Test"); got != "before" {
		t.Errorf("recorded header = %q, want before", got)
	}
}

func TestCallsReturnsIndependentJSON(t *testing.T) {
	s := New(t)
	s.Reply("b2_create_key", http.StatusOK, `{}`)
	response, err := http.Post(s.URL()+"/b2api/v4/b2_create_key", "application/json", strings.NewReader(`{"keyName":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}

	call := "b2_create_key"
	s.Calls(call)[0].JSON["added"] = true
	if _, ok := s.Calls(call)[0].JSON["added"]; ok {
		t.Error("returned JSON changed the recorded request")
	}
}

func TestRecordsQuery(t *testing.T) {
	s := New(t)
	call := "b2_get_bucket_notification_rules"
	s.Reply(call, http.StatusOK, `{}`)
	response := get(t, s.URL()+"/b2api/v4/"+call+"?bucketId=b-1")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}

	record := s.Calls(call)[0]
	if record.Path != "/b2api/v4/b2_get_bucket_notification_rules" || record.Call != call {
		t.Errorf("record = %+v, want path and call for b2_get_bucket_notification_rules", record)
	}
	if got := record.Query.Get("bucketId"); got != "b-1" {
		t.Fatalf("bucketId = %q, want b-1", got)
	}
	record.Query.Set("bucketId", "changed")
	if got := s.Calls(call)[0].Query.Get("bucketId"); got != "b-1" {
		t.Errorf("returned Query changed the recorded request: got %q, want b-1", got)
	}
}

func TestHandleAndCallsCanReadJSONConcurrently(t *testing.T) {
	s := New(t)
	handlerStarted := make(chan struct{})
	readJSON := make(chan struct{})
	s.Handle("b2_create_bucket", func(r Request) Response {
		close(handlerStarted)
		<-readJSON
		for range 10000 {
			if r.JSON["bucketName"] != "test" {
				return Response{Status: http.StatusInternalServerError, Body: "wrong request JSON"}
			}
		}
		return Response{Status: http.StatusOK, Body: `{}`}
	})

	responseDone := make(chan error, 1)
	go func() {
		response, err := http.Post(s.URL()+"/b2api/v4/b2_create_bucket", "application/json", strings.NewReader(`{"bucketName":"test"}`))
		if err == nil {
			err = response.Body.Close()
		}
		responseDone <- err
	}()
	<-handlerStarted

	callsStarted := make(chan struct{})
	callsDone := make(chan struct{})
	go func() {
		defer close(callsDone)
		close(callsStarted)
		for range 10000 {
			_ = s.Calls("b2_create_bucket")
		}
	}()
	<-callsStarted
	close(readJSON)
	<-callsDone
	if err := <-responseDone; err != nil {
		t.Fatal(err)
	}
}

func TestRecordsInArrivalOrderUnderConcurrency(t *testing.T) {
	s := New(t)
	s.Reply("b2_list_buckets", http.StatusOK, `{}`)
	const count = 50
	var wg sync.WaitGroup
	errors := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response, err := http.Get(s.URL() + "/b2api/v3/b2_list_buckets")
			if err != nil {
				errors <- err
				return
			}
			if err := response.Body.Close(); err != nil {
				errors <- err
				return
			}
			if response.StatusCode != http.StatusOK {
				errors <- fmt.Errorf("status = %d", response.StatusCode)
			}
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	records := s.Requests()
	if len(records) != count {
		t.Fatalf("records = %d, want %d", len(records), count)
	}
	for i, r := range records {
		if r.Seq != i+1 || r.Call != "b2_list_buckets" {
			t.Errorf("record %d = seq %d call %q", i, r.Seq, r.Call)
		}
	}
}

func TestDefaultAuthorizeUsesServerURL(t *testing.T) {
	s := New(t)
	for _, version := range []string{"v3", "v4"} {
		t.Run(version, func(t *testing.T) {
			response := get(t, s.URL()+"/b2api/"+version+"/b2_authorize_account")
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", response.StatusCode)
			}
			var body struct {
				AccountID          string `json:"accountId"`
				AuthorizationToken string `json:"authorizationToken"`
				APIInfo            struct {
					StorageAPI struct {
						APIURL                  string `json:"apiUrl"`
						DownloadURL             string `json:"downloadUrl"`
						S3APIURL                string `json:"s3ApiUrl"`
						AbsoluteMinimumPartSize int64  `json:"absoluteMinimumPartSize"`
						RecommendedPartSize     int64  `json:"recommendedPartSize"`
						Allowed                 struct {
							Buckets    []string `json:"buckets"`
							NamePrefix *string  `json:"namePrefix"`
						} `json:"allowed"`
					} `json:"storageApi"`
				} `json:"apiInfo"`
			}
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			api := body.APIInfo.StorageAPI
			if body.AccountID != "a" || body.AuthorizationToken != "test-token" || api.APIURL != s.URL() || api.DownloadURL != s.URL() || api.S3APIURL != s.URL() || api.AbsoluteMinimumPartSize != 5000000 || api.RecommendedPartSize != 100000000 || api.Allowed.Buckets != nil || api.Allowed.NamePrefix != nil {
				t.Errorf("authorization response does not match built-in v4 shape: %+v", body)
			}
		})
	}
	s.Reply("b2_authorize_account", http.StatusAccepted, `{"replacement":true}`)
	if response := get(t, s.URL()+"/b2api/v4/b2_authorize_account"); response.StatusCode != http.StatusAccepted {
		t.Errorf("scripted authorize status = %d, want 202", response.StatusCode)
	}
}

func TestUploadURLsAreServedAndRecorded(t *testing.T) {
	s := New(t)
	for _, tc := range []struct{ api, path, token string }{
		{"b2_get_upload_url", "/upload_file", "upload-token"},
		{"b2_get_upload_part_url", "/upload_part", "upload-part-token"},
	} {
		t.Run(tc.api, func(t *testing.T) {
			response := get(t, s.URL()+"/b2api/v4/"+tc.api)
			var upload struct {
				URL   string `json:"uploadUrl"`
				Token string `json:"authorizationToken"`
			}
			if err := json.NewDecoder(response.Body).Decode(&upload); err != nil {
				t.Fatal(err)
			}
			if upload.URL != s.URL()+tc.path || upload.Token != tc.token {
				t.Errorf("upload credentials = %+v", upload)
			}
			posted, err := http.Post(upload.URL, "application/octet-stream", strings.NewReader("raw-body"))
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(posted.Body)
			if closeErr := posted.Body.Close(); closeErr != nil {
				t.Error(closeErr)
			}
			if err != nil || posted.StatusCode != http.StatusOK || string(data) != `{}` {
				t.Errorf("upload response = %d %q, err %v", posted.StatusCode, data, err)
			}
			record := s.Last(t, strings.TrimPrefix(tc.path, "/"))
			if record.Path != tc.path || string(record.Body) != "raw-body" || record.JSON != nil {
				t.Errorf("upload record = %+v", record)
			}
		})
	}
}

func TestReplySequenceRepeatsLast(t *testing.T) {
	s := New(t)
	s.ReplySequence("b2_copy_file", Response{Status: 503, Body: "retry"}, Response{Status: 200, Body: "ok", Header: http.Header{"X-Test": {"response"}}})
	for i, want := range []struct {
		status int
		body   string
	}{{503, "retry"}, {200, "ok"}, {200, "ok"}} {
		response := get(t, s.URL()+"/b2api/v4/b2_copy_file")
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want.status || string(data) != want.body {
			t.Errorf("reply %d = %d %q, want %d %q", i, response.StatusCode, data, want.status, want.body)
		}
		if i > 0 && response.Header.Get("X-Test") != "response" {
			t.Errorf("reply %d header = %q", i, response.Header.Get("X-Test"))
		}
	}
	s.Reply("b2_copy_file", 201, "replacement")
	if response := get(t, s.URL()+"/b2api/v4/b2_copy_file"); response.StatusCode != 201 {
		t.Errorf("replacement status = %d, want 201", response.StatusCode)
	}
}

func TestHandleSeesTheRequest(t *testing.T) {
	s := New(t)
	s.Handle("b2_create_bucket", func(r Request) Response {
		// Calling Requests here would deadlock if the handler ran under the server mutex.
		if len(s.Requests()) != 1 {
			return Response{Status: 500, Body: "request was not recorded before Handle"}
		}
		return Response{Status: 201, Body: fmt.Sprintf(`{"name":%q}`, r.JSON["bucketName"])}
	})
	response, err := http.Post(s.URL()+"/b2api/v4/b2_create_bucket", "application/json", strings.NewReader(`{"bucketName":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close response body: %v", err)
		}
	})
	if response.StatusCode != 201 {
		t.Fatalf("status = %d, want 201", response.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["name"] != "test" {
		t.Errorf("handler response = %v, want name test", body)
	}
}

func TestUnscriptedCallFails(t *testing.T) {
	fake := &recordingTB{TB: t}
	s := New(fake)
	response := get(t, s.URL()+"/b2api/v4/b2_unknown")
	if response.StatusCode != 400 {
		t.Errorf("status = %d, want 400", response.StatusCode)
	}
	var body struct {
		Status  int    `json:"status"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Status != 400 || body.Code != "bad_request" || body.Message != "b2wiretest: no response scripted for this call" {
		t.Errorf("error body = %+v", body)
	}
	if messages := fake.errors(); len(messages) != 1 || !strings.Contains(messages[0], "b2_unknown") {
		t.Errorf("error reports = %v, want b2_unknown", messages)
	}
	if len(s.Calls("b2_unknown")) != 1 {
		t.Error("unscripted call was not recorded")
	}
	missing := &recordingTB{TB: t}
	s.Last(missing, "never_called")
	if messages := missing.errors(); len(messages) != 1 || !strings.Contains(messages[0], "never_called") {
		t.Errorf("Last failure = %v, want never_called", messages)
	}
}

func TestAssertJSONReportsGotAndWant(t *testing.T) {
	r := Request{Call: "b2_update_bucket", JSON: map[string]any{"count": float64(5), "bucketIds": []any{"a"}}}
	fake := &recordingTB{TB: t}
	AssertJSON(fake, r, "count", int64(5))
	AssertJSON(fake, r, "bucketIds", []string{"a"})
	if messages := fake.errors(); len(messages) != 0 {
		t.Errorf("equivalent JSON values reported errors: %v", messages)
	}
	AssertJSON(fake, r, "count", 6)
	AssertJSON(fake, r, "bucketIds.1", "b")
	messages := fake.errors()
	if len(messages) != 2 || !strings.Contains(messages[0], "b2_update_bucket body count: got 5, want 6") || !strings.Contains(messages[1], "b2_update_bucket body bucketIds.1: missing") {
		t.Errorf("assertion reports = %v", messages)
	}
}

func TestAssertNoJSONAndAssertHeader(t *testing.T) {
	r := Request{Call: "b2_create_bucket", JSON: map[string]any{"nested": map[string]any{"present": true}}, Header: http.Header{"X-Test": {"value"}}}
	fake := &recordingTB{TB: t}
	AssertNoJSON(fake, r, "nested.absent")
	AssertHeader(fake, r, "X-Test", "value")
	if messages := fake.errors(); len(messages) != 0 {
		t.Errorf("matching assertions reported errors: %v", messages)
	}
	AssertNoJSON(fake, r, "nested.present")
	AssertHeader(fake, r, "X-Test", "other")
	messages := fake.errors()
	if len(messages) != 2 || !strings.Contains(messages[0], "b2_create_bucket body nested.present: got true, want missing") || !strings.Contains(messages[1], `b2_create_bucket header X-Test: got "value", want "other"`) {
		t.Errorf("assertion reports = %v", messages)
	}
}

func TestAssertQueryReportsGotAndWant(t *testing.T) {
	r := Request{Call: "b2_get_bucket_notification_rules", Query: url.Values{"bucketId": {"b-1"}}}
	fake := &recordingTB{TB: t}
	AssertQuery(fake, r, "bucketId", "other")
	AssertQuery(fake, r, "missing", "value")
	messages := fake.errors()
	if len(messages) != 2 || !strings.Contains(messages[0], `b2_get_bucket_notification_rules query bucketId: got "b-1", want "other"`) || !strings.Contains(messages[1], `b2_get_bucket_notification_rules query missing: missing, want "value"`) {
		t.Errorf("assertion reports = %v", messages)
	}
}
