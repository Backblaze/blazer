package base

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

type closeTrackingBody struct {
	io.Reader
	closed bool
}

func (b *closeTrackingBody) Close() error {
	b.closed = true
	return nil
}

func TestDownloadContentLengthFitsNativeInt(t *testing.T) {
	for _, tc := range []struct {
		name    string
		length  int64
		maxInt  int64
		wantErr bool
	}{
		{"32-bit boundary", math.MaxInt32, math.MaxInt32, false},
		{"32-bit overflow", int64(math.MaxInt32) + 1, math.MaxInt32, true},
		{"64-bit accepts large file", int64(math.MaxInt32) + 1, math.MaxInt64, false},
		{"negative", -1, math.MaxInt64, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}
			resp.Header.Set("Content-Length", fmt.Sprint(tc.length))
			got, err := newFileReaderWithMaxLength(resp, tc.maxInt)
			if strconv.IntSize == 32 && tc.length > math.MaxInt32 {
				tc.wantErr = true
			}
			if (err != nil) != tc.wantErr {
				t.Fatalf("newFileReaderWithMaxLength(%d, %d) error = %v, want error %v", tc.length, tc.maxInt, err, tc.wantErr)
			}
			if err == nil {
				defer func() {
					if err := got.Close(); err != nil {
						t.Error(err)
					}
				}()
				if int64(got.ContentLength) != tc.length {
					t.Fatalf("content length = %d, want %d", got.ContentLength, tc.length)
				}
			}
		})
	}
}

func TestFileReaderClosesBodyOnHeaderAndStatusErrors(t *testing.T) {
	for _, tc := range []struct {
		name, length string
		status       int
	}{
		{"bad length", "not-a-number", http.StatusOK},
		{"negative length", "-1", http.StatusOK},
		{"bad metadata escape", "0", http.StatusOK},
		{"not found", "0", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &closeTrackingBody{Reader: strings.NewReader(`{"status":404,"code":"not_found","message":"missing"}`)}
			req, err := http.NewRequest(http.MethodGet, "https://example.invalid/b2api/v4/b2_download_file_by_id?fileId=id", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("X-Blazer-Method", "b2_download_file_by_id")
			resp := &http.Response{StatusCode: tc.status, Status: http.StatusText(tc.status), Header: make(http.Header), Body: body, Request: req}
			resp.Header.Set("Content-Length", tc.length)
			if tc.name == "bad metadata escape" {
				resp.Header.Set("X-Bz-Info-%GG", "bad")
			}
			reader, err := newFileReader(resp)
			if err == nil || reader != nil || !body.closed {
				t.Fatalf("newFileReader = (%v, %v), body closed=%v; want error and close", reader, err, body.closed)
			}
		})
	}
}

func TestFileReaderRejects32BitContentLengthOverflow(t *testing.T) {
	const overflow = int64(math.MaxInt32) + 1
	body := &closeTrackingBody{Reader: strings.NewReader("")}
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}
	resp.Header.Set("Content-Length", "2147483648")
	reader, err := newFileReaderWithMaxLength(resp, math.MaxInt32)
	if err == nil || reader != nil || !body.closed {
		t.Fatalf("32-bit overflow %d: reader=%v error=%v body closed=%v", overflow, reader, err, body.closed)
	}
}

func TestRetryAfterLargeValueIsBoundedBeforeNativeIntConversion(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int
	}{
		{"7", 7},
		{"2147483648", 30},
		{"99999999999", 30},
		{"9223372036854775807", 30},
		{"-1", 0},
	} {
		got, err := parseRetryAfterSeconds(tc.value)
		if err != nil || got != tc.want {
			t.Errorf("parseRetryAfterSeconds(%q) = (%d, %v), want %d", tc.value, got, err, tc.want)
		}
	}
}
