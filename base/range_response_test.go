package base

import (
	"errors"
	"net/http"
	"testing"
)

func TestValidateRangeResponse(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       int
		offset, size int64
		length, span string
		wantErr      bool
	}{
		{name: "200 whole file", status: 200, offset: 0, size: 6, length: "6"},
		{name: "200 shorter than request", status: 200, offset: 0, size: 8, length: "6"},
		{name: "200 open ended", status: 200, offset: 0, size: -1, length: "6"},
		{name: "200 overlong", status: 200, offset: 0, size: 2, length: "6", wantErr: true},
		{name: "200 at nonzero offset", status: 200, offset: 2, size: 2, length: "2", wantErr: true},
		{name: "200 chunked", status: 200, offset: 0, size: 6, wantErr: true},
		{name: "200 invalid length", status: 200, offset: 0, size: 6, length: "invalid", wantErr: true},
		{name: "206 matching range", status: 206, offset: 2, size: 2, length: "2", span: "bytes 2-3/6"},
		{name: "206 stops at total", status: 206, offset: 4, size: 8, length: "2", span: "bytes 4-5/6"},
		{name: "206 open ended", status: 206, offset: 2, size: 0, length: "4", span: "bytes 2-5/6"},
		{name: "206 wrong start", status: 206, offset: 2, size: 2, length: "2", span: "bytes 1-2/6", wantErr: true},
		{name: "206 wrong end", status: 206, offset: 2, size: 2, length: "1", span: "bytes 2-2/6", wantErr: true},
		{name: "206 wrong total", status: 206, offset: 2, size: 2, length: "2", span: "bytes 2-3/3", wantErr: true},
		{name: "206 end past total", status: 206, offset: 2, size: 2, length: "999998", span: "bytes 2-999999/10", wantErr: true},
		{name: "206 reversed range", status: 206, offset: 2, size: 2, length: "2", span: "bytes 2-1/6", wantErr: true},
		{name: "206 missing Content-Range", status: 206, offset: 2, size: 2, length: "2", wantErr: true},
		{name: "206 invalid Content-Range", status: 206, offset: 2, size: 2, length: "2", span: "bytes 2-3/*", wantErr: true},
		{name: "206 signed Content-Range", status: 206, offset: 2, size: 2, length: "2", span: "bytes 2-3/+6", wantErr: true},
		{name: "206 invalid Content-Range prefix", status: 206, offset: 2, size: 2, length: "2", span: "2-3/6", wantErr: true},
		{name: "206 overlong body", status: 206, offset: 2, size: 2, length: "6", span: "bytes 2-3/6", wantErr: true},
		{name: "206 short body", status: 206, offset: 2, size: 2, length: "1", span: "bytes 2-3/6", wantErr: true},
		{name: "206 chunked", status: 206, offset: 2, size: 2, span: "bytes 2-3/6", wantErr: true},
		{name: "unexpected status", status: 204, offset: 2, size: 2, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: tc.status, Header: make(http.Header)}
			if tc.length != "" {
				resp.Header.Set("Content-Length", tc.length)
			}
			if tc.span != "" {
				resp.Header.Set("Content-Range", tc.span)
			}
			err := validateRangeResponse(resp, "b2_download_file_by_name", tc.offset, tc.size)
			if got := errors.Is(err, ErrInvalidRangeResponse); got != tc.wantErr {
				t.Errorf("validateRangeResponse() = %v, invalid range error = %t, want %t", err, got, tc.wantErr)
			}
		})
	}
}
