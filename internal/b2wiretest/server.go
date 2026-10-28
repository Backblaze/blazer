// Package b2wiretest provides a recording HTTP server for B2 wire tests.
package b2wiretest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Request is a snapshot of one HTTP request received by a Server.
type Request struct {
	Seq    int
	Method string
	Path   string
	Call   string
	Header http.Header
	Body   []byte
	JSON   map[string]any
}

// Response is the status, headers, and body returned for a scripted call.
type Response struct {
	Status int
	Body   string
	Header http.Header
}

type script struct {
	responses []Response
	handler   func(Request) Response
	used      int
}

// Server records B2 requests and answers with built-in or scripted responses.
type Server struct {
	t        testing.TB
	http     *httptest.Server
	mu       sync.Mutex
	requests []Request
	scripts  map[string]*script
}

// New starts a recording server and closes it when t finishes.
func New(t testing.TB) *Server {
	t.Helper()
	s := &Server{t: t, scripts: make(map[string]*script)}
	s.http = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.http.Close)
	return s
}

// URL returns the server's base URL.
func (s *Server) URL() string { return s.http.URL }

// Reply registers a response repeated for every request to call.
func (s *Server) Reply(call string, status int, body string) {
	s.ReplySequence(call, Response{Status: status, Body: body})
}

// ReplySequence registers successive responses; the last response repeats.
func (s *Server) ReplySequence(call string, rs ...Response) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scripts[call] = &script{responses: append([]Response(nil), rs...)}
}

// Handle registers a response computed from each request. It replaces any earlier script.
func (s *Server) Handle(call string, h func(Request) Response) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scripts[call] = &script{handler: h}
}

// Requests returns all requests in arrival order.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return copyRequests(s.requests)
}

// Calls returns requests for call in arrival order.
func (s *Server) Calls(call string) []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	var matches []Request
	for _, r := range s.requests {
		if r.Call == call {
			matches = append(matches, r)
		}
	}
	return copyRequests(matches)
}

// Last returns the last request for call, or fails the test if none exists.
func (s *Server) Last(t testing.TB, call string) Request {
	t.Helper()
	requests := s.Calls(call)
	if len(requests) == 0 {
		t.Fatalf("b2wiretest: no request for %s", call)
		return Request{}
	}
	return requests[len(requests)-1]
}

func copyRequests(requests []Request) []Request {
	result := make([]Request, len(requests))
	for i, r := range requests {
		result[i] = r
		result[i].Header = r.Header.Clone()
		result[i].Body = append([]byte(nil), r.Body...)
		if r.JSON != nil {
			result[i].JSON = nil
			_ = json.Unmarshal(result[i].Body, &result[i].JSON)
		}
	}
	return result
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.t.Errorf("b2wiretest: read %s body: %v", r.URL.Path, err)
		http.Error(w, "b2wiretest: cannot read request body", http.StatusBadRequest)
		return
	}
	call := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
	req := Request{Method: r.Method, Path: r.URL.Path, Call: call, Header: r.Header.Clone(), Body: body}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &req.JSON)
	}

	s.mu.Lock()
	req.Seq = len(s.requests) + 1
	s.requests = append(s.requests, req)
	registered, ok := s.scripts[call]
	var response Response
	var handler func(Request) Response
	if ok {
		handler = registered.handler
		if handler == nil && len(registered.responses) > 0 {
			index := registered.used
			if index >= len(registered.responses) {
				index = len(registered.responses) - 1
			}
			response = registered.responses[index]
			registered.used++
		}
	}
	s.mu.Unlock()

	if handler != nil {
		response = handler(copyRequests([]Request{req})[0])
	} else if !ok {
		response, ok = s.builtin(call)
	}
	if !ok || response.Status == 0 {
		s.t.Errorf("b2wiretest: no response for %s", call)
		response = Response{Status: http.StatusBadRequest,
			Body: fmt.Sprintf(`{"status":400,"code":"bad_request","message":"b2wiretest: no response for %s"}`, call)}
	}
	for name, values := range response.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.WriteHeader(response.Status)
	if _, err := io.WriteString(w, response.Body); err != nil {
		s.t.Errorf("b2wiretest: write %s response: %v", call, err)
	}
}

func (s *Server) builtin(call string) (Response, bool) {
	switch call {
	case "b2_authorize_account":
		return Response{Status: http.StatusOK, Body: fmt.Sprintf(`{"accountId":"a","authorizationToken":"test-token","applicationKeyExpirationTimestamp":0,"apiInfo":{"storageApi":{"absoluteMinimumPartSize":5000000,"apiUrl":"%[1]s","capabilities":[],"downloadUrl":"%[1]s","storageApi":"storage","recommendedPartSize":100000000,"s3ApiUrl":"%[1]s","allowed":{"buckets":null,"capabilities":[],"namePrefix":null}}}}`, s.URL())}, true
	case "b2_get_upload_url":
		return Response{Status: http.StatusOK, Body: fmt.Sprintf(`{"uploadUrl":%q,"authorizationToken":"upload-token"}`, s.URL()+"/upload_file")}, true
	case "b2_get_upload_part_url":
		return Response{Status: http.StatusOK, Body: fmt.Sprintf(`{"uploadUrl":%q,"authorizationToken":"upload-part-token"}`, s.URL()+"/upload_part")}, true
	case "upload_file", "upload_part":
		return Response{Status: http.StatusOK, Body: `{}`}, true
	default:
		return Response{}, false
	}
}
