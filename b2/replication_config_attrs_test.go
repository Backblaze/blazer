package b2

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type replicationConfigRoundTripper func(*http.Request) (*http.Response, error)

func (f replicationConfigRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestBucketAttrsReplicationConfigReadback(t *testing.T) {
	tests := []struct {
		name       string
		config     map[string]interface{}
		wantConfig bool
	}{
		{
			name: "source configuration",
			config: map[string]interface{}{
				"isClientAuthorizedToRead": true,
				"value": map[string]interface{}{
					"asReplicationSource": map[string]interface{}{
						"sourceApplicationKeyId": "source-key",
						"replicationRules": []interface{}{map[string]interface{}{
							"destinationBucketId":  "destination-bucket",
							"fileNamePrefix":       "demo/",
							"includeExistingFiles": true,
							"isEnabled":            true,
							"priority":             128,
							"replicationRuleName":  "demo-rule",
						}},
					},
				},
			},
			wantConfig: true,
		},
		{name: "no configuration"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			transport := replicationConfigRoundTripper(func(req *http.Request) (*http.Response, error) {
				var response interface{}
				switch {
				case strings.HasSuffix(req.URL.Path, "/b2_authorize_account"):
					response = map[string]interface{}{
						"accountId": "account", "authorizationToken": "token",
						"apiInfo": map[string]interface{}{"storageApi": map[string]interface{}{
							"apiUrl": "https://b2.test", "downloadUrl": "https://b2.test",
						}},
					}
				case strings.HasSuffix(req.URL.Path, "/b2_list_buckets"):
					bucket := map[string]interface{}{
						"bucketId": "bucket-id", "bucketName": "bucket", "bucketType": "allPrivate",
						"bucketInfo": map[string]string{}, "revision": 1,
					}
					if tc.config != nil {
						bucket["replicationConfiguration"] = tc.config
					}
					response = map[string]interface{}{"buckets": []interface{}{bucket}}
				default:
					return nil, fmt.Errorf("unexpected request %s", req.URL)
				}
				body, err := json.Marshal(response)
				if err != nil {
					return nil, err
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), Request: req}, nil
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
			attrs, err := bucket.Attrs(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.wantConfig {
				if attrs.ReplicationConfig != nil {
					t.Fatalf("ReplicationConfig = %+v, want nil", attrs.ReplicationConfig)
				}
				return
			}
			config := attrs.ReplicationConfig
			if config == nil {
				t.Fatal("ReplicationConfig = nil, want source configuration")
			}
			source := config.AsReplicationSource
			if source.SourceApplicationKeyID != "source-key" {
				t.Errorf("SourceApplicationKeyID = %q, want %q", source.SourceApplicationKeyID, "source-key")
			}
			if len(source.ReplicationRules) != 1 {
				t.Fatalf("ReplicationRules length = %d, want 1", len(source.ReplicationRules))
			}
			rule := source.ReplicationRules[0]
			if rule.DestinationBucketID != "destination-bucket" || rule.FileNamePrefix != "demo/" || !rule.IncludeExistingFiles || !rule.IsEnabled || rule.Priority != 128 || rule.ReplicationRuleName != "demo-rule" {
				t.Errorf("ReplicationRules[0] = %+v, want source rule values", rule)
			}
		})
	}
}
