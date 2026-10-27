// Package b2sim provides an in-memory B2 wire server for blazer clients.
// Pass New() to b2.Transport to exercise the normal HTTP, retry, and JSON path.
// It is a testing aid, not a complete B2 emulator: it does not enforce
// application-key credentials or capabilities, retention, replication, encryption, lifecycle,
// CORS, or download-authorization signatures. Unsupported endpoints fail with
// a non-2xx response; they never silently succeed.
// With valid simulator authorization, b2_hide_file, b2_list_unfinished_large_files
// (so Writer.Resume), b2_list_parts, b2_copy_file, b2_copy_part (so Copy and
// Compose with copied parts), b2_download_file_by_id, b2_get_download_authorization,
// b2_get_bucket_notification_rules, b2_set_bucket_notification_rules,
// b2_update_file_retention, b2_update_file_legal_hold, b2_create_key (v4),
// b2_list_keys, and b2_delete_key return HTTP 501 unsupported_operation.
// Other unimplemented v4 endpoints also return 501; v3 endpoints return 404.
// Multipart part-number and minimum-size limits are enforced like B2.
package b2sim

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const origin = "http://b2sim.invalid"

const absoluteMinimumPartSize = 5_000_000

type bucket struct {
	id, name, kind string
	info           map[string]any
	rules, cors    any
	sse, retention any
	replication    any
	fileLock       bool
	revision       int
	versions       []*version
}

type version struct {
	id, bucketID, name, contentType, sha, action string
	data                                         []byte
	info                                         map[string]string
	sse                                          map[string]any
	retention                                    any
	legalHold                                    string
	stamp                                        int64
	order                                        uint64
}

type largeFile struct {
	file   *version
	parts  map[int][]byte
	hashes map[int]string
}

// Simulator is an independent, concurrency-safe in-memory B2 service.
// It stores bucket and file settings but does not enforce retention, replication,
// encryption, lifecycle, or download-authorization policies.
type Simulator struct {
	mu      sync.Mutex
	next    uint64
	buckets map[string]*bucket
	large   map[string]*largeFile
	calls   map[string]int
}

// New creates an empty simulator. No sockets or external services are used.
func New() *Simulator {
	return &Simulator{buckets: make(map[string]*bucket), large: make(map[string]*largeFile), calls: make(map[string]int)}
}

// Calls returns a snapshot of B2 endpoint counts for test assertions.
func (s *Simulator) Calls() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	counts := make(map[string]int, len(s.calls))
	for name, count := range s.calls {
		counts[name] = count
	}
	return counts
}

// Seed creates a private bucket for a test and returns its ID. Repeated names
// return the existing ID.
func (s *Simulator) Seed(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range s.buckets {
		if b.name == name {
			return b.id
		}
	}
	b := &bucket{id: s.id("bucket"), name: name, kind: "allPrivate", info: map[string]any{}, rules: []any{}, cors: []any{}, revision: 1}
	s.buckets[b.id] = b
	return b.id
}

// Buckets returns a sorted snapshot of bucket names.
func (s *Simulator) Buckets() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.buckets))
	for _, b := range s.buckets {
		names = append(names, b.name)
	}
	sort.Strings(names)
	return names
}

func (s *Simulator) id(prefix string) string {
	s.next++ // caller holds s.mu
	return prefix + "-" + strconv.FormatUint(s.next, 10)
}

func reply(req *http.Request, status int, value any) (*http.Response, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return rawReply(req, status, data, http.Header{"Content-Type": []string{"application/json"}}), nil
}

func rawReply(req *http.Request, status int, data []byte, headers http.Header) *http.Response {
	if headers == nil {
		headers = make(http.Header)
	}
	headers.Set("Content-Length", strconv.Itoa(len(data)))
	if req.Method == http.MethodHead {
		return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)), Header: headers, Body: http.NoBody, ContentLength: int64(len(data)), Request: req}
	}
	return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)), Header: headers, Body: io.NopCloser(bytes.NewReader(data)), ContentLength: int64(len(data)), Request: req}
}

func failure(req *http.Request, status int, code, message string) (*http.Response, error) {
	return reply(req, status, map[string]any{"status": status, "code": code, "message": message})
}

func decode(data []byte) (map[string]any, error) {
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, fmt.Errorf("expected JSON object")
	}
	return value, nil
}

func str(value any) string { v, _ := value.(string); return v }

func num(value any) int {
	v, _ := value.(float64)
	if v <= 0 {
		return 0
	}
	// Bound untrusted JSON numbers before converting on 32-bit platforms.
	if v >= float64(^uint(0)>>1) {
		return int(^uint(0) >> 1)
	}
	return int(v)
}

func textMap(value any) map[string]string {
	result := make(map[string]string)
	if m, ok := value.(map[string]any); ok {
		for k, v := range m {
			result[k] = str(v)
		}
	}
	return result
}

// responseSSE keeps only the public mode and algorithm. In particular, a
// request's SSE-C customer key must never be retained in simulator state or
// echoed in file responses.
func responseSSE(value any) map[string]any {
	settings := argsMap(value)
	return map[string]any{"mode": settings["mode"], "algorithm": settings["algorithm"]}
}

func digest(data []byte) string {
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])
}

func verifiedUpload(data []byte, supplied string) ([]byte, string, bool) {
	if supplied == "hex_digits_at_end" {
		if len(data) < 40 {
			return nil, "", false
		}
		supplied = string(data[len(data)-40:])
		data = data[:len(data)-40]
		if _, err := hex.DecodeString(supplied); err != nil {
			return nil, "", false
		}
	}
	return data, supplied, supplied == digest(data)
}

func (b *bucket) response() map[string]any {
	return map[string]any{
		"bucketId": b.id, "bucketName": b.name, "bucketType": b.kind,
		"bucketInfo": b.info, "lifecycleRules": b.rules, "corsRules": b.cors,
		"revision":                    b.revision,
		"defaultServerSideEncryption": map[string]any{"isClientAuthorizedToRead": true, "value": b.sse},
		"fileLockConfiguration": map[string]any{"isClientAuthorizedToRead": true, "value": map[string]any{
			"isFileLockEnabled": b.fileLock, "defaultRetention": b.retention,
		}},
		"replicationConfiguration": map[string]any{"isClientAuthorizedToRead": true, "value": b.replication},
	}
}

func (f *version) response() map[string]any {
	retention := f.retention
	if retention == nil {
		retention = map[string]any{"mode": nil, "retainUntilTimestamp": nil}
	}
	var legalHold any
	if f.legalHold != "" {
		legalHold = f.legalHold
	}
	return map[string]any{
		"fileId": f.id, "fileName": f.name, "accountId": "b2sim-account", "bucketId": f.bucketID,
		"contentLength": len(f.data), "contentSha1": f.sha, "contentType": f.contentType,
		"fileInfo": f.info, "action": f.action, "uploadTimestamp": f.stamp,
		"serverSideEncryption": f.sse,
		"fileRetention":        map[string]any{"isClientAuthorizedToRead": true, "value": retention},
		"legalHold":            map[string]any{"isClientAuthorizedToRead": true, "value": legalHold},
	}
}

// RoundTrip serves supported B2 requests entirely in memory. Request bodies
// are read before locking, so an arbitrary caller-owned Reader cannot hold the
// simulator's state lock while blocked.
func (s *Simulator) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	var data []byte
	if req.Body != nil {
		defer func() { _ = req.Body.Close() }()
		readDone := make(chan struct{})
		closerDone := make(chan struct{})
		go func() {
			defer close(closerDone)
			select {
			case <-req.Context().Done():
				_ = req.Body.Close()
			case <-readDone:
			}
		}()
		var err error
		data, err = io.ReadAll(req.Body)
		close(readDone)
		<-closerDone
		if ctxErr := req.Context().Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := req.URL.Path
	operation := strings.TrimPrefix(path, "/b2api/v4/")
	switch {
	case strings.HasPrefix(path, "/upload/file/"):
		operation = "b2_upload_file"
	case strings.HasPrefix(path, "/upload/part/"):
		operation = "b2_upload_part"
	case strings.HasPrefix(path, "/file/"):
		operation = "b2_download_file_by_name"
	}
	s.calls[operation]++
	if strings.HasPrefix(path, "/file/") {
		return s.download(req)
	}
	if strings.HasPrefix(path, "/upload/file/") {
		return s.upload(req, strings.TrimPrefix(path, "/upload/file/"), data)
	}
	if strings.HasPrefix(path, "/upload/part/") {
		return s.uploadPart(req, strings.TrimPrefix(path, "/upload/part/"), data)
	}
	if !strings.HasPrefix(path, "/b2api/v4/") {
		return failure(req, http.StatusNotFound, "not_found", "unknown simulator endpoint")
	}
	method := strings.TrimPrefix(path, "/b2api/v4/")
	if method == "b2_authorize_account" {
		if req.Method != http.MethodGet || !strings.HasPrefix(req.Header.Get("Authorization"), "Basic ") {
			return failure(req, http.StatusUnauthorized, "unauthorized", "basic authorization required")
		}
		return reply(req, http.StatusOK, map[string]any{
			"accountId": "b2sim-account", "authorizationToken": "b2sim-token",
			"apiInfo": map[string]any{"storageApi": map[string]any{
				"absoluteMinimumPartSize": absoluteMinimumPartSize, "recommendedPartSize": 100_000_000,
				"apiUrl": origin, "downloadUrl": origin, "s3ApiUrl": origin, "storageApi": "storage",
				"capabilities": []string{"listBuckets", "writeBuckets", "readBuckets", "writeFiles", "readFiles", "deleteFiles"},
				"allowed":      map[string]any{"buckets": nil, "capabilities": []string{}, "namePrefix": nil},
			}},
		})
	}
	if req.Header.Get("Authorization") != "b2sim-token" {
		return failure(req, http.StatusUnauthorized, "bad_auth_token", "simulator authorization required")
	}
	switch method {
	case "b2_create_bucket", "b2_list_buckets", "b2_update_bucket", "b2_delete_bucket",
		"b2_get_upload_url", "b2_start_large_file", "b2_get_upload_part_url",
		"b2_finish_large_file", "b2_cancel_large_file", "b2_list_file_names",
		"b2_list_file_versions", "b2_delete_file_version", "b2_get_file_info":
	default:
		return failure(req, http.StatusNotImplemented, "unsupported_operation", "unsupported simulator endpoint: "+method)
	}
	if req.Method != http.MethodPost {
		return failure(req, http.StatusMethodNotAllowed, "method_not_allowed", "simulator requires POST for "+method)
	}
	args, err := decode(data)
	if err != nil {
		return failure(req, http.StatusBadRequest, "bad_request", err.Error())
	}
	switch method {
	case "b2_create_bucket":
		name := str(args["bucketName"])
		if name == "" {
			return failure(req, 400, "bad_request", "bucketName is required")
		}
		for _, b := range s.buckets {
			if b.name == name {
				return failure(req, 409, "duplicate_bucket_name", "bucket already exists")
			}
		}
		b := &bucket{id: s.id("bucket"), name: name, kind: str(args["bucketType"]), info: argsMap(args["bucketInfo"]), rules: args["lifecycleRules"], cors: args["corsRules"], replication: args["replicationConfiguration"], fileLock: args["fileLockEnabled"] == true, revision: 1}
		if value := args["defaultServerSideEncryption"]; value != nil {
			b.sse = responseSSE(value)
		}
		if b.kind == "" {
			b.kind = "allPrivate"
		}
		if b.rules == nil {
			b.rules = []any{}
		}
		if b.cors == nil {
			b.cors = []any{}
		}
		s.buckets[b.id] = b
		return reply(req, 200, b.response())
	case "b2_list_buckets":
		result := make([]any, 0)
		for _, b := range s.buckets {
			if filter := str(args["bucketId"]); filter != "" && filter != b.id {
				continue
			}
			if filter := str(args["bucketName"]); filter != "" && filter != b.name {
				continue
			}
			if types, ok := args["bucketTypes"].([]any); ok && len(types) > 0 {
				matched := false
				for _, kind := range types {
					if str(kind) == b.kind {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
			}
			result = append(result, b.response())
		}
		sort.Slice(result, func(i, j int) bool {
			return str(result[i].(map[string]any)["bucketName"]) < str(result[j].(map[string]any)["bucketName"])
		})
		return reply(req, 200, map[string]any{"buckets": result})
	case "b2_update_bucket":
		b := s.buckets[str(args["bucketId"])]
		if b == nil {
			return failure(req, 404, "not_found", "bucket not found")
		}
		if revision, present := args["ifRevisionIs"]; present {
			if value, ok := revision.(float64); !ok || value != float64(b.revision) {
				return failure(req, 409, "conflict", "bucket revision does not match")
			}
		}
		if v, ok := args["bucketType"]; ok {
			b.kind = str(v)
		}
		if v, ok := args["bucketInfo"]; ok {
			b.info = argsMap(v)
		}
		if v, ok := args["lifecycleRules"]; ok {
			b.rules = v
		}
		if v, ok := args["corsRules"]; ok {
			b.cors = v
		}
		if v, ok := args["defaultServerSideEncryption"]; ok {
			b.sse = nil
			if v != nil {
				b.sse = responseSSE(v)
			}
		}
		if v, ok := args["defaultRetention"]; ok {
			b.retention = v
		}
		if v, ok := args["replicationConfiguration"]; ok {
			b.replication = v
		}
		if v, ok := args["fileLockEnabled"]; ok {
			b.fileLock = v == true
		}
		b.revision++
		return reply(req, 200, b.response())
	case "b2_delete_bucket":
		b := s.buckets[str(args["bucketId"])]
		if b == nil {
			return failure(req, 404, "not_found", "bucket not found")
		}
		if len(b.versions) != 0 {
			return failure(req, 400, "bad_request", "bucket is not empty")
		}
		for _, upload := range s.large {
			if upload.file.bucketID == b.id {
				return failure(req, 400, "bad_request", "bucket has unfinished large files")
			}
		}
		delete(s.buckets, b.id)
		return reply(req, 200, map[string]any{"bucketId": b.id})
	case "b2_get_upload_url":
		id := str(args["bucketId"])
		if s.buckets[id] == nil {
			return failure(req, 404, "not_found", "bucket not found")
		}
		return reply(req, 200, map[string]any{"uploadUrl": origin + "/upload/file/" + id, "authorizationToken": "b2sim-token"})
	case "b2_start_large_file":
		id := str(args["bucketId"])
		if s.buckets[id] == nil {
			return failure(req, 404, "not_found", "bucket not found")
		}
		fileSSE := args["serverSideEncryption"]
		if fileSSE == nil {
			fileSSE = s.buckets[id].sse
		}
		f := &version{id: s.id("large"), bucketID: id, name: str(args["fileName"]), contentType: str(args["contentType"]), info: textMap(args["fileInfo"]), sse: responseSSE(fileSSE), sha: "none", action: "start", stamp: time.Now().UnixMilli(), retention: args["fileRetention"], legalHold: str(args["legalHold"]), order: s.next}
		s.large[f.id] = &largeFile{file: f, parts: make(map[int][]byte), hashes: make(map[int]string)}
		return reply(req, 200, map[string]any{"fileId": f.id})
	case "b2_get_upload_part_url":
		id := str(args["fileId"])
		if s.large[id] == nil {
			return failure(req, 404, "not_found", "large file not found")
		}
		return reply(req, 200, map[string]any{"uploadUrl": origin + "/upload/part/" + id, "authorizationToken": "b2sim-token"})
	case "b2_finish_large_file":
		return s.finish(req, args)
	case "b2_cancel_large_file":
		id := str(args["fileId"])
		if s.large[id] == nil {
			return failure(req, 404, "not_found", "large file not found")
		}
		delete(s.large, id)
		return reply(req, 200, map[string]any{"fileId": id})
	case "b2_list_file_names", "b2_list_file_versions":
		return s.list(req, method, args)
	case "b2_delete_file_version":
		id := str(args["fileId"])
		for _, b := range s.buckets {
			for i, f := range b.versions {
				if f.id == id && f.name == str(args["fileName"]) {
					b.versions = append(b.versions[:i], b.versions[i+1:]...)
					return reply(req, 200, map[string]any{"fileId": id, "fileName": f.name})
				}
			}
		}
		return failure(req, 404, "not_found", "file version not found")
	case "b2_get_file_info":
		id := str(args["fileId"])
		for _, b := range s.buckets {
			for _, f := range b.versions {
				if f.id == id {
					return reply(req, 200, f.response())
				}
			}
		}
		return failure(req, 404, "not_found", "file not found")
	default:
		return failure(req, http.StatusNotImplemented, "unsupported_operation", "unsupported simulator endpoint: "+method)
	}
}

func argsMap(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func (s *Simulator) upload(req *http.Request, bucketID string, data []byte) (*http.Response, error) {
	if req.Method != http.MethodPost || req.Header.Get("Authorization") != "b2sim-token" {
		return failure(req, 401, "bad_auth_token", "upload authorization required")
	}
	b := s.buckets[bucketID]
	if b == nil {
		return failure(req, 404, "not_found", "bucket not found")
	}
	name, err := url.QueryUnescape(req.Header.Get("X-Bz-File-Name"))
	if err != nil || name == "" {
		return failure(req, 400, "bad_request", "invalid file name")
	}
	data, sha, valid := verifiedUpload(data, req.Header.Get("X-Bz-Content-Sha1"))
	if !valid {
		return failure(req, 400, "bad_request", "content SHA1 mismatch")
	}
	info := make(map[string]string)
	for key, values := range req.Header {
		if strings.HasPrefix(key, "X-Bz-Info-") && len(values) > 0 {
			value, err := url.QueryUnescape(values[0])
			if err != nil {
				return failure(req, 400, "bad_request", "invalid file info")
			}
			info[strings.ToLower(strings.TrimPrefix(key, "X-Bz-Info-"))] = value
		}
	}
	fileSSE := b.sse
	if algorithm := req.Header.Get("X-Bz-Server-Side-Encryption"); algorithm != "" {
		fileSSE = map[string]any{"mode": "SSE-B2", "algorithm": algorithm}
	}
	if algorithm := req.Header.Get("X-Bz-Server-Side-Encryption-Customer-Algorithm"); algorithm != "" {
		if req.Header.Get("X-Bz-Server-Side-Encryption") != "" {
			return failure(req, 400, "bad_request", "conflicting SSE-B2 and SSE-C headers")
		}
		fileSSE = map[string]any{"mode": "SSE-C", "algorithm": algorithm}
	}
	f := &version{id: s.id("file"), bucketID: b.id, name: name, contentType: req.Header.Get("Content-Type"), data: append([]byte(nil), data...), sha: sha, info: info, sse: responseSSE(fileSSE), action: "upload", stamp: time.Now().UnixMilli(), legalHold: req.Header.Get("X-Bz-File-Legal-Hold"), order: s.next}
	if mode := req.Header.Get("X-Bz-File-Retention-Mode"); mode != "" {
		until, err := strconv.ParseInt(req.Header.Get("X-Bz-File-Retention-Retain-Until-Timestamp"), 10, 64)
		if err != nil {
			return failure(req, 400, "bad_request", "invalid retention timestamp")
		}
		f.retention = map[string]any{"mode": mode, "retainUntilTimestamp": until}
	}
	b.versions = append(b.versions, f)
	return reply(req, 200, f.response())
}

func (s *Simulator) uploadPart(req *http.Request, id string, data []byte) (*http.Response, error) {
	if req.Method != http.MethodPost || req.Header.Get("Authorization") != "b2sim-token" {
		return failure(req, 401, "bad_auth_token", "part authorization required")
	}
	l := s.large[id]
	if l == nil {
		return failure(req, 404, "not_found", "large file not found")
	}
	part, err := strconv.Atoi(req.Header.Get("X-Bz-Part-Number"))
	if err != nil || part < 1 || part > 10000 {
		return failure(req, 400, "bad_request", "invalid part number")
	}
	data, sha, valid := verifiedUpload(data, req.Header.Get("X-Bz-Content-Sha1"))
	if !valid {
		return failure(req, 400, "bad_request", "part SHA1 mismatch")
	}
	l.parts[part] = append([]byte(nil), data...)
	l.hashes[part] = sha
	return reply(req, 200, map[string]any{"fileId": id, "partNumber": part, "contentSha1": sha, "contentLength": len(data)})
}

func (s *Simulator) finish(req *http.Request, args map[string]any) (*http.Response, error) {
	id := str(args["fileId"])
	l := s.large[id]
	if l == nil {
		return failure(req, 404, "not_found", "large file not found")
	}
	wants, _ := args["partSha1Array"].([]any)
	if len(wants) != len(l.parts) || len(wants) < 2 {
		return failure(req, 400, "bad_request", "incomplete part array")
	}
	var data []byte
	for i, want := range wants {
		part := i + 1
		piece, exists := l.parts[part]
		if !exists || l.hashes[part] == "" || str(want) != l.hashes[part] {
			return failure(req, 400, "bad_request", "part SHA1 mismatch")
		}
		if i < len(wants)-1 && len(piece) < absoluteMinimumPartSize {
			return failure(req, 400, "bad_request", "non-final part is smaller than the absolute minimum part size")
		}
		data = append(data, piece...)
	}
	f := l.file
	f.data = data
	f.action = "upload"
	f.stamp = time.Now().UnixMilli()
	b := s.buckets[f.bucketID]
	if b == nil {
		return failure(req, 404, "not_found", "bucket not found")
	}
	s.next++
	f.order = s.next
	b.versions = append(b.versions, f)
	delete(s.large, id)
	return reply(req, 200, f.response())
}

func (s *Simulator) list(req *http.Request, method string, args map[string]any) (*http.Response, error) {
	b := s.buckets[str(args["bucketId"])]
	if b == nil {
		return failure(req, 404, "not_found", "bucket not found")
	}
	versions := append([]*version(nil), b.versions...)
	sort.Slice(versions, func(i, j int) bool {
		if versions[i].name == versions[j].name {
			return versions[i].order > versions[j].order
		}
		return versions[i].name < versions[j].name
	})
	result := make([]any, 0)
	seen := make(map[string]bool)
	seenFolders := make(map[string]bool)
	startName, startID := str(args["startFileName"]), str(args["startFileId"])
	prefix, delimiter := str(args["prefix"]), str(args["delimiter"])
	started := startID == ""
	for _, f := range versions {
		if f.name < startName {
			continue
		}
		if !started {
			if f.name == startName && f.id != startID {
				continue
			}
			started = true
		}
		if method == "b2_list_file_names" {
			if seen[f.name] {
				continue
			}
			seen[f.name] = true
		}
		if prefix != "" && !strings.HasPrefix(f.name, prefix) {
			continue
		}
		if delimiter != "" {
			if at := strings.Index(strings.TrimPrefix(f.name, prefix), delimiter); at >= 0 {
				folderName := f.name[:len(prefix)+at+len(delimiter)]
				if seenFolders[folderName] {
					continue
				}
				seenFolders[folderName] = true
				folder := *f
				folder.name, folder.id, folder.action = folderName, "folder:"+folderName, "folder"
				folder.data, folder.sha, folder.info = nil, "none", nil
				result = append(result, folder.response())
				continue
			}
		}
		result = append(result, f.response())
	}
	count := num(args["maxFileCount"])
	if count > 0 && len(result) > count {
		next := result[count].(map[string]any)
		result = result[:count]
		if method == "b2_list_file_names" {
			return reply(req, 200, map[string]any{"files": result, "nextFileName": next["fileName"]})
		}
		return reply(req, 200, map[string]any{"files": result, "nextFileName": next["fileName"], "nextFileId": next["fileId"]})
	}
	if method == "b2_list_file_names" {
		return reply(req, 200, map[string]any{"files": result, "nextFileName": nil})
	}
	return reply(req, 200, map[string]any{"files": result, "nextFileName": nil, "nextFileId": nil})
}

func (s *Simulator) download(req *http.Request) (*http.Response, error) {
	if (req.Method != http.MethodGet && req.Method != http.MethodHead) || req.Header.Get("Authorization") != "b2sim-token" {
		return failure(req, 401, "bad_auth_token", "download authorization required")
	}
	parts := strings.SplitN(strings.TrimPrefix(req.URL.EscapedPath(), "/file/"), "/", 2)
	if len(parts) != 2 {
		return failure(req, 404, "not_found", "file not found")
	}
	name, err := url.QueryUnescape(parts[1])
	if err != nil {
		return failure(req, 400, "bad_request", "invalid file name")
	}
	var f *version
	for _, b := range s.buckets {
		if b.name == parts[0] {
			for i := len(b.versions) - 1; i >= 0; i-- {
				if b.versions[i].name == name {
					f = b.versions[i]
					break
				}
			}
		}
	}
	if f == nil {
		return failure(req, 404, "not_found", "file not found")
	}
	data := f.data
	status := 200
	header := make(http.Header)
	if r := req.Header.Get("Range"); r != "" {
		if !strings.HasPrefix(r, "bytes=") {
			return failure(req, 416, "range_not_satisfiable", "invalid range")
		}
		bounds := strings.SplitN(strings.TrimPrefix(r, "bytes="), "-", 2)
		if len(bounds) != 2 {
			return failure(req, 416, "range_not_satisfiable", "invalid range")
		}
		start, err := strconv.Atoi(bounds[0])
		if err != nil || start < 0 || start >= len(data) {
			return failure(req, 416, "range_not_satisfiable", "range beyond end")
		}
		end := len(data) - 1
		if bounds[1] != "" {
			requested, parseErr := strconv.ParseInt(bounds[1], 10, 64)
			if parseErr != nil || requested < int64(start) {
				return failure(req, 416, "range_not_satisfiable", "invalid range")
			}
			if requested < int64(len(data)) {
				end, err = strconv.Atoi(bounds[1])
				if err != nil {
					return failure(req, 416, "range_not_satisfiable", "invalid range")
				}
			}
		}
		data = data[start : end+1]
		status = 206
		header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(f.data)))
	}
	header.Set("X-Bz-File-Id", f.id)
	header.Set("X-Bz-Content-Sha1", f.sha)
	header.Set("X-Bz-File-Name", url.QueryEscape(f.name))
	header.Set("Content-Type", f.contentType)
	for k, v := range f.info {
		header.Set("X-Bz-Info-"+url.QueryEscape(k), url.QueryEscape(v))
	}
	return rawReply(req, status, data, header), nil
}
