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
//
// It currently lacks support for the following APIs:
//
// b2_download_file_by_id
package base

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"regexp"
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
	DefaultUserAgent = "blazer/0.7.2"
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
	var retryAfter int
	retry := resp.Header.Get("Retry-After")
	if retry != "" {
		r, err := strconv.ParseInt(retry, 10, 64)
		if err != nil {
			r = 0
			blog.V(1).Infof("couldn't parse retry-after header %q: %v", retry, err)
		}
		retryAfter = int(r)
	}
	return b2err{
		msg:     msgBody,
		retry:   retryAfter,
		code:    resp.StatusCode,
		msgCode: msg.Code,
		method:  resp.Request.Header.Get("X-Blazer-Method"),
	}
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
	return time.Duration(e.retry) * time.Second
}

func logRequest(req *http.Request, args []byte) {
	if !blog.V(2) {
		return
	}
	var headers []string
	for k, v := range req.Header {
		if k == "Authorization" || k == "X-Blazer-Method" {
			continue
		}
		headers = append(headers, fmt.Sprintf("%s: %s", k, strings.Join(v, ",")))
	}
	hstr := strings.Join(headers, ";")
	method := req.Header.Get("X-Blazer-Method")
	if args != nil {
		blog.V(2).Infof(">> %s %v: %v headers: {%s} args: (%s)", method, req.Method, req.URL, hstr, string(args))
		return
	}
	blog.V(2).Infof(">> %s %v: %v {%s} (no args)", method, req.Method, req.URL, hstr)
}

var authRegexp = regexp.MustCompile(`"authorizationToken": ".[^"]*"`)

func logResponse(resp *http.Response, reply []byte) {
	if !blog.V(2) {
		return
	}
	var headers []string
	for k, v := range resp.Header {
		headers = append(headers, fmt.Sprintf("%s: %s", k, strings.Join(v, ",")))
	}
	hstr := strings.Join(headers, "; ")
	method := resp.Request.Header.Get("X-Blazer-Method")
	id := resp.Request.Header.Get("X-Blazer-Request-ID")
	if reply != nil {
		safe := string(authRegexp.ReplaceAll(reply, []byte(`"authorizationToken": "[redacted]"`)))
		blog.V(2).Infof("<< %s (%s) %s {%s} (%s)", method, id, resp.Status, hstr, safe)
		return
	}
	blog.V(2).Infof("<< %s (%s) %s {%s} (no reply)", method, id, resp.Status, hstr)
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
	accountID   string
	authToken   string
	apiURI      string
	s3URI       string
	downloadURI string
	minPartSize int
	opts        *b2Options
	bucket      string // restricted to this bucket if present
	pfx         string // restricted to objects with this prefix if present
}

// Update replaces the B2 object with a new one, in-place.
func (b *B2) Update(n *B2) {
	b.accountID = n.accountID
	b.authToken = n.authToken
	b.apiURI = n.apiURI
	b.downloadURI = n.downloadURI
	b.minPartSize = n.minPartSize
	b.opts = n.opts
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
		blog.V(2).Infof(">> %s uri: %v err: %v", method, req.URL, err)
		// The following code will work regardless of whether err is an x509.UnknownAuthorityError
		// (Go 1.19 and earlier) or a tls.CertificateVerificationError that wraps an
		// x509.UnknownAuthorityError (Go 1.20 and later).
		// See https://go.dev/doc/go1.20#cryptotlspkgcryptotls
		switch err.(type) {
		case x509.UnknownAuthorityError:
			return nil, err
		}
		if errors.As(err, &x509.UnknownAuthorityError{}) {
			return nil, err
		}

		return nil, b2err{
			msg:   err.Error(),
			retry: 1,
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
			blog.V(1).Infof("%s: couldn't read response: %v", method, err)
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
	if err := b2opts.makeRequest(ctx, "b2_authorize_account", "GET", b2opts.getAPIBase()+b2types.V3api+"b2_authorize_account", nil, b2resp, headers, nil); err != nil {
		return nil, err
	}
	return &B2{
		accountID:   b2resp.AccountID,
		authToken:   b2resp.AuthToken,
		apiURI:      b2resp.APIInfo.StorageAPIInfo.URI,
		s3URI:       b2resp.APIInfo.StorageAPIInfo.S3URI,
		downloadURI: b2resp.APIInfo.StorageAPIInfo.DownloadURI,
		minPartSize: b2resp.APIInfo.StorageAPIInfo.AbsMinPartSize,
		bucket:      b2resp.APIInfo.StorageAPIInfo.Bucket,
		pfx:         b2resp.APIInfo.StorageAPIInfo.Prefix,
		opts:        b2opts,
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

// FailSomeUploads requests intermittent upload failures from the B2 service.
// This is mostly useful for testing.
func FailSomeUploads() AuthOption {
	return func(o *b2Options) {
		o.failSomeUploads = true
	}
}

// ExpireSomeAuthTokens requests intermittent authentication failures from the
// B2 service.
func ExpireSomeAuthTokens() AuthOption {
	return func(o *b2Options) {
		o.expireTokens = true
	}
}

// ForceCapExceeded requests a cap limit from the B2 service.  This causes all
// uploads to be treated as if they would exceed the configure B2 capacity.
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

// CreateBucket wraps b2_create_bucket.
func (b *B2) CreateBucket(ctx context.Context, name, btype string, info map[string]string, rules []LifecycleRule) (*Bucket, error) {
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
		AccountID:      b.accountID,
		Name:           name,
		Type:           btype,
		Info:           info,
		LifecycleRules: b2rules,
	}
	b2resp := &b2types.CreateBucketResponse{}
	headers := map[string]string{
		"Authorization": b.authToken,
	}
	if err := b.opts.makeRequest(ctx, "b2_create_bucket", "POST", b.apiURI+b2types.V3api+"b2_create_bucket", b2req, b2resp, headers, nil); err != nil {
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
	return &Bucket{
		Name:           name,
		Info:           b2resp.Info,
		LifecycleRules: respRules,
		ID:             b2resp.BucketID,
		rev:            b2resp.Revision,
		b2:             b,
	}, nil
}

// DeleteBucket wraps b2_delete_bucket.
func (b *Bucket) DeleteBucket(ctx context.Context) error {
	b2req := &b2types.DeleteBucketRequest{
		AccountID: b.b2.accountID,
		BucketID:  b.ID,
	}
	headers := map[string]string{
		"Authorization": b.b2.authToken,
	}
	return b.b2.opts.makeRequest(ctx, "b2_delete_bucket", "POST", b.b2.apiURI+b2types.V3api+"b2_delete_bucket", b2req, nil, headers, nil)
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

	CORSRules                   []b2types.CORSRule
	DefaultRetention            *b2types.Retention
	DefaultServerSideEncryption *b2types.ServerSideEncryption
	FileLockEnabled             bool
	ReplicationConfiguration    *b2types.ReplicationConfiguration
}

// Update wraps b2_update_bucket.
func (b *Bucket) Update(ctx context.Context) (*Bucket, error) {
	var rules []b2types.LifecycleRule
	for _, rule := range b.LifecycleRules {
		rules = append(rules, b2types.LifecycleRule{
			DaysNewUntilHidden:     rule.DaysNewUntilHidden,
			DaysHiddenUntilDeleted: rule.DaysHiddenUntilDeleted,
			Prefix:                 rule.Prefix,
		})
	}
	b2req := &b2types.UpdateBucketRequest{
		AccountID: b.b2.accountID,
		BucketID:  b.ID,
		// Name:           b.Name,
		Type:           b.Type,
		Info:           b.Info,
		LifecycleRules: rules,
		IfRevisionIs:   b.rev,

		CORSRules:                   b.CORSRules,
		DefaultRetention:            b.DefaultRetention,
		DefaultServerSideEncryption: b.DefaultServerSideEncryption,
		FileLockEnabled:             b.FileLockEnabled,
		ReplicationConfiguration:    b.ReplicationConfiguration,
	}
	headers := map[string]string{
		"Authorization": b.b2.authToken,
	}
	b2resp := &b2types.UpdateBucketResponse{}
	if err := b.b2.opts.makeRequest(ctx, "b2_update_bucket", "POST", b.b2.apiURI+b2types.V3api+"b2_update_bucket", b2req, b2resp, headers, nil); err != nil {
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
		DefaultServerSideEncryption: b2resp.DefaultServerSideEncryption,
		FileLockEnabled:             b2resp.FileLockConfig.Val.IsFileLockEnabled,
		ReplicationConfiguration:    b2resp.ReplicationConfiguration.Value,
	}
	if b2resp.FileLockConfig.Val.DefaultRetention.Mode != nil {
		updated.DefaultRetention = &b2types.Retention{}
		updated.DefaultRetention.Mode = *b2resp.FileLockConfig.Val.DefaultRetention.Mode
		updated.DefaultRetention.Period = &b2types.RetentionPeriod{
			Duration: b2resp.FileLockConfig.Val.DefaultRetention.Period.Duration,
			Unit:     *b2resp.FileLockConfig.Val.DefaultRetention.Period.Unit,
		}
	}

	return updated, nil
}

// BaseURL returns the base part of the download URLs.
func (b *Bucket) BaseURL() string {
	return b.b2.downloadURI
}

// S3URL returns the base URL for S3-compatible API calls.
func (b *Bucket) S3URL() string {
	return b.b2.s3URI
}

// ListBuckets wraps b2_list_buckets.  If name is non-empty, only that bucket
// will be returned if it exists; else nothing will be returned.
func (b *B2) ListBuckets(ctx context.Context, name string, bucketTypes ...string) ([]*Bucket, error) {
	b2req := &b2types.ListBucketsRequest{
		AccountID:   b.accountID,
		Bucket:      b.bucket,
		Name:        name,
		BucketTypes: bucketTypes,
	}
	b2resp := &b2types.ListBucketsResponse{}
	headers := map[string]string{
		"Authorization": b.authToken,
	}
	if err := b.opts.makeRequest(ctx, "b2_list_buckets", "POST", b.apiURI+b2types.V3api+"b2_list_buckets", b2req, b2resp, headers, nil); err != nil {
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
		buckets = append(buckets, &Bucket{
			Name:           bucket.Name,
			Type:           bucket.Type,
			Info:           bucket.Info,
			LifecycleRules: rules,
			ID:             bucket.BucketID,
			rev:            bucket.Revision,
			b2:             b,
		})
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
	b2req := &b2types.GetUploadURLRequest{
		BucketID: b.ID,
	}
	b2resp := &b2types.GetUploadURLResponse{}
	headers := map[string]string{
		"Authorization": b.b2.authToken,
	}
	if err := b.b2.opts.makeRequest(ctx, "b2_get_upload_url", "POST", b.b2.apiURI+b2types.V3api+"b2_get_upload_url", b2req, b2resp, headers, nil); err != nil {
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

// UploadFile wraps b2_upload_file.
func (url *URL) UploadFile(ctx context.Context, r io.Reader, size int, name, contentType, sha1 string, info map[string]string) (*File, error) {
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
	b2resp := &b2types.UploadFileResponse{}
	if err := url.b2.opts.makeRequest(ctx, "b2_upload_file", "POST", url.uri, nil, b2resp, headers, &requestBody{body: r, size: int64(size)}); err != nil {
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

// DeleteFileVersion wraps b2_delete_file_version.
func (f *File) DeleteFileVersion(ctx context.Context) error {
	b2req := &b2types.DeleteFileVersionRequest{
		Name:   f.Name,
		FileID: f.ID,
	}
	headers := map[string]string{
		"Authorization": f.b2.authToken,
	}
	return f.b2.opts.makeRequest(ctx, "b2_delete_file_version", "POST", f.b2.apiURI+b2types.V3api+"b2_delete_file_version", b2req, nil, headers, nil)
}

// LargeFile holds information necessary to implement B2 large file support.
type LargeFile struct {
	ID string
	b2 *B2

	mu     sync.Mutex
	size   int64
	hashes map[int]string
}

// StartLargeFile wraps b2_start_large_file.
func (b *Bucket) StartLargeFile(ctx context.Context, name, contentType string, info map[string]string) (*LargeFile, error) {
	b2req := &b2types.StartLargeFileRequest{
		BucketID:    b.ID,
		Name:        name,
		ContentType: contentType,
		Info:        info,
	}
	b2resp := &b2types.StartLargeFileResponse{}
	headers := map[string]string{
		"Authorization": b.b2.authToken,
	}
	if err := b.b2.opts.makeRequest(ctx, "b2_start_large_file", "POST", b.b2.apiURI+b2types.V3api+"b2_start_large_file", b2req, b2resp, headers, nil); err != nil {
		return nil, err
	}
	return &LargeFile{
		ID:     b2resp.ID,
		b2:     b.b2,
		hashes: make(map[int]string),
	}, nil
}

// CancelLargeFile wraps b2_cancel_large_file.
func (l *LargeFile) CancelLargeFile(ctx context.Context) error {
	b2req := &b2types.CancelLargeFileRequest{
		ID: l.ID,
	}
	headers := map[string]string{
		"Authorization": l.b2.authToken,
	}
	return l.b2.opts.makeRequest(ctx, "b2_cancel_large_file", "POST", l.b2.apiURI+b2types.V3api+"b2_cancel_large_file", b2req, nil, headers, nil)
}

// FilePart is a piece of a started, but not finished, large file upload.
type FilePart struct {
	Number int
	SHA1   string
	Size   int64
}

// ListParts wraps b2_list_parts.
func (f *File) ListParts(ctx context.Context, next, count int) ([]*FilePart, int, error) {
	b2req := &b2types.ListPartsRequest{
		ID:    f.ID,
		Start: next,
		Count: count,
	}
	b2resp := &b2types.ListPartsResponse{}
	headers := map[string]string{
		"Authorization": f.b2.authToken,
	}
	if err := f.b2.opts.makeRequest(ctx, "b2_list_parts", "POST", f.b2.apiURI+b2types.V3api+"b2_list_parts", b2req, b2resp, headers, nil); err != nil {
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
		ID:     f.ID,
		b2:     f.b2,
		size:   size,
		hashes: s,
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
	b2req := &getUploadPartURLRequest{
		ID: l.ID,
	}
	b2resp := &getUploadPartURLResponse{}
	headers := map[string]string{
		"Authorization": l.b2.authToken,
	}
	if err := l.b2.opts.makeRequest(ctx, "b2_get_upload_part_url", "POST", l.b2.apiURI+b2types.V3api+"b2_get_upload_part_url", b2req, b2resp, headers, nil); err != nil {
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
	headers := map[string]string{
		"Authorization":     fc.token,
		"X-Bz-Part-Number":  fmt.Sprintf("%d", index),
		"Content-Length":    fmt.Sprintf("%d", size),
		"X-Bz-Content-Sha1": sha1,
	}
	if sha1 == "hex_digits_at_end" {
		r = &keepFinalBytes{r: r, remain: size}
	}
	if err := fc.file.b2.opts.makeRequest(ctx, "b2_upload_part", "POST", fc.url, nil, nil, headers, &requestBody{body: r, size: int64(size)}); err != nil {
		return 0, err
	}
	fc.file.mu.Lock()
	if sha1 == "hex_digits_at_end" {
		sha1 = string(r.(*keepFinalBytes).sha[:])
	}
	fc.file.hashes[index] = sha1
	fc.file.size += int64(size)
	fc.file.mu.Unlock()
	return size, nil
}

// FinishLargeFile wraps b2_finish_large_file.
func (l *LargeFile) FinishLargeFile(ctx context.Context) (*File, error) {
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
		"Authorization": l.b2.authToken,
	}
	if err := l.b2.opts.makeRequest(ctx, "b2_finish_large_file", "POST", l.b2.apiURI+b2types.V3api+"b2_finish_large_file", b2req, b2resp, headers, nil); err != nil {
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
	b2req := &b2types.ListUnfinishedLargeFilesRequest{
		BucketID:     b.ID,
		Continuation: continuation,
		Count:        count,
	}
	b2resp := &b2types.ListUnfinishedLargeFilesResponse{}
	headers := map[string]string{
		"Authorization": b.b2.authToken,
	}
	if err := b.b2.opts.makeRequest(ctx, "b2_list_unfinished_large_files", "POST", b.b2.apiURI+b2types.V3api+"b2_list_unfinished_large_files", b2req, b2resp, headers, nil); err != nil {
		return nil, "", err
	}
	cont := b2resp.Continuation
	var files []*File
	for _, f := range b2resp.Files {
		files = append(files, &File{
			Name:      f.Name,
			Status:    f.Action,
			Timestamp: millitime(f.Timestamp),
			b2:        b.b2,
			ID:        f.FileID,
			Info: &FileInfo{
				Name:        f.Name,
				ContentType: f.ContentType,
				Info:        f.Info,
				Timestamp:   millitime(f.Timestamp),
			},
		})
	}
	return files, cont, nil
}

// ListFileNames wraps b2_list_file_names.
func (b *Bucket) ListFileNames(ctx context.Context, count int, continuation, prefix, delimiter string) ([]*File, string, error) {
	if prefix == "" {
		prefix = b.b2.pfx
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
		"Authorization": b.b2.authToken,
	}
	if err := b.b2.opts.makeRequest(ctx, "b2_list_file_names", "POST", b.b2.apiURI+b2types.V3api+"b2_list_file_names", b2req, b2resp, headers, nil); err != nil {
		return nil, "", err
	}
	cont := b2resp.Continuation
	var files []*File
	for _, f := range b2resp.Files {
		files = append(files, &File{
			Name:      f.Name,
			Size:      f.Size,
			Status:    f.Action,
			Timestamp: millitime(f.Timestamp),
			Info: &FileInfo{
				Name:        f.Name,
				SHA1:        f.SHA1,
				MD5:         f.MD5,
				Size:        f.Size,
				ContentType: f.ContentType,
				Info:        f.Info,
				Status:      f.Action,
				Timestamp:   millitime(f.Timestamp),
			},
			ID: f.FileID,
			b2: b.b2,
		})
	}
	return files, cont, nil
}

// ListFileVersions wraps b2_list_file_versions.
func (b *Bucket) ListFileVersions(ctx context.Context, count int, startName, startID, prefix, delimiter string) ([]*File, string, string, error) {
	if prefix == "" {
		prefix = b.b2.pfx
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
		"Authorization": b.b2.authToken,
	}
	if err := b.b2.opts.makeRequest(ctx, "b2_list_file_versions", "POST", b.b2.apiURI+b2types.V3api+"b2_list_file_versions", b2req, b2resp, headers, nil); err != nil {
		return nil, "", "", err
	}
	var files []*File
	for _, f := range b2resp.Files {
		files = append(files, &File{
			Name:      f.Name,
			Size:      f.Size,
			Status:    f.Action,
			Timestamp: millitime(f.Timestamp),
			Info: &FileInfo{
				Name:        f.Name,
				SHA1:        f.SHA1,
				MD5:         f.MD5,
				Size:        f.Size,
				ContentType: f.ContentType,
				Info:        f.Info,
				Status:      f.Action,
				Timestamp:   millitime(f.Timestamp),
			},
			ID: f.FileID,
			b2: b.b2,
		})
	}
	return files, b2resp.NextName, b2resp.NextID, nil
}

// GetDownloadAuthorization wraps b2_get_download_authorization.
func (b *Bucket) GetDownloadAuthorization(ctx context.Context, prefix string, valid time.Duration, contentDisposition string) (string, error) {
	b2req := &b2types.GetDownloadAuthorizationRequest{
		BucketID:           b.ID,
		Prefix:             prefix,
		Valid:              int(valid.Seconds()),
		ContentDisposition: contentDisposition,
	}
	b2resp := &b2types.GetDownloadAuthorizationResponse{}
	headers := map[string]string{
		"Authorization": b.b2.authToken,
	}
	if err := b.b2.opts.makeRequest(ctx, "b2_get_download_authorization", "POST", b.b2.apiURI+b2types.V3api+"b2_get_download_authorization", b2req, b2resp, headers, nil); err != nil {
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
	uri := fmt.Sprintf("%s/file/%s/%s", b.b2.downloadURI, b.Name, escape(name))
	method := "GET"
	if header {
		method = "HEAD"
	}
	req, err := http.NewRequest(method, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", b.b2.authToken)
	req.Header.Set("X-Blazer-Request-ID", fmt.Sprintf("%d", atomic.AddInt64(&reqID, 1)))
	req.Header.Set("X-Blazer-Method", "b2_download_file_by_name")
	b.b2.opts.addHeaders(req)
	rng := mkRange(offset, size)
	if rng != "" {
		req.Header.Set("Range", rng)
	}
	logRequest(req, nil)
	resp, err := makeNetRequest(ctx, req, b.b2.opts.getTransport())
	if err != nil {
		return nil, err
	}
	logResponse(resp, nil)
	if resp.StatusCode != 200 && resp.StatusCode != 206 {
		defer resp.Body.Close()
		return nil, mkErr(resp)
	}
	clen, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	if err != nil {
		resp.Body.Close()
		return nil, err
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
		ContentLength: int(clen),
		Info:          info,
	}, nil
}

// HideFile wraps b2_hide_file.
func (b *Bucket) HideFile(ctx context.Context, name string) (*File, error) {
	b2req := &b2types.HideFileRequest{
		BucketID: b.ID,
		File:     name,
	}
	b2resp := &b2types.HideFileResponse{}
	headers := map[string]string{
		"Authorization": b.b2.authToken,
	}
	if err := b.b2.opts.makeRequest(ctx, "b2_hide_file", "POST", b.b2.apiURI+b2types.V3api+"b2_hide_file", b2req, b2resp, headers, nil); err != nil {
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
	Name        string
	SHA1        string
	MD5         string
	Size        int64
	ContentType string
	Info        map[string]string
	Status      string
	Timestamp   time.Time
}

// GetFileInfo wraps b2_get_file_info.
func (f *File) GetFileInfo(ctx context.Context) (*FileInfo, error) {
	b2req := &b2types.GetFileInfoRequest{
		ID: f.ID,
	}
	b2resp := &b2types.GetFileInfoResponse{}
	headers := map[string]string{
		"Authorization": f.b2.authToken,
	}
	if err := f.b2.opts.makeRequest(ctx, "b2_get_file_info", "POST", f.b2.apiURI+b2types.V3api+"b2_get_file_info", b2req, b2resp, headers, nil); err != nil {
		return nil, err
	}
	f.Status = b2resp.Action
	f.Name = b2resp.Name
	f.Timestamp = millitime(b2resp.Timestamp)
	f.Info = &FileInfo{
		Name:        b2resp.Name,
		SHA1:        b2resp.SHA1,
		MD5:         b2resp.MD5,
		Size:        b2resp.Size,
		ContentType: b2resp.ContentType,
		Info:        b2resp.Info,
		Status:      b2resp.Action,
		Timestamp:   millitime(b2resp.Timestamp),
	}
	return f.Info, nil
}

// AsLargeFile return a LargeFile with the same fields as this File
func (f *File) AsLargeFile() *LargeFile {
	return &LargeFile{
		ID: f.ID,
		b2: f.b2,
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

// CreateKey wraps b2_create_key.
func (b *B2) CreateKey(ctx context.Context, name string, caps []string, valid time.Duration, bucketID string, prefix string) (*Key, error) {
	b2req := &b2types.CreateKeyRequest{
		AccountID:    b.accountID,
		Capabilities: caps,
		Name:         name,
		Valid:        int(valid.Seconds()),
		BucketID:     bucketID,
		Prefix:       prefix,
	}
	b2resp := &b2types.CreateKeyResponse{}
	headers := map[string]string{
		"Authorization": b.authToken,
	}
	if err := b.opts.makeRequest(ctx, "b2_create_key", "POST", b.apiURI+b2types.V3api+"b2_create_key", b2req, b2resp, headers, nil); err != nil {
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
	b2req := &b2types.DeleteKeyRequest{
		KeyID: k.ID,
	}
	headers := map[string]string{
		"Authorization": k.b2.authToken,
	}
	return k.b2.opts.makeRequest(ctx, "b2_delete_key", "POST", k.b2.apiURI+b2types.V3api+"b2_delete_key", b2req, nil, headers, nil)
}

// ListKeys wraps b2_list_keys.
func (b *B2) ListKeys(ctx context.Context, max int, next string) ([]*Key, string, error) {
	b2req := &b2types.ListKeysRequest{
		AccountID: b.accountID,
		Max:       max,
		Next:      next,
	}
	headers := map[string]string{
		"Authorization": b.authToken,
	}
	b2resp := &b2types.ListKeysResponse{}
	if err := b.opts.makeRequest(ctx, "b2_list_keys", "POST", b.apiURI+b2types.V3api+"b2_list_keys", b2req, b2resp, headers, nil); err != nil {
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
