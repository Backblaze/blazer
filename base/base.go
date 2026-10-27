// Copyright 2016, the Blazer authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package base provides a very low-level interface on top of the B2 API.
// It is not intended to be used directly.
package base

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Backblaze/blazer/internal/b2types"
	"github.com/Backblaze/blazer/internal/blog"
)

const (
	APIBase          = "https://api.backblazeb2.com"
	DefaultUserAgent = "blazer/0.9.0"
)

type b2err struct {
	msg     string
	method  string
	retry   int
	code    int
	msgCode string
}

func (e b2err) Error() string {
	if e.method == "" {
		return fmt.Sprintf("b2 error: %s", e.msg)
	}
	return fmt.Sprintf("%s: %d: %s", e.method, e.code, e.msg)
}

// Action checks an error and returns a recommended course of action.
func Action(err error) ErrAction {
	e, ok := err.(b2err)
	if !ok {
		return Punt
	}
	if e.retry > 0 {
		// A zero status means the transport failed before an HTTP response arrived.
		if e.code == 0 && (e.method == "b2_upload_file" || e.method == "b2_upload_part") {
			return AttemptNewUpload
		}
		return Retry
	}
	if e.code >= 500 && e.code < 600 && (e.method == "b2_upload_file" || e.method == "b2_upload_part") {
		return AttemptNewUpload
	}
	switch e.code {
	case 401:
		switch e.method {
		case "b2_authorize_account":
			return Punt
		case "b2_upload_file", "b2_upload_part":
			return AttemptNewUpload
		}
		return ReAuthenticate
	case 400:
		// See restic/restic#1207
		if e.method == "b2_upload_file" && strings.HasPrefix(e.msg, "more than one upload using auth token") {
			return AttemptNewUpload
		}
		return Punt
	case 408:
		return AttemptNewUpload
	case 429, 500, 503:
		return Retry
	}
	return Punt
}

// ErrAction is an action that a caller can take when any function returns an
// error.
type ErrAction int

// Code returns the error code and message.
func Code(err error) (int, string) {
	e, ok := err.(b2err)
	if !ok {
		return 0, ""
	}
	return e.code, e.msg
}

// MsgCode returns the error code, msgCode and message.
func MsgCode(err error) (int, string, string) {
	e, ok := err.(b2err)
	if !ok {
		return 0, "", ""
	}
	return e.code, e.msgCode, e.msg
}

const (
	// ReAuthenticate indicates that the B2 account authentication tokens have
	// expired, and should be refreshed with a new call to AuthorizeAccount.
	ReAuthenticate ErrAction = iota

	// AttemptNewUpload indicates that an upload's authentication token (or URL
	// endpoint) has expired, and that users should request new ones with a call
	// to GetUploadURL or GetUploadPartURL.
	AttemptNewUpload

	// Retry indicates that the caller should wait an appropriate amount of time,
	// and then reattempt the RPC.
	Retry

	// Punt means that there is no useful action to be taken on this error, and
	// that it should be displayed to the user.
	Punt
)

func mkErr(resp *http.Response) error {
	data, err := ioutil.ReadAll(resp.Body)
	var msgBody string
	if err != nil {
		msgBody = fmt.Sprintf("couldn't read message body: %v", err)
	}
	logResponse(resp, data)
	msg := &b2types.ErrorMessage{}
	if err := json.Unmarshal(data, msg); err != nil {
		if msgBody != "" {
			msgBody = fmt.Sprintf("couldn't read message body: %v", err)
		}
	}
	if msgBody == "" {
		msgBody = msg.Msg
	}
	if isDownloadByIDRequest(resp.Request) {
		// Server error text may echo the file ID.
		msgBody = redactDownloadIDText(resp.Request, msgBody)
	}
	var retryAfter int
	retry := resp.Header.Get("Retry-After")
	if retry != "" {
		parsed, err := parseRetryAfterSeconds(retry)
		if err != nil {
			blog.V(1).Info("couldn't parse retry-after header")
		}
		retryAfter = parsed
	}
	return b2err{
		msg:     msgBody,
		retry:   retryAfter,
		code:    resp.StatusCode,
		msgCode: msg.Code,
		method:  resp.Request.Header.Get("X-Blazer-Method"),
	}
}

func parseRetryAfterSeconds(raw string) (int, error) {
	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, err
	}
	if seconds <= 0 {
		return 0, nil
	}
	if seconds >= 30 {
		return 30, nil
	}
	return int(seconds), nil
}

// MaxReuploads returns an appropriate amount of retries for reuploading,
// given a method and an error if any was returned by the server.
func MaxReuploads(err error) uint {
	e, ok := err.(b2err)
	if !ok {
		return 5 // for non b2err errors (e.g. chunk size mismatch), try to reupload 5 times
	}
	if e.method == "b2_upload_file" || e.method == "b2_upload_part" {
		return 5
	}
	return 0
}

// MaxRetries returns an appropriate amount of retries, given a method
// and an error if any was returned by the server.
func MaxRetries(err error) uint {
	e, ok := err.(b2err)
	if !ok {
		return 0 // for non b2err errors, don't retry
	}
	if e.method == "b2_upload_file" || e.method == "b2_upload_part" {
		return 20
	} else if e.method == "b2_download_file_by_id" || e.method == "b2_download_file_by_name" {
		return 20
	}
	return 5
}

// Backoff returns an appropriate amount of time to wait, given an error, if
// any was returned by the server.  If the return value is 0, but Action
// indicates Retry, the user should implement their own exponential backoff,
// beginning with one second.
func Backoff(err error) time.Duration {
	e, ok := err.(b2err)
	if !ok {
		return 0
	}
	// Cap at the same 30s ceiling used by internal/retry.Backoff, so a
	// large or malformed server-supplied Retry-After value can't produce
	// an unbounded single sleep in the retry loop.
	if e.retry > 30 {
		return 30 * time.Second
	}
	return time.Duration(e.retry) * time.Second
}

func logRequest(req *http.Request, args []byte) {
	if !blog.V(2) {
		return
	}
	if isDownloadByIDRequest(req) {
		// The file ID is in the query string. Do not log it or other query
		// parameters that could have been appended to the download URL.
		blog.V(2).Infof(">> b2_download_file_by_id %s [URL and headers redacted]", req.Method)
		return
	}
	var headers []string
	for k := range req.Header {
		if sensitiveLogField(k) || k == "X-Blazer-Method" {
			continue
		}
		headers = append(headers, k)
	}
	hstr := strings.Join(headers, ";")
	method := req.Header.Get("X-Blazer-Method")
	if args != nil {
		blog.V(2).Infof(">> %s %v headers: {%s} args: (%s)", method, req.Method, hstr, redactSensitiveJSON(args))
		return
	}
	blog.V(2).Infof(">> %s %v {%s} (no args)", method, req.Method, hstr)
}

// sensitiveLogField recognizes credentials in both JSON field names and HTTP headers.
func sensitiveLogField(name string) bool {
	name = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(name, "-", ""), "_", ""))
	return strings.Contains(name, "authorization") || strings.Contains(name, "token") ||
		strings.Contains(name, "secret") || strings.Contains(name, "applicationkey") ||
		strings.Contains(name, "customerkey") || strings.Contains(name, "cookie") ||
		strings.Contains(name, "authenticate")
}

func redactSensitiveValue(value interface{}) {
	switch value := value.(type) {
	case map[string]interface{}:
		for key, item := range value {
			if sensitiveLogField(key) {
				value[key] = "[redacted]"
			} else {
				redactSensitiveValue(item)
			}
		}
	case []interface{}:
		for _, item := range value {
			redactSensitiveValue(item)
		}
	}
}

func redactSensitiveJSON(data []byte) string {
	var value interface{}
	if err := json.Unmarshal(data, &value); err != nil {
		return "[unparseable JSON omitted]"
	}
	redactSensitiveValue(value)
	safe, err := json.Marshal(value)
	if err != nil {
		return "[JSON omitted]"
	}
	return string(safe)
}

func redactSSECHeader(name, value string) string {
	if strings.EqualFold(name, "X-Bz-Server-Side-Encryption-Customer-Key") || strings.EqualFold(name, "X-Bz-Server-Side-Encryption-Customer-Key-Md5") {
		return "[REDACTED]"
	}
	return value
}

func redactSSECJSON(args []byte) []byte {
	var value any
	if err := json.Unmarshal(args, &value); err != nil {
		return []byte("[REDACTED]")
	}
	redactSSECFields(value)
	redacted, err := json.Marshal(value)
	if err != nil {
		return []byte("[REDACTED]")
	}
	return redacted
}

func redactSSECFields(value any) {
	switch value := value.(type) {
	case map[string]any:
		for key, nested := range value {
			if strings.EqualFold(key, "customerKey") || strings.EqualFold(key, "customerKeyMd5") {
				value[key] = "[REDACTED]"
				continue
			}
			redactSSECFields(nested)
		}
	case []any:
		for _, nested := range value {
			redactSSECFields(nested)
		}
	}
}

func logResponse(resp *http.Response, reply []byte) {
	if !blog.V(2) {
		return
	}
	if isDownloadByIDRequest(resp.Request) {
		blog.V(2).Infof("<< b2_download_file_by_id status=%d [headers and body redacted]", resp.StatusCode)
		return
	}
	if resp.Request != nil && resp.Request.Header.Get("X-Blazer-Method") == "b2_authorize_account" {
		blog.V(2).Infof("<< b2_authorize_account status=%d [redacted]", resp.StatusCode)
		return
	}
	var headers []string
	for k := range resp.Header {
		if resp.StatusCode >= 400 || sensitiveLogField(k) {
			continue
		}
		headers = append(headers, k)
	}
	hstr := strings.Join(headers, "; ")
	method := resp.Request.Header.Get("X-Blazer-Method")
	id := resp.Request.Header.Get("X-Blazer-Request-ID")
	if resp.StatusCode >= 400 {
		blog.V(2).Infof("<< %s (%s) %d {%s} (error response omitted)", method, id, resp.StatusCode, hstr)
		return
	}
	if reply != nil {
		safe := redactSensitiveJSON(reply)
		blog.V(2).Infof("<< %s (%s) %d {%s} (%s)", method, id, resp.StatusCode, hstr, safe)
		return
	}
	blog.V(2).Infof("<< %s (%s) %d {%s} (no reply)", method, id, resp.StatusCode, hstr)
}

func isDownloadByIDRequest(req *http.Request) bool {
	return req != nil && req.Header.Get("X-Blazer-Method") == "b2_download_file_by_id"
}

func redactDownloadIDText(req *http.Request, value string) string {
	if !isDownloadByIDRequest(req) {
		return value
	}
	id := req.URL.Query().Get("fileId")
	if id != "" {
		for _, secret := range []string{req.URL.String(), url.QueryEscape(id), url.PathEscape(id), id} {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	for _, header := range []string{"X-Bz-Server-Side-Encryption-Customer-Key", "X-Bz-Server-Side-Encryption-Customer-Key-Md5"} {
		secret := req.Header.Get(header)
		if secret != "" {
			value = strings.ReplaceAll(value, url.QueryEscape(secret), "[REDACTED]")
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}

func millitime(t int64) time.Time {
	return time.Unix(t/1000, t%1000*1e6)
}

type b2Options struct {
	transport       http.RoundTripper
	failSomeUploads bool
	expireTokens    bool
	capExceeded     bool
	apiBase         string
	userAgent       string
}

func (o *b2Options) addHeaders(req *http.Request) {
	if o.failSomeUploads {
		req.Header.Add("X-Bz-Test-Mode", "fail_some_uploads")
	}
	if o.expireTokens {
		req.Header.Add("X-Bz-Test-Mode", "expire_some_account_authorization_tokens")
	}
	if o.capExceeded {
		req.Header.Add("X-Bz-Test-Mode", "force_cap_exceeded")
	}
	req.Header.Set("User-Agent", o.getUserAgent())
}

func (o *b2Options) getAPIBase() string {
	if o.apiBase != "" {
		return o.apiBase
	}
	return APIBase
}

func (o *b2Options) getUserAgent() string {
	if o.userAgent != "" {
		return fmt.Sprintf("%s %s", o.userAgent, DefaultUserAgent)
	}
	return DefaultUserAgent
}

func (o *b2Options) getTransport() http.RoundTripper {
	if o.transport == nil {
		return http.DefaultTransport
	}
	return o.transport
}

// B2 holds account information for Backblaze.
type B2 struct {
	mu                  sync.RWMutex // guards the authorization fields below
	accountID           string
	authToken           string
	apiURI              string
	s3URI               string
	downloadURI         string
	minPartSize         int
	recommendedPartSize int
	opts                *b2Options
	buckets             []string // restricted to these buckets if non-empty
	pfx                 string   // restricted to objects with this prefix if present
	capabilities        []string
}

// AuthData is the authorization state needed to resume a B2 session. It does
// not contain the application key used to obtain or refresh the session.
type AuthData struct {
	ApplicationKeyID    string
	AccountID           string
	AuthToken           string
	APIURL              string
	DownloadURL         string
	S3URL               string
	AbsMinPartSize      int
	RecommendedPartSize int
	AllowedBuckets      []string
	AllowedPrefix       string
	AllowedCapabilities []string
}

// AuthData returns a detached snapshot of the current authorization state.
func (b *B2) AuthData() AuthData {
	state := b.snapshot()
	return AuthData{
		AccountID: state.accountID, AuthToken: state.authToken, APIURL: state.apiURI,
		DownloadURL: state.downloadURI, S3URL: state.s3URI,
		AbsMinPartSize: state.minPartSize, RecommendedPartSize: state.recommendedPartSize,
		AllowedBuckets: append([]string(nil), state.buckets...), AllowedPrefix: state.pfx,
		AllowedCapabilities: append([]string(nil), state.capabilities...),
	}
}

// NewAuthorizedAccount restores a session while applying this client's own
// transport, user agent, and other request options.
func NewAuthorizedAccount(data AuthData, opts ...AuthOption) *B2 {
	b2opts := &b2Options{}
	for _, option := range opts {
		option(b2opts)
	}
	return &B2{
		accountID: data.AccountID, authToken: data.AuthToken, apiURI: data.APIURL,
		downloadURI: data.DownloadURL, s3URI: data.S3URL,
		minPartSize: data.AbsMinPartSize, recommendedPartSize: data.RecommendedPartSize,
		buckets: append([]string(nil), data.AllowedBuckets...), pfx: data.AllowedPrefix,
		capabilities: append([]string(nil), data.AllowedCapabilities...), opts: b2opts,
	}
}

// authState is a detached snapshot. A request uses one value for its endpoint,
// token, account scope and options, without holding a lock during network I/O.
type authState struct {
	accountID, authToken, apiURI, s3URI, downloadURI string
	minPartSize, recommendedPartSize                 int
	opts                                             *b2Options
	buckets, capabilities                            []string
	pfx                                              string
}

func (b *B2) snapshot() authState {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return authState{
		accountID: b.accountID, authToken: b.authToken, apiURI: b.apiURI,
		s3URI: b.s3URI, downloadURI: b.downloadURI,
		minPartSize: b.minPartSize, recommendedPartSize: b.recommendedPartSize,
		opts: b.opts, buckets: b.buckets, pfx: b.pfx, capabilities: b.capabilities,
	}
}

// Update replaces the B2 object with a new one, in-place.
func (b *B2) Update(n *B2) {
	state := n.snapshot()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.accountID = state.accountID
	b.authToken = state.authToken
	b.apiURI = state.apiURI
	b.s3URI = state.s3URI
	b.downloadURI = state.downloadURI
	b.minPartSize = state.minPartSize
	b.recommendedPartSize = state.recommendedPartSize
	b.opts = state.opts
	b.buckets = state.buckets
	b.pfx = state.pfx
	b.capabilities = state.capabilities
}

type httpReply struct {
	resp *http.Response
	err  error
}

func makeNetRequest(ctx context.Context, req *http.Request, rt http.RoundTripper) (*http.Response, error) {
	req = req.WithContext(ctx)
	resp, err := rt.RoundTrip(req)
	switch err {
	case nil:
		return resp, nil
	case context.Canceled, context.DeadlineExceeded:
		return nil, err
	default:
		method := req.Header.Get("X-Blazer-Method")
		blog.V(2).Infof(">> %s transport error: %T [URL and error text redacted]", method, err)
		// Only an upload fault is classified by method (see Action). Other
		// transport errors stay method-less so their retry budget and message
		// do not change.
		if method != "b2_upload_file" && method != "b2_upload_part" {
			method = ""
		}
		// Certificate verification failures are permanent, including distrust of
		// compromised CAs. Preserve the original error instead of retrying or
		// requesting a new upload URL.
		var verificationErr *tls.CertificateVerificationError
		var unknownAuthorityErr x509.UnknownAuthorityError
		var invalidCertErr x509.CertificateInvalidError
		var hostnameErr x509.HostnameError
		if errors.As(err, &verificationErr) ||
			errors.As(err, &unknownAuthorityErr) ||
			errors.As(err, &invalidCertErr) ||
			errors.As(err, &hostnameErr) {
			return nil, err
		}

		// A transport error may include the full URL. Preserve ordinary error
		// details while removing the file ID before a caller can log it.
		message := redactDownloadIDText(req, err.Error())
		return nil, b2err{
			msg:    message,
			method: method,
			retry:  1,
		}
	}
}

type requestBody struct {
	size int64
	body io.Reader
}

func (rb *requestBody) getSize() int64 {
	if rb == nil {
		return 0
	}
	return rb.size
}

func (rb *requestBody) getBody() io.Reader {
	if rb == nil {
		return nil
	}
	if rb.getSize() == 0 {
		// https://github.com/kurin/blazer/issues/57
		// When body is non-nil, but the request's ContentLength is 0, it is
		// replaced with -1, which causes the client to send a chunked encoding,
		// which confuses B2.
		return http.NoBody
	}
	return rb.body
}

type keepFinalBytes struct {
	r      io.Reader
	remain int
	sha    [40]byte
}

func (k *keepFinalBytes) Read(p []byte) (int, error) {
	n, err := k.r.Read(p)
	if k.remain-n > 40 {
		k.remain -= n
		return n, err
	}
	// This was a whole lot harder than it looks.
	pi := -40 + k.remain
	if pi < 0 {
		pi = 0
	}
	pe := n
	ki := 40 - k.remain
	if ki < 0 {
		ki = 0
	}
	ke := n - k.remain + 40
	copy(k.sha[ki:ke], p[pi:pe])
	k.remain -= n
	return n, err
}

var reqID int64

func (o *b2Options) makeRequest(ctx context.Context, method, verb, uri string, b2req, b2resp interface{}, headers map[string]string, body *requestBody) error {
	var args []byte
	if b2req != nil {
		enc, err := json.Marshal(b2req)
		if err != nil {
			return err
		}
		args = enc
		body = &requestBody{
			body: bytes.NewBuffer(enc),
			size: int64(len(enc)),
		}
	}
	req, err := http.NewRequest(verb, uri, body.getBody())
	if err != nil {
		return err
	}
	req.ContentLength = body.getSize()
	for k, v := range headers {
		if strings.HasPrefix(k, "X-Bz-Info") || strings.HasPrefix(k, "X-Bz-File-Name") {
			v = escape(v)
		}
		req.Header.Set(k, v)
	}
	req.Header.Set("X-Blazer-Request-ID", fmt.Sprintf("%d", atomic.AddInt64(&reqID, 1)))
	req.Header.Set("X-Blazer-Method", method)
	o.addHeaders(req)
	logRequest(req, args)
	resp, err := makeNetRequest(ctx, req, o.getTransport())
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return mkErr(resp)
	}
	var replyArgs []byte
	if b2resp != nil {
		rbuf := &bytes.Buffer{}
		r := io.TeeReader(resp.Body, rbuf)
		decoder := json.NewDecoder(r)
		if err := decoder.Decode(b2resp); err != nil {
			return err
		}
		replyArgs = rbuf.Bytes()
	} else {
		ra, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			blog.V(1).Infof("%s: couldn't read response body", method)
		}
		replyArgs = ra
	}
	logResponse(resp, replyArgs)
	return nil
}

// AuthorizeAccount wraps b2_authorize_account.
func AuthorizeAccount(ctx context.Context, account, key string, opts ...AuthOption) (*B2, error) {
	auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", account, key)))
	b2resp := &b2types.AuthorizeAccountResponse{}
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Basic %s", auth),
	}
	b2opts := &b2Options{}
	for _, f := range opts {
		f(b2opts)
	}
	if err := b2opts.makeRequest(ctx, "b2_authorize_account", "GET", b2opts.getAPIBase()+b2types.V4api+"b2_authorize_account", nil, b2resp, headers, nil); err != nil {
		return nil, err
	}
	storageAPI := b2resp.APIInfo.StorageAPIInfo
	// A restricted key's scope lives under storageApi.allowed. Master keys
	// carry an allowed with null buckets and namePrefix, leaving buckets and
	// pfx empty; the nil check also tolerates an absent allowed.
	var buckets []string
	var pfx string
	if allowed := storageAPI.Allowed; allowed != nil {
		for _, b := range allowed.Buckets {
			buckets = append(buckets, b.ID)
		}
		pfx = allowed.Prefix
	}
	return &B2{
		accountID:           b2resp.AccountID,
		authToken:           b2resp.AuthToken,
		apiURI:              storageAPI.URI,
		s3URI:               storageAPI.S3URI,
		downloadURI:         storageAPI.DownloadURI,
		minPartSize:         storageAPI.AbsMinPartSize,
		recommendedPartSize: storageAPI.PartSize,
		buckets:             buckets,
		pfx:                 pfx,
		capabilities:        append([]string(nil), storageAPI.Capabilities...),
		opts:                b2opts,
	}, nil
}

// An AuthOption allows callers to choose per-session settings.
type AuthOption func(*b2Options)

// UserAgent sets the User-Agent HTTP header.  The default header is
// "blazer/<version>"; the value set here will be prepended to that.  This can
// be set multiple times.
func UserAgent(agent string) AuthOption {
	return func(o *b2Options) {
		if o.userAgent == "" {
			o.userAgent = agent
			return
		}
		o.userAgent = fmt.Sprintf("%s %s", agent, o.userAgent)
	}
}

// Transport returns an AuthOption that sets the underlying HTTP mechanism.
func Transport(rt http.RoundTripper) AuthOption {
	return func(o *b2Options) {
		o.transport = rt
	}
}

// FailSomeUploads requests intermittent upload failures from the B2 service by
// sending B2's documented X-Bz-Test-Mode: fail_some_uploads header. It is for
// testing retry and error-handling paths only; do not use it in production.
func FailSomeUploads() AuthOption {
	return func(o *b2Options) {
		o.failSomeUploads = true
	}
}

// ExpireSomeAuthTokens requests intermittent authentication failures from the
// B2 service by sending B2's documented
// X-Bz-Test-Mode: expire_some_account_authorization_tokens header. It is for
// testing re-authentication paths only; do not use it in production.
func ExpireSomeAuthTokens() AuthOption {
	return func(o *b2Options) {
		o.expireTokens = true
	}
}

// ForceCapExceeded requests a cap limit from the B2 service by sending B2's
// documented X-Bz-Test-Mode: force_cap_exceeded header. This causes all uploads
// to be treated as if they would exceed the configured B2 capacity. It is for
// testing only; do not use it in production.
func ForceCapExceeded() AuthOption {
	return func(o *b2Options) {
		o.capExceeded = true
	}
}

// SetAPIBase returns an AuthOption that uses the given URL as the base for API
// requests.
func SetAPIBase(url string) AuthOption {
	return func(o *b2Options) {
		o.apiBase = url
	}
}

type LifecycleRule struct {
	Prefix                 string
	DaysNewUntilHidden     int
	DaysHiddenUntilDeleted int
}

// decodeDefaultRetention returns the default retention from a bucket's
// fileLockConfiguration, or nil when the configuration is absent (the key may
// not be allowed to read it) or no default retention is set.
func decodeDefaultRetention(cfg *b2types.FileLockConfiguration) *b2types.Retention {
	if cfg == nil {
		return nil
	}
	dr := cfg.Val.DefaultRetention
	if dr.Mode == nil || dr.Period.Unit == nil {
		return nil
	}
	return &b2types.Retention{
		Mode: *dr.Mode,
		Period: &b2types.RetentionPeriod{
			Duration: dr.Period.Duration,
			Unit:     *dr.Period.Unit,
		},
	}
}

func normalizeDefaultServerSideEncryption(sse *b2types.ServerSideEncryption) *b2types.ServerSideEncryption {
	if sse == nil || sse.Mode == "" {
		return nil
	}
	return sse
}

// CreateBucket wraps b2_create_bucket. A nil sse leaves the bucket's default
// server-side encryption to the server.
func (b *B2) CreateBucket(ctx context.Context, name, btype string, info map[string]string, rules []LifecycleRule, sse *b2types.ServerSideEncryption, corsRules []b2types.CORSRule, fileLockEnabled bool, replicationConfiguration *b2types.ReplicationConfiguration) (*Bucket, error) {
	state := b.snapshot()
	if btype != "allPublic" {
		btype = "allPrivate"
	}
	var b2rules []b2types.LifecycleRule
	for _, rule := range rules {
		b2rules = append(b2rules, b2types.LifecycleRule{
			Prefix:                 rule.Prefix,
			DaysNewUntilHidden:     rule.DaysNewUntilHidden,
			DaysHiddenUntilDeleted: rule.DaysHiddenUntilDeleted,
		})
	}
	b2req := &b2types.CreateBucketRequest{
		AccountID:      state.accountID,
		Name:           name,
		Type:           btype,
		Info:           info,
		LifecycleRules: b2rules,

		DefaultServerSideEncryption: normalizeDefaultServerSideEncryption(sse),
		CORSRules:                   corsRules,
		FileLockEnabled:             fileLockEnabled,
		ReplicationConfiguration:    replicationConfiguration,
	}
	b2resp := &b2types.CreateBucketResponse{}
	headers := map[string]string{
		"Authorization": state.authToken,
	}
	if err := state.opts.makeRequest(ctx, "b2_create_bucket", "POST", state.apiURI+b2types.V4api+"b2_create_bucket", b2req, b2resp, headers, nil); err != nil {
		return nil, err
	}
	var respRules []LifecycleRule
	for _, rule := range b2resp.LifecycleRules {
		respRules = append(respRules, LifecycleRule{
			Prefix:                 rule.Prefix,
			DaysNewUntilHidden:     rule.DaysNewUntilHidden,
			DaysHiddenUntilDeleted: rule.DaysHiddenUntilDeleted,
		})
	}
	bucket := &Bucket{
		Name:                        name,
		Info:                        b2resp.Info,
		LifecycleRules:              respRules,
		ID:                          b2resp.BucketID,
		rev:                         b2resp.Revision,
		b2:                          b,
		CORSRules:                   b2resp.CORSRules,
		DefaultServerSideEncryption: normalizeDefaultServerSideEncryption(b2resp.DefaultServerSideEncryption.Value),
	}
	if b2resp.FileLockConfig != nil {
		bucket.FileLockEnabled = b2resp.FileLockConfig.Val.IsFileLockEnabled
	}
	bucket.DefaultRetention = decodeDefaultRetention(b2resp.FileLockConfig)
	if cfg := b2resp.ReplicationConfiguration; cfg != nil && cfg.IsClientAuthorizedToRead && cfg.Value != nil &&
		(cfg.Value.AsReplicationSource != nil || cfg.Value.AsReplicationDestination != nil) {
		bucket.ReplicationConfiguration = cfg.Value
	}
	return bucket, nil
}

// DeleteBucket wraps b2_delete_bucket.
func (b *Bucket) DeleteBucket(ctx context.Context) error {
	state := b.b2.snapshot()
	b2req := &b2types.DeleteBucketRequest{
		AccountID: state.accountID,
		BucketID:  b.ID,
	}
	headers := map[string]string{
		"Authorization": state.authToken,
	}
	return state.opts.makeRequest(ctx, "b2_delete_bucket", "POST", state.apiURI+b2types.V4api+"b2_delete_bucket", b2req, nil, headers, nil)
}

// Bucket holds B2 bucket details.
type Bucket struct {
	Name           string
	Type           string
	Info           map[string]string
	LifecycleRules []LifecycleRule
	ID             string
	rev            int
	b2             *B2

	CORSRules        []b2types.CORSRule
	DefaultRetention *b2types.Retention
	// DefaultServerSideEncryption is the bucket's default encryption for new
	// files. It is nil when the key may not read it, and a nil value is left
	// out of Update so the server keeps whatever it has.
	DefaultServerSideEncryption *b2types.ServerSideEncryption
	FileLockEnabled             bool
	ReplicationConfiguration    *b2types.ReplicationConfiguration
	// ReplicationConfigurationUnreadable records a list response that withheld
	// replication settings. Callers must not replace a config they cannot read.
	ReplicationConfigurationUnreadable bool
}

// Update wraps b2_update_bucket.
func (b *Bucket) Update(ctx context.Context) (*Bucket, error) {
	state := b.b2.snapshot()
	// Only a non-nil list is sent: nil leaves lifecycle rules unchanged, and an
	// empty non-nil list removes them all.
	var rules *[]b2types.LifecycleRule
	if b.LifecycleRules != nil {
		rs := make([]b2types.LifecycleRule, 0, len(b.LifecycleRules))
		for _, rule := range b.LifecycleRules {
			rs = append(rs, b2types.LifecycleRule{
				DaysNewUntilHidden:     rule.DaysNewUntilHidden,
				DaysHiddenUntilDeleted: rule.DaysHiddenUntilDeleted,
				Prefix:                 rule.Prefix,
			})
		}
		rules = &rs
	}
	var info *map[string]string
	if b.Info != nil {
		info = &b.Info
	}
	b2req := &b2types.UpdateBucketRequest{
		AccountID: state.accountID,
		BucketID:  b.ID,
		// Name:           b.Name,
		Type:           b.Type,
		Info:           info,
		LifecycleRules: rules,
		IfRevisionIs:   b.rev,

		CORSRules:                   b.CORSRules,
		DefaultRetention:            b.DefaultRetention,
		DefaultServerSideEncryption: normalizeDefaultServerSideEncryption(b.DefaultServerSideEncryption),
		FileLockEnabled:             b.FileLockEnabled,
		ReplicationConfiguration:    b.ReplicationConfiguration,
	}
	headers := map[string]string{
		"Authorization": state.authToken,
	}
	b2resp := &b2types.UpdateBucketResponse{}
	if err := state.opts.makeRequest(ctx, "b2_update_bucket", "POST", state.apiURI+b2types.V4api+"b2_update_bucket", b2req, b2resp, headers, nil); err != nil {
		return nil, err
	}
	var respRules []LifecycleRule
	for _, rule := range b2resp.LifecycleRules {
		respRules = append(respRules, LifecycleRule{
			Prefix:                 rule.Prefix,
			DaysNewUntilHidden:     rule.DaysNewUntilHidden,
			DaysHiddenUntilDeleted: rule.DaysHiddenUntilDeleted,
		})
	}
	updated := &Bucket{
		Name:                        b.Name,
		Type:                        b2resp.Type,
		Info:                        b2resp.Info,
		LifecycleRules:              respRules,
		ID:                          b2resp.BucketID,
		b2:                          b.b2,
		CORSRules:                   b2resp.CORSRules,
		DefaultServerSideEncryption: normalizeDefaultServerSideEncryption(b2resp.DefaultServerSideEncryption.Value),
		FileLockEnabled:             b2resp.FileLockConfig.Val.IsFileLockEnabled,
		ReplicationConfiguration:    b2resp.ReplicationConfiguration.Value,
	}
	updated.DefaultRetention = decodeDefaultRetention(b2resp.FileLockConfig)

	return updated, nil
}

// BaseURL returns the base part of the download URLs.
func (b *Bucket) BaseURL() string {
	state := b.b2.snapshot()
	return state.downloadURI
}

// S3URL returns the base URL for S3-compatible API calls.
func (b *Bucket) S3URL() string {
	state := b.b2.snapshot()
	return state.s3URI
}

// NotificationRules wraps b2_get_bucket_notification_rules.
func (b *Bucket) NotificationRules(ctx context.Context) ([]b2types.EventNotificationRule, error) {
	state := b.b2.snapshot()
	query := url.Values{"bucketId": []string{b.ID}}
	b2resp := &b2types.GetBucketNotificationRulesResponse{}
	headers := map[string]string{
		"Authorization": state.authToken,
	}
	endpoint := state.apiURI + b2types.V4api + "b2_get_bucket_notification_rules?" + query.Encode()
	if err := state.opts.makeRequest(ctx, "b2_get_bucket_notification_rules", "GET", endpoint, nil, b2resp, headers, nil); err != nil {
		return nil, err
	}
	return b2resp.EventNotificationRules, nil
}

// SetNotificationRules replaces a bucket's complete notification rule set.
func (b *Bucket) SetNotificationRules(ctx context.Context, rules []b2types.EventNotificationRule) error {
	state := b.b2.snapshot()
	// A non-nil empty array clears rules. B2 does not treat null as an empty
	// rule set, so allocate even when the caller supplied an empty slice.
	b2rules := make([]b2types.EventNotificationRule, len(rules))
	copy(b2rules, rules)
	b2req := &b2types.SetBucketNotificationRulesRequest{
		BucketID:               b.ID,
		EventNotificationRules: b2rules,
	}
	b2resp := &b2types.SetBucketNotificationRulesResponse{}
	headers := map[string]string{
		"Authorization": state.authToken,
	}
	return state.opts.makeRequest(ctx, "b2_set_bucket_notification_rules", "POST", state.apiURI+b2types.V4api+"b2_set_bucket_notification_rules", b2req, b2resp, headers, nil)
}

// ListBuckets wraps b2_list_buckets.  If name is non-empty, only that bucket
// will be returned if it exists; else nothing will be returned.
func (b *B2) ListBuckets(ctx context.Context, name string, bucketTypes ...string) ([]*Bucket, error) {
	state := b.snapshot()
	// b2_list_buckets does not narrow results to a restricted key's scope: it
	// rejects any unfiltered request from such a key with 401, and accepts at
	// most one bucketId filter per request. A single-bucket key can send its
	// bucket as the filter; a multi-bucket key with no name filter has to fan
	// out one request per allowed bucket and merge the results.
	if name == "" && len(state.buckets) > 1 {
		var buckets []*Bucket
		for _, id := range state.buckets {
			bs, err := b.listBucketsWithState(ctx, state, id, "", bucketTypes)
			if err != nil {
				return nil, err
			}
			buckets = append(buckets, bs...)
		}
		return buckets, nil
	}
	var filterBucketID string
	if len(state.buckets) == 1 {
		filterBucketID = state.buckets[0]
	}
	return b.listBucketsWithState(ctx, state, filterBucketID, name, bucketTypes)
}

func (b *B2) listBucketsWithState(ctx context.Context, state authState, bucketID, name string, bucketTypes []string) ([]*Bucket, error) {
	b2req := &b2types.ListBucketsRequest{
		AccountID:   state.accountID,
		Bucket:      bucketID,
		Name:        name,
		BucketTypes: bucketTypes,
	}
	b2resp := &b2types.ListBucketsResponse{}
	headers := map[string]string{
		"Authorization": state.authToken,
	}
	if err := state.opts.makeRequest(ctx, "b2_list_buckets", "POST", state.apiURI+b2types.V4api+"b2_list_buckets", b2req, b2resp, headers, nil); err != nil {
		return nil, err
	}
	var buckets []*Bucket
	for _, bucket := range b2resp.Buckets {
		var rules []LifecycleRule
		for _, rule := range bucket.LifecycleRules {
			rules = append(rules, LifecycleRule{
				Prefix:                 rule.Prefix,
				DaysNewUntilHidden:     rule.DaysNewUntilHidden,
				DaysHiddenUntilDeleted: rule.DaysHiddenUntilDeleted,
			})
		}
		listed := &Bucket{
			Name:                        bucket.Name,
			Type:                        bucket.Type,
			Info:                        bucket.Info,
			LifecycleRules:              rules,
			ID:                          bucket.BucketID,
			rev:                         bucket.Revision,
			b2:                          b,
			CORSRules:                   bucket.CORSRules,
			DefaultServerSideEncryption: normalizeDefaultServerSideEncryption(bucket.DefaultServerSideEncryption.Value),
		}
		if bucket.FileLockConfig != nil {
			listed.FileLockEnabled = bucket.FileLockConfig.Val.IsFileLockEnabled
		}
		listed.DefaultRetention = decodeDefaultRetention(bucket.FileLockConfig)
		if cfg := bucket.ReplicationConfiguration; cfg != nil && !cfg.IsClientAuthorizedToRead {
			listed.ReplicationConfigurationUnreadable = true
		}
		if cfg := bucket.ReplicationConfiguration; cfg != nil && cfg.IsClientAuthorizedToRead && cfg.Value != nil &&
			(cfg.Value.AsReplicationSource != nil || cfg.Value.AsReplicationDestination != nil) {
			listed.ReplicationConfiguration = cfg.Value
		}
		buckets = append(buckets, listed)
	}
	return buckets, nil
}

// URL holds information from the b2_get_upload_url API.
type URL struct {
	uri    string
	token  string
	b2     *B2
	bucket *Bucket
}

// Reload reloads URL in-place, by reissuing a b2_get_upload_url and
// overwriting the previous values.
func (url *URL) Reload(ctx context.Context) error {
	n, err := url.bucket.GetUploadURL(ctx)
	if err != nil {
		return err
	}
	url.uri = n.uri
	url.token = n.token
	return nil
}

// GetUploadURL wraps b2_get_upload_url.
func (b *Bucket) GetUploadURL(ctx context.Context) (*URL, error) {
	state := b.b2.snapshot()
	b2req := &b2types.GetUploadURLRequest{
		BucketID: b.ID,
	}
	b2resp := &b2types.GetUploadURLResponse{}
	headers := map[string]string{
		"Authorization": state.authToken,
	}
	if err := state.opts.makeRequest(ctx, "b2_get_upload_url", "POST", state.apiURI+b2types.V4api+"b2_get_upload_url", b2req, b2resp, headers, nil); err != nil {
		return nil, err
	}
	return &URL{
		uri:    b2resp.URI,
		token:  b2resp.Token,
		b2:     b.b2,
		bucket: b,
	}, nil
}

// File represents a B2 file.
type File struct {
	Name      string
	Size      int64
	Status    string
	Timestamp time.Time
	Info      *FileInfo
	ID        string
	b2        *B2
}

// File returns a bare File struct, but with the appropriate id and b2
// interfaces.
func (b *Bucket) File(id, name string) *File {
	return &File{
		Name:   name,
		Status: "upload", // Default to regular file
		ID:     id,
		b2:     b.b2,
	}
}

// FileRetention is per-file Object Lock retention: a mode ("governance" or
// "compliance") and a retain-until time in milliseconds since the Unix epoch.
type FileRetention struct {
	Mode                 string
	RetainUntilTimestamp int64
}

// FileLock holds the optional Object Lock settings of a file. The zero value
// sets neither.
type FileLock struct {
	Retention *FileRetention
	LegalHold string // "on", "off" or "" for unset
}

func addServerSideEncryptionHeaders(headers map[string]string, sse *b2types.ServerSideEncryption) error {
	if sse == nil {
		return nil
	}
	hasSSEB2 := strings.EqualFold(sse.Mode, "SSE-B2") || headers["X-Bz-Server-Side-Encryption"] != ""
	hasSSEC := strings.EqualFold(sse.Mode, "SSE-C") || sse.CustomerKey != "" || sse.CustomerKeyMD5 != ""
	if hasSSEB2 && hasSSEC {
		return errors.New("b2: SSE-B2 and SSE-C cannot be used on the same request")
	}
	if sse.Algorithm != "AES256" {
		return errors.New("b2: server-side encryption algorithm must be AES256")
	}
	switch {
	case hasSSEC:
		if sse.CustomerKey == "" || sse.CustomerKeyMD5 == "" {
			return errors.New("b2: SSE-C requires a customer key and its MD5 digest")
		}
		headers["X-Bz-Server-Side-Encryption-Customer-Algorithm"] = sse.Algorithm
		headers["X-Bz-Server-Side-Encryption-Customer-Key"] = sse.CustomerKey
		headers["X-Bz-Server-Side-Encryption-Customer-Key-Md5"] = sse.CustomerKeyMD5
	case hasSSEB2:
		headers["X-Bz-Server-Side-Encryption"] = sse.Algorithm
	default:
		return errors.New("b2: unknown server-side encryption mode")
	}
	return nil
}

func (l FileLock) retentionRequest() *b2types.FileRetention {
	if l.Retention == nil {
		return nil
	}
	return &b2types.FileRetention{Mode: &l.Retention.Mode, RetainUntilTimestamp: &l.Retention.RetainUntilTimestamp}
}

// UploadFile wraps b2_upload_file. At most one FileLock may be given; it sets
// Object Lock retention and legal hold on the new file.
func (url *URL) UploadFile(ctx context.Context, r io.Reader, size int, name, contentType, sha1 string, info map[string]string, lock ...FileLock) (*File, error) {
	return url.UploadFileWithSSEC(ctx, r, size, name, contentType, sha1, info, nil, lock...)
}

// UploadFileWithSSEC wraps b2_upload_file and sends per-request server-side
// encryption settings. Callers normally use package b2 instead of this
// low-level method.
func (url *URL) UploadFileWithSSEC(ctx context.Context, r io.Reader, size int, name, contentType, sha1 string, info map[string]string, sse *b2types.ServerSideEncryption, lock ...FileLock) (*File, error) {
	state := url.b2.snapshot()
	headers := map[string]string{
		"Authorization":     url.token,
		"X-Bz-File-Name":    name,
		"Content-Type":      contentType,
		"Content-Length":    fmt.Sprintf("%d", size),
		"X-Bz-Content-Sha1": sha1,
	}
	for k, v := range info {
		headers[fmt.Sprintf("X-Bz-Info-%s", k)] = v
	}
	if len(lock) > 0 {
		if rt := lock[0].Retention; rt != nil {
			headers["X-Bz-File-Retention-Mode"] = rt.Mode
			headers["X-Bz-File-Retention-Retain-Until-Timestamp"] = fmt.Sprintf("%d", rt.RetainUntilTimestamp)
		}
		if lock[0].LegalHold != "" {
			headers["X-Bz-File-Legal-Hold"] = lock[0].LegalHold
		}
	}
	if err := addServerSideEncryptionHeaders(headers, sse); err != nil {
		return nil, err
	}
	b2resp := &b2types.UploadFileResponse{}
	if err := state.opts.makeRequest(ctx, "b2_upload_file", "POST", url.uri, nil, b2resp, headers, &requestBody{body: r, size: int64(size)}); err != nil {
		return nil, err
	}
	return &File{
		Name:      name,
		Size:      int64(size),
		Timestamp: millitime(b2resp.Timestamp),
		Status:    b2resp.Action,
		ID:        b2resp.FileID,
		b2:        url.b2,
	}, nil
}

// DeleteFileVersion wraps b2_delete_file_version. Passing true requests a
// governance-retention bypass; it requires the bypassGovernance capability and
// does not bypass compliance retention.
// https://www.backblaze.com/apidocs/b2-delete-file-version
// https://www.backblaze.com/docs/cloud-storage-object-lock
func (f *File) DeleteFileVersion(ctx context.Context, bypassGovernance ...bool) error {
	state := f.b2.snapshot()
	bypass := len(bypassGovernance) > 0 && bypassGovernance[0]
	b2req := &b2types.DeleteFileVersionRequest{
		Name:             f.Name,
		FileID:           f.ID,
		BypassGovernance: bypass,
	}
	headers := map[string]string{
		"Authorization": state.authToken,
	}
	return state.opts.makeRequest(ctx, "b2_delete_file_version", "POST", state.apiURI+b2types.V4api+"b2_delete_file_version", b2req, nil, headers, nil)
}

// UpdateFileRetention wraps b2_update_file_retention. A nil retention removes
// the file's retention, which for governance mode requires bypassGovernance.
func (f *File) UpdateFileRetention(ctx context.Context, retention *FileRetention, bypassGovernance bool) error {
	state := f.b2.snapshot()
	b2req := &b2types.UpdateFileRetentionRequest{FileID: f.ID, Name: f.Name, BypassGovernance: bypassGovernance}
	if retention != nil {
		b2req.Retention = *FileLock{Retention: retention}.retentionRequest()
	}
	headers := map[string]string{"Authorization": state.authToken}
	return state.opts.makeRequest(ctx, "b2_update_file_retention", "POST", state.apiURI+b2types.V4api+"b2_update_file_retention", b2req, nil, headers, nil)
}

// UpdateFileLegalHold wraps b2_update_file_legal_hold.
func (f *File) UpdateFileLegalHold(ctx context.Context, legalHold string) error {
	state := f.b2.snapshot()
	b2req := &b2types.UpdateFileLegalHoldRequest{FileID: f.ID, Name: f.Name, LegalHold: legalHold}
	headers := map[string]string{"Authorization": state.authToken}
	return state.opts.makeRequest(ctx, "b2_update_file_legal_hold", "POST", state.apiURI+b2types.V4api+"b2_update_file_legal_hold", b2req, nil, headers, nil)
}

// LargeFile holds information necessary to implement B2 large file support.
type LargeFile struct {
	ID string
	b2 *B2

	mu        sync.Mutex
	size      int64
	hashes    map[int]string
	partSizes map[int]int64
}

// previousPartSize resolves an old part's size before replacing it. CompileParts
// carries hashes and a total, but not individual sizes, so replacements of its
// seeded parts require one list-parts lookup before the old part is overwritten.
func (l *LargeFile) previousPartSize(ctx context.Context, number int) (int64, error) {
	l.mu.Lock()
	sha1, exists := l.hashes[number]
	size, known := l.partSizes[number]
	l.mu.Unlock()
	if !exists || known {
		return size, nil
	}
	parts, _, err := (&File{ID: l.ID, b2: l.b2}).ListParts(ctx, number, 1)
	if err != nil {
		return 0, err
	}
	if len(parts) != 1 || parts[0].Number != number || parts[0].SHA1 != sha1 || parts[0].Size <= 0 {
		return 0, fmt.Errorf("b2: cannot determine prior size of large-file part %d", number)
	}
	l.mu.Lock()
	if l.hashes[number] != sha1 {
		l.mu.Unlock()
		return 0, fmt.Errorf("b2: large-file part %d changed while resolving its size", number)
	}
	if l.partSizes == nil {
		l.partSizes = make(map[int]int64)
	}
	l.partSizes[number] = parts[0].Size
	l.mu.Unlock()
	return parts[0].Size, nil
}

// copyPartSSE reserves the optional encryption argument for a later SSE-C API.
type copyPartSSE struct{}

// CopyPart copies a source file or byte range into a part of this large file.
func (l *LargeFile) CopyPart(ctx context.Context, sourceFileID string, partNumber int, byteRange string, _ *copyPartSSE) (*FilePart, error) {
	previousSize, err := l.previousPartSize(ctx, partNumber)
	if err != nil {
		return nil, err
	}
	req := &b2types.CopyPartRequest{SourceFileID: sourceFileID, LargeFileID: l.ID, PartNumber: partNumber, Range: byteRange}
	resp := &b2types.CopyPartResponse{}
	state := l.b2.snapshot()
	headers := map[string]string{"Authorization": state.authToken}
	if err := state.opts.makeRequest(ctx, "b2_copy_part", "POST", state.apiURI+b2types.V4api+"b2_copy_part", req, resp, headers, nil); err != nil {
		return nil, err
	}
	if resp.FileID != l.ID || resp.PartNumber != partNumber || resp.Size <= 0 || resp.Size > 5e9 || resp.SHA1 == "" {
		return nil, fmt.Errorf("b2_copy_part: inconsistent response for file %q part %d", l.ID, partNumber)
	}
	l.mu.Lock()
	if known, ok := l.partSizes[partNumber]; ok {
		previousSize = known
	}
	l.hashes[partNumber] = resp.SHA1
	if l.partSizes == nil {
		l.partSizes = make(map[int]int64)
	}
	l.partSizes[partNumber] = resp.Size
	l.size += resp.Size - previousSize
	l.mu.Unlock()
	return &FilePart{Number: resp.PartNumber, SHA1: resp.SHA1, Size: resp.Size}, nil
}

// StartLargeFile wraps b2_start_large_file. At most one FileLock may be given;
// it sets Object Lock retention and legal hold on the new file.
func (b *Bucket) StartLargeFile(ctx context.Context, name, contentType string, info map[string]string, lock ...FileLock) (*LargeFile, error) {
	return b.StartLargeFileWithSSEC(ctx, name, contentType, info, nil, lock...)
}

// StartLargeFileWithSSEC wraps b2_start_large_file and carries SSE-C settings
// in the JSON request body.
func (b *Bucket) StartLargeFileWithSSEC(ctx context.Context, name, contentType string, info map[string]string, sse *b2types.ServerSideEncryption, lock ...FileLock) (*LargeFile, error) {
	if err := addServerSideEncryptionHeaders(map[string]string{}, sse); err != nil {
		return nil, err
	}
	state := b.b2.snapshot()
	b2req := &b2types.StartLargeFileRequest{
		BucketID:             b.ID,
		Name:                 name,
		ContentType:          contentType,
		Info:                 info,
		ServerSideEncryption: sse,
	}
	if len(lock) > 0 {
		b2req.Retention = lock[0].retentionRequest()
		b2req.LegalHold = lock[0].LegalHold
	}
	b2resp := &b2types.StartLargeFileResponse{}
	headers := map[string]string{
		"Authorization": state.authToken,
	}
	if err := state.opts.makeRequest(ctx, "b2_start_large_file", "POST", state.apiURI+b2types.V4api+"b2_start_large_file", b2req, b2resp, headers, nil); err != nil {
		return nil, err
	}
	return &LargeFile{
		ID:        b2resp.ID,
		b2:        b.b2,
		hashes:    make(map[int]string),
		partSizes: make(map[int]int64),
	}, nil
}

// CancelLargeFile wraps b2_cancel_large_file.
func (l *LargeFile) CancelLargeFile(ctx context.Context) error {
	state := l.b2.snapshot()
	b2req := &b2types.CancelLargeFileRequest{
		ID: l.ID,
	}
	headers := map[string]string{
		"Authorization": state.authToken,
	}
	return state.opts.makeRequest(ctx, "b2_cancel_large_file", "POST", state.apiURI+b2types.V4api+"b2_cancel_large_file", b2req, nil, headers, nil)
}

// FilePart is a piece of a started, but not finished, large file upload.
type FilePart struct {
	Number int
	SHA1   string
	Size   int64
}

// ListParts wraps b2_list_parts.
func (f *File) ListParts(ctx context.Context, next, count int) ([]*FilePart, int, error) {
	state := f.b2.snapshot()
	b2req := &b2types.ListPartsRequest{
		ID:    f.ID,
		Start: next,
		Count: count,
	}
	b2resp := &b2types.ListPartsResponse{}
	headers := map[string]string{
		"Authorization": state.authToken,
	}
	if err := state.opts.makeRequest(ctx, "b2_list_parts", "POST", state.apiURI+b2types.V4api+"b2_list_parts", b2req, b2resp, headers, nil); err != nil {
		return nil, 0, err
	}
	var parts []*FilePart
	for _, part := range b2resp.Parts {
		parts = append(parts, &FilePart{
			Number: part.Number,
			SHA1:   part.SHA1,
			Size:   part.Size,
		})
	}
	return parts, b2resp.Next, nil
}

// CompileParts returns a LargeFile that can accept new data.  Seen is a
// mapping of completed part numbers to SHA1 strings; size is the total size of
// all the completed parts to this point.
func (f *File) CompileParts(size int64, seen map[int]string) *LargeFile {
	s := make(map[int]string)
	for k, v := range seen {
		s[k] = v
	}
	return &LargeFile{
		ID:        f.ID,
		b2:        f.b2,
		size:      size,
		hashes:    s,
		partSizes: make(map[int]int64),
	}
}

// FileChunk holds information necessary for uploading file chunks.
type FileChunk struct {
	url   string
	token string
	file  *LargeFile
}

type getUploadPartURLRequest struct {
	ID string `json:"fileId"`
}

type getUploadPartURLResponse struct {
	URL   string `json:"uploadUrl"`
	Token string `json:"authorizationToken"`
}

// GetUploadPartURL wraps b2_get_upload_part_url.
func (l *LargeFile) GetUploadPartURL(ctx context.Context) (*FileChunk, error) {
	state := l.b2.snapshot()
	b2req := &getUploadPartURLRequest{
		ID: l.ID,
	}
	b2resp := &getUploadPartURLResponse{}
	headers := map[string]string{
		"Authorization": state.authToken,
	}
	if err := state.opts.makeRequest(ctx, "b2_get_upload_part_url", "POST", state.apiURI+b2types.V4api+"b2_get_upload_part_url", b2req, b2resp, headers, nil); err != nil {
		return nil, err
	}
	return &FileChunk{
		url:   b2resp.URL,
		token: b2resp.Token,
		file:  l,
	}, nil
}

// Reload reloads FileChunk in-place.
func (fc *FileChunk) Reload(ctx context.Context) error {
	n, err := fc.file.GetUploadPartURL(ctx)
	if err != nil {
		return err
	}
	fc.url = n.url
	fc.token = n.token
	return nil
}

// UploadPart wraps b2_upload_part.
func (fc *FileChunk) UploadPart(ctx context.Context, r io.Reader, sha1 string, size, index int) (int, error) {
	return fc.UploadPartWithSSEC(ctx, r, sha1, size, index, nil)
}

// UploadPartWithSSEC wraps b2_upload_part and repeats the SSE-C headers selected
// by b2_start_large_file.
func (fc *FileChunk) UploadPartWithSSEC(ctx context.Context, r io.Reader, sha1 string, size, index int, sse *b2types.ServerSideEncryption) (int, error) {
	previousSize, err := fc.file.previousPartSize(ctx, index)
	if err != nil {
		return 0, err
	}
	state := fc.file.b2.snapshot()
	headers := map[string]string{
		"Authorization":     fc.token,
		"X-Bz-Part-Number":  fmt.Sprintf("%d", index),
		"Content-Length":    fmt.Sprintf("%d", size),
		"X-Bz-Content-Sha1": sha1,
	}
	if err := addServerSideEncryptionHeaders(headers, sse); err != nil {
		return 0, err
	}
	if sha1 == "hex_digits_at_end" {
		r = &keepFinalBytes{r: r, remain: size}
	}
	if err := state.opts.makeRequest(ctx, "b2_upload_part", "POST", fc.url, nil, nil, headers, &requestBody{body: r, size: int64(size)}); err != nil {
		return 0, err
	}
	fc.file.mu.Lock()
	if known, ok := fc.file.partSizes[index]; ok {
		previousSize = known
	}
	if sha1 == "hex_digits_at_end" {
		sha1 = string(r.(*keepFinalBytes).sha[:])
	}
	fc.file.hashes[index] = sha1
	if fc.file.partSizes == nil {
		fc.file.partSizes = make(map[int]int64)
	}
	fc.file.partSizes[index] = int64(size)
	fc.file.size += int64(size) - previousSize
	fc.file.mu.Unlock()
	return size, nil
}

// FinishLargeFile wraps b2_finish_large_file.
func (l *LargeFile) FinishLargeFile(ctx context.Context) (*File, error) {
	state := l.b2.snapshot()
	l.mu.Lock()
	defer l.mu.Unlock()
	b2req := &b2types.FinishLargeFileRequest{
		ID:     l.ID,
		Hashes: make([]string, len(l.hashes)),
	}
	b2resp := &b2types.FinishLargeFileResponse{}
	for k, v := range l.hashes {
		if len(b2req.Hashes) < k {
			return nil, fmt.Errorf("b2_finish_large_file: invalid index %d", k)
		}
		b2req.Hashes[k-1] = v
	}
	headers := map[string]string{
		"Authorization": state.authToken,
	}
	if err := state.opts.makeRequest(ctx, "b2_finish_large_file", "POST", state.apiURI+b2types.V4api+"b2_finish_large_file", b2req, b2resp, headers, nil); err != nil {
		return nil, err
	}
	return &File{
		Name:      b2resp.Name,
		Size:      l.size,
		Timestamp: millitime(b2resp.Timestamp),
		Status:    b2resp.Action,
		ID:        b2resp.FileID,
		b2:        l.b2,
	}, nil
}

// ListUnfinishedLargeFiles wraps b2_list_unfinished_large_files.
func (b *Bucket) ListUnfinishedLargeFiles(ctx context.Context, count int, continuation string) ([]*File, string, error) {
	state := b.b2.snapshot()
	b2req := &b2types.ListUnfinishedLargeFilesRequest{
		BucketID:     b.ID,
		Continuation: continuation,
		Count:        count,
	}
	b2resp := &b2types.ListUnfinishedLargeFilesResponse{}
	headers := map[string]string{
		"Authorization": state.authToken,
	}
	if err := state.opts.makeRequest(ctx, "b2_list_unfinished_large_files", "POST", state.apiURI+b2types.V4api+"b2_list_unfinished_large_files", b2req, b2resp, headers, nil); err != nil {
		return nil, "", err
	}
	cont := b2resp.Continuation
	var files []*File
	for _, f := range b2resp.Files {
		retention, legalHold := fileLockFromResponse(f.Retention, f.LegalHold)
		var mode, algorithm, keyMD5 string
		if sse := f.ServerSideEncryption; sse != nil {
			mode, algorithm, keyMD5 = sse.Mode, sse.Algorithm, sse.CustomerKeyMD5
		}
		files = append(files, &File{
			Name:      f.Name,
			Status:    f.Action,
			Timestamp: millitime(f.Timestamp),
			b2:        b.b2,
			ID:        f.FileID,
			Info: &FileInfo{
				Name:                 f.Name,
				ContentType:          f.ContentType,
				Info:                 f.Info,
				Timestamp:            millitime(f.Timestamp),
				Retention:            retention,
				LegalHold:            legalHold,
				ServerSideEncryption: fileEncryptionMetadata(f.ServerSideEncryption),
				EncryptionMode:       mode,
				EncryptionAlgorithm:  algorithm,
				SSECKeyMD5:           keyMD5,
			},
		})
	}
	return files, cont, nil
}

// ListFileNames wraps b2_list_file_names.
func (b *Bucket) ListFileNames(ctx context.Context, count int, continuation, prefix, delimiter string) ([]*File, string, error) {
	state := b.b2.snapshot()
	if prefix == "" {
		prefix = state.pfx
	}
	b2req := &b2types.ListFileNamesRequest{
		Count:        count,
		Continuation: continuation,
		BucketID:     b.ID,
		Prefix:       prefix,
		Delimiter:    delimiter,
	}
	b2resp := &b2types.ListFileNamesResponse{}
	headers := map[string]string{
		"Authorization": state.authToken,
	}
	if err := state.opts.makeRequest(ctx, "b2_list_file_names", "POST", state.apiURI+b2types.V4api+"b2_list_file_names", b2req, b2resp, headers, nil); err != nil {
		return nil, "", err
	}
	cont := b2resp.Continuation
	var files []*File
	for _, f := range b2resp.Files {
		retention, legalHold := fileLockFromResponse(f.Retention, f.LegalHold)
		files = append(files, &File{
			Name:      f.Name,
			Size:      f.Size,
			Status:    f.Action,
			Timestamp: millitime(f.Timestamp),
			Info: &FileInfo{
				Name:                 f.Name,
				SHA1:                 f.SHA1,
				MD5:                  f.MD5,
				Size:                 f.Size,
				ContentType:          f.ContentType,
				Info:                 f.Info,
				Status:               f.Action,
				Timestamp:            millitime(f.Timestamp),
				Retention:            retention,
				LegalHold:            legalHold,
				ServerSideEncryption: fileEncryptionMetadata(f.ServerSideEncryption),
			},
			ID: f.FileID,
			b2: b.b2,
		})
	}
	return files, cont, nil
}

// ListFileVersions wraps b2_list_file_versions.
func (b *Bucket) ListFileVersions(ctx context.Context, count int, startName, startID, prefix, delimiter string) ([]*File, string, string, error) {
	state := b.b2.snapshot()
	if prefix == "" {
		prefix = state.pfx
	}
	b2req := &b2types.ListFileVersionsRequest{
		BucketID:  b.ID,
		Count:     count,
		StartName: startName,
		StartID:   startID,
		Prefix:    prefix,
		Delimiter: delimiter,
	}
	b2resp := &b2types.ListFileVersionsResponse{}
	headers := map[string]string{
		"Authorization": state.authToken,
	}
	if err := state.opts.makeRequest(ctx, "b2_list_file_versions", "POST", state.apiURI+b2types.V4api+"b2_list_file_versions", b2req, b2resp, headers, nil); err != nil {
		return nil, "", "", err
	}
	var files []*File
	for _, f := range b2resp.Files {
		retention, legalHold := fileLockFromResponse(f.Retention, f.LegalHold)
		files = append(files, &File{
			Name:      f.Name,
			Size:      f.Size,
			Status:    f.Action,
			Timestamp: millitime(f.Timestamp),
			Info: &FileInfo{
				Name:                 f.Name,
				SHA1:                 f.SHA1,
				MD5:                  f.MD5,
				Size:                 f.Size,
				ContentType:          f.ContentType,
				Info:                 f.Info,
				Status:               f.Action,
				Timestamp:            millitime(f.Timestamp),
				Retention:            retention,
				LegalHold:            legalHold,
				ServerSideEncryption: fileEncryptionMetadata(f.ServerSideEncryption),
			},
			ID: f.FileID,
			b2: b.b2,
		})
	}
	return files, b2resp.NextName, b2resp.NextID, nil
}

// GetDownloadAuthorization wraps b2_get_download_authorization.
func (b *Bucket) GetDownloadAuthorization(ctx context.Context, prefix string, valid time.Duration, contentDisposition string) (string, error) {
	state := b.b2.snapshot()
	b2req := &b2types.GetDownloadAuthorizationRequest{
		BucketID:           b.ID,
		Prefix:             prefix,
		Valid:              int(valid.Seconds()),
		ContentDisposition: contentDisposition,
	}
	b2resp := &b2types.GetDownloadAuthorizationResponse{}
	headers := map[string]string{
		"Authorization": state.authToken,
	}
	if err := state.opts.makeRequest(ctx, "b2_get_download_authorization", "POST", state.apiURI+b2types.V4api+"b2_get_download_authorization", b2req, b2resp, headers, nil); err != nil {
		return "", err
	}
	return b2resp.Token, nil
}

// FileReader is an io.ReadCloser that downloads a file from B2.
type FileReader struct {
	io.ReadCloser
	ContentLength int
	ContentType   string
	SHA1          string
	ID            string
	Info          map[string]string
}

func mkRange(offset, size int64) string {
	if offset == 0 && size == 0 {
		return ""
	}
	if size == 0 {
		return fmt.Sprintf("bytes=%d-", offset)
	}
	return fmt.Sprintf("bytes=%d-%d", offset, offset+size-1)
}

// DownloadFileByName wraps b2_download_file_by_name.
func (b *Bucket) DownloadFileByName(ctx context.Context, name string, offset, size int64, header bool) (*FileReader, error) {
	return b.DownloadFileByNameWithSSEC(ctx, name, offset, size, header, nil)
}

// DownloadFileByNameWithSSEC wraps b2_download_file_by_name and sends the
// customer-managed key on GET, HEAD and ranged requests.
func (b *Bucket) DownloadFileByNameWithSSEC(ctx context.Context, name string, offset, size int64, header bool, sse *b2types.ServerSideEncryption) (*FileReader, error) {
	state := b.b2.snapshot()
	uri := fmt.Sprintf("%s/file/%s/%s", state.downloadURI, b.Name, escape(name))
	return b.downloadFile(ctx, state, uri, "b2_download_file_by_name", offset, size, header, sse)
}

// DownloadFileByID wraps b2_download_file_by_id.
func (b *Bucket) DownloadFileByID(ctx context.Context, id string, offset, size int64, header bool) (*FileReader, error) {
	return b.DownloadFileByIDWithSSEC(ctx, id, offset, size, header, nil)
}

// DownloadFileByIDWithSSEC downloads the requested version with an optional
// customer-managed key on this request only.
func (b *Bucket) DownloadFileByIDWithSSEC(ctx context.Context, id string, offset, size int64, header bool, sse *b2types.ServerSideEncryption) (*FileReader, error) {
	state := b.b2.snapshot()
	uri := state.downloadURI + b2types.V4api + "b2_download_file_by_id?fileId=" + url.QueryEscape(id)
	return b.downloadFile(ctx, state, uri, "b2_download_file_by_id", offset, size, header, sse)
}

func (b *Bucket) downloadFile(ctx context.Context, state authState, uri, b2Method string, offset, size int64, header bool, sse *b2types.ServerSideEncryption) (*FileReader, error) {
	method := "GET"
	if header {
		method = "HEAD"
	}
	req, err := http.NewRequest(method, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", state.authToken)
	req.Header.Set("X-Blazer-Request-ID", fmt.Sprintf("%d", atomic.AddInt64(&reqID, 1)))
	req.Header.Set("X-Blazer-Method", b2Method)
	// B2 returns the stored bytes, including gzip-encoded files. Prevent Go's
	// transport from transparently decompressing and discarding Content-Length.
	req.Header.Set("Accept-Encoding", "identity")
	encryptionHeaders := map[string]string{}
	if err := addServerSideEncryptionHeaders(encryptionHeaders, sse); err != nil {
		return nil, err
	}
	for name, value := range encryptionHeaders {
		req.Header.Set(name, value)
	}
	state.opts.addHeaders(req)
	rng := mkRange(offset, size)
	if rng != "" {
		req.Header.Set("Range", rng)
	}
	logRequest(req, nil)
	resp, err := makeNetRequest(ctx, req, state.opts.getTransport())
	if err != nil {
		return nil, err
	}
	logResponse(resp, nil)
	return newFileReader(resp)
}

func newFileReader(resp *http.Response) (*FileReader, error) {
	return newFileReaderWithMaxLength(resp, int64(^uint(0)>>1))
}

func newFileReaderWithMaxLength(resp *http.Response, maxInt int64) (*FileReader, error) {
	if resp.StatusCode != 200 && resp.StatusCode != 206 {
		defer resp.Body.Close()
		return nil, mkErr(resp)
	}
	contentLength, err := strconv.Atoi(resp.Header.Get("Content-Length"))
	if err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	if contentLength < 0 || int64(contentLength) > maxInt {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("invalid download content length %d", contentLength)
	}
	info := make(map[string]string)
	for key := range resp.Header {
		if !strings.HasPrefix(key, "X-Bz-Info-") {
			continue
		}
		name, err := unescape(strings.TrimPrefix(key, "X-Bz-Info-"))
		if err != nil {
			resp.Body.Close()
			return nil, err
		}
		val, err := unescape(resp.Header.Get(key))
		if err != nil {
			resp.Body.Close()
			return nil, err
		}
		info[name] = val
	}
	sha1 := resp.Header.Get("X-Bz-Content-Sha1")
	if sha1 == "none" && info["Large_file_sha1"] != "" {
		sha1 = info["Large_file_sha1"]
	}
	return &FileReader{
		ReadCloser:    resp.Body,
		SHA1:          sha1,
		ID:            resp.Header.Get("X-Bz-File-Id"),
		ContentType:   resp.Header.Get("Content-Type"),
		ContentLength: contentLength,
		Info:          info,
	}, nil
}

// HideFile wraps b2_hide_file.
func (b *Bucket) HideFile(ctx context.Context, name string) (*File, error) {
	state := b.b2.snapshot()
	b2req := &b2types.HideFileRequest{
		BucketID: b.ID,
		File:     name,
	}
	b2resp := &b2types.HideFileResponse{}
	headers := map[string]string{
		"Authorization": state.authToken,
	}
	if err := state.opts.makeRequest(ctx, "b2_hide_file", "POST", state.apiURI+b2types.V4api+"b2_hide_file", b2req, b2resp, headers, nil); err != nil {
		return nil, err
	}
	return &File{
		Status:    b2resp.Action,
		Name:      name,
		Timestamp: millitime(b2resp.Timestamp),
		b2:        b.b2,
		ID:        b2resp.ID,
	}, nil
}

// FileInfo holds information about a specific file.
type FileInfo struct {
	Name                 string
	SHA1                 string
	MD5                  string
	Size                 int64
	ContentType          string
	Info                 map[string]string
	Status               string
	Timestamp            time.Time
	Retention            *FileRetention
	LegalHold            string
	ServerSideEncryption *b2types.ServerSideEncryption
	EncryptionMode       string
	EncryptionAlgorithm  string
	SSECKeyMD5           string
}

// fileEncryptionMetadata retains only non-secret response metadata. Never
// cache a customer key even if a server unexpectedly echoes it.
func fileEncryptionMetadata(sse *b2types.ServerSideEncryption) *b2types.ServerSideEncryption {
	if sse == nil {
		return nil
	}
	return &b2types.ServerSideEncryption{
		Mode: sse.Mode, Algorithm: sse.Algorithm, CustomerKeyMD5: sse.CustomerKeyMD5,
	}
}

// GetFileInfoByName returns metadata for the current visible file with name.
// A one-result b2_list_file_names request avoids downloading the file and
// returns no result for a hidden file or a name that only shares a prefix.
func (b *Bucket) GetFileInfoByName(ctx context.Context, name string) (*FileInfo, error) {
	state := b.b2.snapshot()
	if name == "" {
		return nil, fmt.Errorf("file %q: %w", name, fs.ErrNotExist)
	}
	request := &b2types.ListFileNamesRequest{
		BucketID:     b.ID,
		Count:        1,
		Continuation: name,
		Prefix:       name,
	}
	response := &b2types.ListFileNamesResponse{}
	headers := map[string]string{"Authorization": state.authToken}
	if err := state.opts.makeRequest(ctx, "b2_list_file_names", "POST", state.apiURI+b2types.V4api+"b2_list_file_names", request, response, headers, nil); err != nil {
		return nil, err
	}
	if len(response.Files) == 0 || response.Files[0].Name != name {
		return nil, fmt.Errorf("file %q: %w", name, fs.ErrNotExist)
	}
	f := response.Files[0]
	retention, legalHold := fileLockFromResponse(f.Retention, f.LegalHold)
	return &FileInfo{
		Name:                 f.Name,
		SHA1:                 f.SHA1,
		MD5:                  f.MD5,
		Size:                 f.Size,
		ContentType:          f.ContentType,
		Info:                 f.Info,
		Status:               f.Action,
		Timestamp:            millitime(f.Timestamp),
		Retention:            retention,
		LegalHold:            legalHold,
		ServerSideEncryption: fileEncryptionMetadata(f.ServerSideEncryption),
	}, nil
}

func fileLockFromResponse(retention b2types.FileRetentionResponse, legalHold b2types.LegalHoldResponse) (*FileRetention, string) {
	var fileRetention *FileRetention
	if v := retention.Value; retention.IsClientAuthorizedToRead && v != nil && v.Mode != nil && v.RetainUntilTimestamp != nil {
		fileRetention = &FileRetention{Mode: *v.Mode, RetainUntilTimestamp: *v.RetainUntilTimestamp}
	}
	var fileLegalHold string
	if legalHold.IsClientAuthorizedToRead {
		fileLegalHold = legalHold.Value
	}
	return fileRetention, fileLegalHold
}

// GetFileInfo wraps b2_get_file_info.
func (f *File) GetFileInfo(ctx context.Context) (*FileInfo, error) {
	state := f.b2.snapshot()
	b2req := &b2types.GetFileInfoRequest{
		ID: f.ID,
	}
	b2resp := &b2types.GetFileInfoResponse{}
	headers := map[string]string{
		"Authorization": state.authToken,
	}
	if err := state.opts.makeRequest(ctx, "b2_get_file_info", "POST", state.apiURI+b2types.V4api+"b2_get_file_info", b2req, b2resp, headers, nil); err != nil {
		return nil, err
	}
	f.Status = b2resp.Action
	f.Name = b2resp.Name
	f.Timestamp = millitime(b2resp.Timestamp)
	retention, legalHold := fileLockFromResponse(b2resp.Retention, b2resp.LegalHold)
	f.Info = &FileInfo{
		Name:                 b2resp.Name,
		SHA1:                 b2resp.SHA1,
		MD5:                  b2resp.MD5,
		Size:                 b2resp.Size,
		ContentType:          b2resp.ContentType,
		Info:                 b2resp.Info,
		Status:               b2resp.Action,
		Timestamp:            millitime(b2resp.Timestamp),
		Retention:            retention,
		LegalHold:            legalHold,
		ServerSideEncryption: fileEncryptionMetadata(b2resp.ServerSideEncryption),
	}
	return f.Info, nil
}

// AsLargeFile return a LargeFile with the same fields as this File
func (f *File) AsLargeFile() *LargeFile {
	return &LargeFile{
		ID:        f.ID,
		b2:        f.b2,
		hashes:    make(map[int]string),
		partSizes: make(map[int]int64),
	}
}

// Key is a B2 application key.
type Key struct {
	ID           string
	Secret       string
	Name         string
	Capabilities []string
	Expires      time.Time
	b2           *B2
}

// CreateKey wraps v3 b2_create_key, producing a single-bucket key (or, with an
// empty bucketID, an unrestricted one) that v3-only clients can still use. For
// a Multi-Bucket Application Key, use CreateKeyMultiBucket.
func (b *B2) CreateKey(ctx context.Context, name string, caps []string, valid time.Duration, bucketID string, prefix string) (*Key, error) {
	state := b.snapshot()
	b2req := &b2types.CreateKeyRequestV3{
		AccountID:    state.accountID,
		Capabilities: caps,
		Name:         name,
		Valid:        int(valid.Seconds()),
		BucketID:     bucketID,
		Prefix:       prefix,
	}
	return b.doCreateKeyWithState(ctx, state, b2types.V3api, b2req)
}

// CreateKeyMultiBucket wraps v4 b2_create_key, producing a Multi-Bucket
// Application Key. Such keys are unusable by v3-only clients; for v3
// compatibility, create one CreateKey per bucket instead.
func (b *B2) CreateKeyMultiBucket(ctx context.Context, name string, caps []string, valid time.Duration, bucketIDs []string, prefix string) (*Key, error) {
	state := b.snapshot()
	b2req := &b2types.CreateKeyRequestV4{
		AccountID:    state.accountID,
		Capabilities: caps,
		Name:         name,
		Valid:        int(valid.Seconds()),
		BucketIDs:    bucketIDs,
		Prefix:       prefix,
	}
	return b.doCreateKeyWithState(ctx, state, b2types.V4api, b2req)
}

func (b *B2) doCreateKeyWithState(ctx context.Context, state authState, apiVersion string, b2req interface{}) (*Key, error) {
	b2resp := &b2types.CreateKeyResponse{}
	headers := map[string]string{
		"Authorization": state.authToken,
	}
	if err := state.opts.makeRequest(ctx, "b2_create_key", "POST", state.apiURI+apiVersion+"b2_create_key", b2req, b2resp, headers, nil); err != nil {
		return nil, err
	}
	return &Key{
		Name:         b2resp.Name,
		ID:           b2resp.ID,
		Secret:       b2resp.Secret,
		Capabilities: b2resp.Capabilities,
		Expires:      millitime(b2resp.Expires),
		b2:           b,
	}, nil
}

// Delete wraps b2_delete_key.
func (k *Key) Delete(ctx context.Context) error {
	state := k.b2.snapshot()
	b2req := &b2types.DeleteKeyRequest{
		KeyID: k.ID,
	}
	headers := map[string]string{
		"Authorization": state.authToken,
	}
	return state.opts.makeRequest(ctx, "b2_delete_key", "POST", state.apiURI+b2types.V4api+"b2_delete_key", b2req, nil, headers, nil)
}

// ListKeys wraps b2_list_keys.
func (b *B2) ListKeys(ctx context.Context, max int, next string) ([]*Key, string, error) {
	state := b.snapshot()
	b2req := &b2types.ListKeysRequest{
		AccountID: state.accountID,
		Max:       max,
		Next:      next,
	}
	headers := map[string]string{
		"Authorization": state.authToken,
	}
	b2resp := &b2types.ListKeysResponse{}
	if err := state.opts.makeRequest(ctx, "b2_list_keys", "POST", state.apiURI+b2types.V4api+"b2_list_keys", b2req, b2resp, headers, nil); err != nil {
		return nil, "", err
	}
	var keys []*Key
	for _, key := range b2resp.Keys {
		keys = append(keys, &Key{
			Name:    key.Name,
			ID:      key.ID,
			Expires: millitime(key.Expires),
			b2:      b,
		})
	}
	return keys, b2resp.Next, nil
}

// CopyFile wraps b2_copy_file. An empty destinationBucketID makes B2 put the
// copy in the source file's bucket. contentType and info are sent only for the
// REPLACE metadata directive.
func (b *Bucket) CopyFile(ctx context.Context, sourceFileID, name, destinationBucketID, byteRange, metadataDirective, contentType string, info map[string]string) (*File, error) {
	return b.CopyFileWithSSEC(ctx, sourceFileID, name, destinationBucketID, byteRange, metadataDirective, contentType, info, nil, nil)
}

// CopyFileWithSSEC wraps b2_copy_file and supplies independent source and
// destination SSE-C settings in the JSON request body.
func (b *Bucket) CopyFileWithSSEC(ctx context.Context, sourceFileID, name, destinationBucketID, byteRange, metadataDirective, contentType string, info map[string]string, sourceSSE, destinationSSE *b2types.ServerSideEncryption) (*File, error) {
	if err := addServerSideEncryptionHeaders(map[string]string{}, sourceSSE); err != nil {
		return nil, fmt.Errorf("source encryption: %w", err)
	}
	if err := addServerSideEncryptionHeaders(map[string]string{}, destinationSSE); err != nil {
		return nil, fmt.Errorf("destination encryption: %w", err)
	}
	state := b.b2.snapshot()
	b2req := &b2types.CopyFileRequest{
		SourceFileID:                    sourceFileID,
		FileName:                        name,
		DestinationBucketID:             destinationBucketID,
		Range:                           byteRange,
		MetadataDirective:               metadataDirective,
		SourceServerSideEncryption:      sourceSSE,
		DestinationServerSideEncryption: destinationSSE,
	}
	if metadataDirective == "REPLACE" {
		b2req.ContentType = &contentType
		if info == nil {
			info = map[string]string{}
		}
		b2req.FileInfo = &info
	}
	b2resp := &b2types.CopyFileResponse{}
	headers := map[string]string{"Authorization": state.authToken}
	if err := state.opts.makeRequest(ctx, "b2_copy_file", "POST", state.apiURI+b2types.V4api+"b2_copy_file", b2req, b2resp, headers, nil); err != nil {
		return nil, err
	}
	return &File{
		Name:      b2resp.Name,
		Size:      b2resp.Size,
		Status:    b2resp.Action,
		Timestamp: millitime(b2resp.Timestamp),
		ID:        b2resp.FileID,
		b2:        b.b2,
	}, nil
}
