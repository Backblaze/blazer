package base

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
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
		{name: "200 empty file", status: 200, offset: 0, size: 1, length: "0"},
		{name: "200 shorter than request", status: 200, offset: 0, size: 8, length: "6"},
		{name: "200 open ended", status: 200, offset: 0, size: -1, length: "6"},
		{name: "200 overlong", status: 200, offset: 0, size: 2, length: "6", wantErr: true},
		{name: "200 at nonzero offset", status: 200, offset: 2, size: 2, length: "2", wantErr: true},
		{name: "200 chunked", status: 200, offset: 0, size: 6, wantErr: true},
		{name: "200 invalid length", status: 200, offset: 0, size: 6, length: "invalid", wantErr: true},
		{name: "206 matching range", status: 206, offset: 2, size: 2, length: "2", span: "bytes 2-3/6"},
		{name: "206 starts at zero", status: 206, offset: 0, size: 2, length: "2", span: "bytes 0-1/6"},
		{name: "206 single byte", status: 206, offset: 2, size: 1, length: "1", span: "bytes 2-2/6"},
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

func TestDownloadsValidateRangesAndLimitBodies(t *testing.T) {
	for _, tc := range []struct {
		name, length, span, body, wantBody, requestRange string
		status                                           int
		offset, size                                     int64
		wantInvalidRange                                 bool
	}{
		{name: "whole file", status: 200, length: "6", body: "abcdef", wantBody: "abcdef"},
		{name: "bounded partial", status: 206, offset: 1, size: 2, requestRange: "bytes=1-2", length: "2", span: "bytes 1-2/6", body: "bcEXTRA", wantBody: "bc"},
		{name: "open ended partial", status: 206, offset: 1, requestRange: "bytes=1-", length: "5", span: "bytes 1-5/6", body: "bcdef", wantBody: "bcdef"},
		{name: "overlong 200", status: 200, size: 2, requestRange: "bytes=0-1", length: "6", body: "abcdef", wantInvalidRange: true},
		{name: "overlong 206", status: 206, offset: 1, size: 2, requestRange: "bytes=1-2", length: "3", span: "bytes 1-2/6", body: "bcd", wantInvalidRange: true},
	} {
		for _, byID := range []bool{false, true} {
			method := "by name"
			if byID {
				method = "by ID"
			}
			t.Run(tc.name+"/"+method, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				body := &closeTrackingBody{Reader: strings.NewReader(tc.body)}
				transport := authSnapshotTransport(func(req *http.Request) (*http.Response, error) {
					if got := req.Header.Get("Range"); got != tc.requestRange {
						t.Errorf("request Range = %q, want %q", got, tc.requestRange)
					}
					// A stub can expose extra body bytes despite Content-Length,
					// independently testing the SDK's body limit and Close forwarding.
					return &http.Response{StatusCode: tc.status, Header: http.Header{
						"Content-Length": {tc.length}, "Content-Range": {tc.span},
					}, Body: body, Request: req}, nil
				})
				bucket := &Bucket{Name: "bucket", b2: &B2{downloadURI: "https://example.invalid", opts: &b2Options{transport: transport}}}
				var reader *FileReader
				var err error
				if byID {
					reader, err = bucket.DownloadFileByID(ctx, "id", tc.offset, tc.size, false)
				} else {
					reader, err = bucket.DownloadFileByName(ctx, "object", tc.offset, tc.size, false)
				}
				if reader != nil {
					defer func() {
						if err := reader.Close(); err != nil {
							t.Error(err)
						}
					}()
				}
				if tc.wantInvalidRange {
					if reader != nil || !errors.Is(err, ErrInvalidRangeResponse) || !body.closed {
						t.Fatalf("download = (%v, %v), closed=%t; want invalid range and closed body", reader, err, body.closed)
					}
					return
				}
				if err != nil || reader == nil {
					t.Fatalf("download = (%v, %v), want reader and nil", reader, err)
				}
				got, readErr := io.ReadAll(reader)
				if readErr != nil || string(got) != tc.wantBody {
					t.Errorf("downloaded body = %q, error=%v; want %q and nil", got, readErr, tc.wantBody)
				}
				if err := reader.Close(); err != nil || !body.closed {
					t.Errorf("Close = %v, underlying closed=%t; want nil and true", err, body.closed)
				}
			})
		}
	}
}
