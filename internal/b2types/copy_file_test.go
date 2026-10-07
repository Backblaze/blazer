package b2types

import (
	"encoding/json"
	"testing"
)

func TestCopyFileRequestWireShape(t *testing.T) {
	empty, full := "", "text/plain"
	info, noInfo := map[string]string{"k": "v"}, map[string]string{}
	tests := []struct {
		name string
		in   CopyFileRequest
		want string
	}{
		{
			name: "COPY sends no contentType or fileInfo",
			in:   CopyFileRequest{SourceFileID: "src", FileName: "dst", MetadataDirective: "COPY"},
			want: `{"sourceFileId":"src","fileName":"dst","metadataDirective":"COPY"}`,
		},
		{
			name: "destination bucket and range",
			in:   CopyFileRequest{SourceFileID: "src", FileName: "dst", DestinationBucketID: "bkt", Range: "bytes=0-99", MetadataDirective: "COPY"},
			want: `{"sourceFileId":"src","fileName":"dst","destinationBucketId":"bkt","range":"bytes=0-99","metadataDirective":"COPY"}`,
		},
		{
			name: "REPLACE sends contentType and fileInfo",
			in:   CopyFileRequest{SourceFileID: "src", FileName: "dst", MetadataDirective: "REPLACE", ContentType: &full, FileInfo: &info},
			want: `{"sourceFileId":"src","fileName":"dst","metadataDirective":"REPLACE","contentType":"text/plain","fileInfo":{"k":"v"}}`,
		},
		{
			name: "REPLACE with an empty fileInfo sends {}",
			in:   CopyFileRequest{SourceFileID: "src", FileName: "dst", MetadataDirective: "REPLACE", ContentType: &full, FileInfo: &noInfo},
			want: `{"sourceFileId":"src","fileName":"dst","metadataDirective":"REPLACE","contentType":"text/plain","fileInfo":{}}`,
		},
		{
			name: "a pointer to an empty contentType is still sent",
			in:   CopyFileRequest{SourceFileID: "src", FileName: "dst", MetadataDirective: "REPLACE", ContentType: &empty},
			want: `{"sourceFileId":"src","fileName":"dst","metadataDirective":"REPLACE","contentType":""}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("request JSON = %s, want %s", got, tc.want)
			}
		})
	}
}
