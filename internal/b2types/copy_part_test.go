package b2types

import (
	"encoding/json"
	"testing"
)

func TestCopyPartRequestWireShape(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  CopyPartRequest
		want string
	}{
		{"whole source", CopyPartRequest{SourceFileID: "src", LargeFileID: "large", PartNumber: 2}, `{"sourceFileId":"src","largeFileId":"large","partNumber":2}`},
		{"range", CopyPartRequest{SourceFileID: "src", LargeFileID: "large", PartNumber: 2, Range: "bytes=0-9"}, `{"sourceFileId":"src","largeFileId":"large","partNumber":2,"range":"bytes=0-9"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.req)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("JSON = %s, want %s", got, tc.want)
			}
		})
	}
	var response CopyPartResponse
	if err := json.Unmarshal([]byte(`{"fileId":"large","partNumber":2,"contentLength":10,"contentSha1":"abc"}`), &response); err != nil {
		t.Fatal(err)
	}
	if response.FileID != "large" || response.PartNumber != 2 || response.Size != 10 || response.SHA1 != "abc" {
		t.Errorf("response = %+v", response)
	}
}
