package b2wiretest

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// RangeDownload returns a B2-style download response for a fixture's body.
// A range covering the whole body is returned as HTTP 200; partial ranges are 206.
func RangeDownload(body, byteRange string) Response {
	start, end := int64(0), int64(len(body)-1)
	if byteRange != "" {
		if !strings.HasPrefix(byteRange, "bytes=") {
			return Response{Status: http.StatusBadRequest, Body: `{"status":400,"code":"bad_request","message":"invalid range"}`}
		}
		startText, endText, ok := strings.Cut(strings.TrimPrefix(byteRange, "bytes="), "-")
		if !ok {
			return Response{Status: http.StatusBadRequest, Body: `{"status":400,"code":"bad_request","message":"invalid range"}`}
		}
		var err error
		start, err = strconv.ParseInt(startText, 10, 64)
		if err != nil || start < 0 {
			return Response{Status: http.StatusBadRequest, Body: `{"status":400,"code":"bad_request","message":"invalid range"}`}
		}
		if endText != "" {
			end, err = strconv.ParseInt(endText, 10, 64)
			if err != nil || end < start {
				return Response{Status: http.StatusBadRequest, Body: `{"status":400,"code":"bad_request","message":"invalid range"}`}
			}
		}
	}
	if byteRange != "" && start >= int64(len(body)) {
		return Response{Status: http.StatusRequestedRangeNotSatisfiable, Body: `{"status":416,"code":"range_not_satisfiable","message":"past end"}`}
	}
	if end >= int64(len(body)) {
		end = int64(len(body)) - 1
	}
	response := Response{Status: http.StatusOK, Header: make(http.Header), Body: body}
	if byteRange != "" && (start != 0 || end != int64(len(body)-1)) {
		response.Status = http.StatusPartialContent
		response.Body = body[start : end+1]
		response.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(body)))
	}
	response.Header.Set("Content-Length", strconv.Itoa(len(response.Body)))
	return response
}
