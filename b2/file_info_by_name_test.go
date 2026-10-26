package b2

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestGetFileInfoByName(t *testing.T) {
	for _, tc := range []struct {
		name     string
		files    []map[string]interface{}
		errorAPI bool
		notFound bool
	}{
		{name: "current visible file", files: []map[string]interface{}{{
			"fileId": "file-id", "fileName": "dir/file.txt", "action": "upload",
			"contentLength": 7, "contentSha1": "a-sha1", "contentType": "text/plain",
			"fileInfo":        map[string]string{"source": "by-name", "src_last_modified_millis": "1700000000000"},
			"uploadTimestamp": int64(1700000001000),
			"fileRetention":   map[string]interface{}{"isClientAuthorizedToRead": true, "value": map[string]interface{}{"mode": "governance", "retainUntilTimestamp": int64(1735689600000)}},
			"legalHold":       map[string]interface{}{"isClientAuthorizedToRead": true, "value": "on"},
		}}},
		{name: "missing file", notFound: true},
		{name: "hidden file", files: []map[string]interface{}{}, notFound: true},
		{name: "next lexical name is not exact", files: []map[string]interface{}{{"fileName": "dir/file.txt-other", "action": "upload"}}, notFound: true},
		{name: "API error is not a download fallback", errorAPI: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var methods []string
			transport := drTransport(func(req *http.Request) (*http.Response, error) {
				method := req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:]
				methods = append(methods, method)
				switch method {
				case "b2_authorize_account":
					return drJSON(req, http.StatusOK, map[string]interface{}{
						"accountId": "account", "authorizationToken": "token",
						"apiInfo": map[string]interface{}{"storageApi": map[string]interface{}{"apiUrl": "https://b2.test", "downloadUrl": "https://b2.test"}},
					})
				case "b2_list_buckets":
					return drJSON(req, http.StatusOK, map[string]interface{}{"buckets": []interface{}{map[string]interface{}{
						"bucketId": "bucket-id", "bucketName": "bucket", "bucketType": "allPrivate", "bucketInfo": map[string]string{},
					}}})
				case "b2_list_file_names":
					var body map[string]interface{}
					if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					want := map[string]interface{}{"bucketId": "bucket-id", "maxFileCount": float64(1), "startFileName": "dir/file.txt", "prefix": "dir/file.txt"}
					if !reflect.DeepEqual(body, want) {
						t.Errorf("list request = %#v, want %#v", body, want)
					}
					if tc.errorAPI {
						return drJSON(req, http.StatusBadRequest, map[string]interface{}{"status": 400, "code": "bad_request", "message": "list unavailable"})
					}
					return drJSON(req, http.StatusOK, map[string]interface{}{"files": tc.files})
				}
				t.Errorf("unexpected request %s", req.URL)
				return drJSON(req, http.StatusNotFound, map[string]interface{}{"status": 404, "code": "not_found", "message": "unexpected"})
			})
			ctx := context.Background()
			client, err := NewClient(ctx, "account", "key", APIBase("https://b2.test"), Transport(transport))
			if err != nil {
				t.Fatal(err)
			}
			bucket, err := client.Bucket(ctx, "bucket")
			if err != nil {
				t.Fatal(err)
			}
			attrs, err := bucket.GetFileInfoByName(ctx, "dir/file.txt")
			if tc.notFound {
				if !IsNotExist(err) || attrs != nil {
					t.Fatalf("got (%v, %v), want not-exist", attrs, err)
				}
			} else if tc.errorAPI {
				if err == nil || IsNotExist(err) || !strings.Contains(err.Error(), "list unavailable") {
					t.Fatalf("got (%v, %v), want original API error", attrs, err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if attrs.Name != "dir/file.txt" || attrs.Size != 7 || attrs.SHA1 != "a-sha1" || attrs.ContentType != "text/plain" || attrs.Info["source"] != "by-name" || attrs.Status != Uploaded {
					t.Errorf("metadata = %+v", attrs)
				}
				if attrs.UploadTimestamp != time.UnixMilli(1700000001000) || attrs.LastModified != time.UnixMilli(1700000000000) {
					t.Errorf("timestamps = %v, %v", attrs.UploadTimestamp, attrs.LastModified)
				}
				if attrs.Retention == nil || attrs.Retention.Mode != RetentionGovernance || attrs.Retention.RetainUntil != time.UnixMilli(1735689600000) || attrs.LegalHold != LegalHoldOn {
					t.Errorf("file lock = %+v, %q", attrs.Retention, attrs.LegalHold)
				}
			}
			if got := fmt.Sprint(methods); got != "[b2_authorize_account b2_list_buckets b2_list_file_names]" {
				t.Errorf("request path = %s; a metadata lookup must not download", got)
			}
		})
	}
}
