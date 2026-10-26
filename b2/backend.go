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

package b2

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/Backblaze/blazer/internal/retry"
)

// This file wraps the baseline interfaces with backoff and retry semantics.

type beRootInterface interface {
	backoff(error) time.Duration
	maxRetries(error) uint
	maxReuploads(error) uint
	retry(error) bool
	reauth(error) bool
	reupload(error) bool
	authorizeAccount(context.Context, string, string, clientOptions) error
	reauthorizeAccount(context.Context) error
	createBucket(ctx context.Context, name, btype string, info map[string]string, rules []LifecycleRule, sse *ServerSideEncryption, corsRules []CORSRule, fileLockEnabled bool, replicationConfig *ReplicationConfiguration) (beBucketInterface, error)
	listBuckets(context.Context, string, ...string) ([]beBucketInterface, error)
	createKey(context.Context, string, []string, time.Duration, string, string) (beKeyInterface, error)
	createKeyMultiBucket(context.Context, string, []string, time.Duration, []string, string) (beKeyInterface, error)
	listKeys(context.Context, int, string) ([]beKeyInterface, string, error)
}

type beRoot struct {
	account, key      string
	b2i               b2RootInterface
	options           clientOptions
	refreshMu         sync.Mutex // serializes refreshes, never base.B2's request lock
	refreshGeneration uint64
}

func (r *beRoot) authGeneration() uint64 {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	return r.refreshGeneration
}

func (r *beRoot) reauthorizeIfUnchanged(ctx context.Context, generation uint64) error {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	if r.refreshGeneration != generation {
		return nil
	}
	if err := r.reauthorizeAccount(ctx); err != nil {
		return err
	}
	r.refreshGeneration++
	return nil
}

type beBucketInterface interface {
	name() string
	btype() BucketType
	attrs() *BucketAttrs
	id() string
	updateBucket(context.Context, *BucketAttrs) error
	deleteBucket(context.Context) error
	notificationRules(context.Context) ([]NotificationRule, error)
	setNotificationRules(context.Context, []NotificationRule) error
	getUploadURL(context.Context) (beURLInterface, error)
	startLargeFile(ctx context.Context, name, contentType string, info map[string]string, retention *FileRetention, legalHold LegalHold, encryption *uploadEncryption) (beLargeFileInterface, error)
	listFileNames(context.Context, int, string, string, string) ([]beFileInterface, string, error)
	getFileInfoByName(context.Context, string) (beFileInfoInterface, error)
	listFileVersions(context.Context, int, string, string, string, string) ([]beFileInterface, string, string, error)
	listUnfinishedLargeFiles(context.Context, int, string) ([]beFileInterface, string, error)
	downloadFileByName(context.Context, string, int64, int64, bool, *SSECKey) (beFileReaderInterface, error)
	downloadFileByID(context.Context, string, int64, int64, bool, *SSECKey) (beFileReaderInterface, error)
	hideFile(context.Context, string) (beFileInterface, error)
	copyFile(context.Context, string, string, string, string, string, string, map[string]string, *SSECKey, *SSECKey) (beFileInterface, error)
	getDownloadAuthorization(context.Context, string, time.Duration, string) (string, error)
	baseURL() string
	s3URL() string
	file(string, string) beFileInterface
}

type beBucket struct {
	b2bucket b2BucketInterface
	ri       beRootInterface
}

type beURLInterface interface {
	uploadFile(context.Context, readResetter, int, string, string, string, map[string]string, *FileRetention, LegalHold, *uploadEncryption) (beFileInterface, error)
}

type beURL struct {
	b2url b2URLInterface
	ri    beRootInterface
}

type beFileInterface interface {
	name() string
	id() string
	size() int64
	timestamp() time.Time
	status() string
	deleteFileVersion(context.Context, bool) error
	updateFileRetention(context.Context, *FileRetention, bool) error
	updateFileLegalHold(context.Context, LegalHold) error
	getFileInfo(context.Context) (beFileInfoInterface, error)
	encryption() (mode, algorithm, keyMD5 string)
	listParts(context.Context, int, int) ([]beFilePartInterface, int, error)
	compileParts(int64, map[int]string, *uploadEncryption) beLargeFileInterface
	AsLargeFile() beLargeFileInterface
}

type beFile struct {
	b2file b2FileInterface
	url    beURLInterface
	ri     beRootInterface
}

type beLargeFileInterface interface {
	finishLargeFile(context.Context) (beFileInterface, error)
	getUploadPartURL(context.Context) (beFileChunkInterface, error)
	copyPart(context.Context, string, int, string) (beFilePartInterface, error)
	cancel(context.Context) error
}

type beLargeFile struct {
	b2largeFile b2LargeFileInterface
	ri          beRootInterface
}

type beFileChunkInterface interface {
	reload(context.Context) error
	uploadPart(context.Context, readResetter, string, int, int) (int, error)
}

type beFileChunk struct {
	b2fileChunk b2FileChunkInterface
	ri          beRootInterface
}

type beFileReaderInterface interface {
	io.ReadCloser
	stats() (int, string, string, map[string]string)
	id() string
}

type beFileReader struct {
	b2fileReader b2FileReaderInterface
	ri           beRootInterface
}

type beFileInfoInterface interface {
	stats() (string, string, int64, string, map[string]string, string, time.Time)
	fileLock() (*FileRetention, LegalHold)
	serverSideEncryption() *ServerSideEncryption
}

type beFilePartInterface interface {
	number() int
	sha1() string
	size() int64
}

type beFilePart struct {
	b2filePart b2FilePartInterface
	ri         beRootInterface
}

type beFileInfo struct {
	name       string
	sha        string
	size       int64
	ct         string
	info       map[string]string
	status     string
	stamp      time.Time
	retention  *FileRetention
	legalHold  LegalHold
	encryption *ServerSideEncryption
}

type beKeyInterface interface {
	del(context.Context) error
	caps() []string
	name() string
	expires() time.Time
	secret() string
	id() string
}

type beKey struct {
	b2i beRootInterface
	k   b2KeyInterface
}

func (r *beRoot) backoff(err error) time.Duration { return r.b2i.backoff(err) }
func (r *beRoot) maxRetries(err error) uint       { return r.b2i.maxRetries(err) }
func (r *beRoot) maxReuploads(err error) uint     { return r.b2i.maxReuploads(err) }
func (r *beRoot) retry(err error) bool            { return r.b2i.retry(err) }
func (r *beRoot) reauth(err error) bool           { return r.b2i.reauth(err) }
func (r *beRoot) reupload(err error) bool         { return r.b2i.reupload(err) }
func (r *beRoot) retryAfter(d time.Duration) <-chan time.Time {
	if timer, ok := r.b2i.(interface {
		retryAfter(time.Duration) <-chan time.Time
	}); ok {
		return timer.retryAfter(d)
	}
	return time.After(d)
}

func (r *beRoot) authorizeAccount(ctx context.Context, account, key string, c clientOptions) error {
	f := func() error {
		if err := r.b2i.authorizeAccount(ctx, account, key, c); err != nil {
			return err
		}
		r.account = account
		r.key = key
		r.options = c
		return nil
	}
	return withBackoff(ctx, r, f)
}

func (r *beRoot) reauthorizeAccount(ctx context.Context) error {
	// The stored credentials/options are set once during NewClient. Refreshing
	// only the underlying authorization avoids rewriting those fields while
	// other operations are reading them.
	return withBackoff(ctx, r, func() error {
		return r.b2i.authorizeAccount(ctx, r.account, r.key, r.options)
	})
}

func (r *beRoot) createBucket(ctx context.Context, name, btype string, info map[string]string, rules []LifecycleRule, sse *ServerSideEncryption, corsRules []CORSRule, fileLockEnabled bool, replicationConfig *ReplicationConfiguration) (beBucketInterface, error) {
	var bi beBucketInterface
	f := func() error {
		g := func() error {
			bucket, err := r.b2i.createBucket(ctx, name, btype, info, rules, sse, corsRules, fileLockEnabled, replicationConfig)
			if err != nil {
				return err
			}
			bi = &beBucket{
				b2bucket: bucket,
				ri:       r,
			}
			return nil
		}
		return withReauth(ctx, r, g)
	}
	if err := withBackoff(ctx, r, f); err != nil {
		return nil, err
	}
	return bi, nil
}

func (r *beRoot) listBuckets(ctx context.Context, name string, bucketTypes ...string) ([]beBucketInterface, error) {
	var buckets []beBucketInterface
	f := func() error {
		g := func() error {
			bs, err := r.b2i.listBuckets(ctx, name, bucketTypes...)
			if err != nil {
				return err
			}
			for _, b := range bs {
				buckets = append(buckets, &beBucket{
					b2bucket: b,
					ri:       r,
				})
			}
			return nil
		}
		return withReauth(ctx, r, g)
	}
	if err := withBackoff(ctx, r, f); err != nil {
		return nil, err
	}
	return buckets, nil
}

func (r *beRoot) createKey(ctx context.Context, name string, caps []string, valid time.Duration, bucketID string, prefix string) (beKeyInterface, error) {
	var k *beKey
	f := func() error {
		g := func() error {
			got, err := r.b2i.createKey(ctx, name, caps, valid, bucketID, prefix)
			if err != nil {
				return err
			}
			k = &beKey{
				b2i: r,
				k:   got,
			}
			return nil
		}
		return withReauth(ctx, r, g)
	}
	if err := withBackoff(ctx, r, f); err != nil {
		return nil, err
	}
	return k, nil
}

func (r *beRoot) createKeyMultiBucket(ctx context.Context, name string, caps []string, valid time.Duration, bucketIDs []string, prefix string) (beKeyInterface, error) {
	var k *beKey
	f := func() error {
		g := func() error {
			got, err := r.b2i.createKeyMultiBucket(ctx, name, caps, valid, bucketIDs, prefix)
			if err != nil {
				return err
			}
			k = &beKey{
				b2i: r,
				k:   got,
			}
			return nil
		}
		return withReauth(ctx, r, g)
	}
	if err := withBackoff(ctx, r, f); err != nil {
		return nil, err
	}
	return k, nil
}

func (r *beRoot) listKeys(ctx context.Context, max int, next string) ([]beKeyInterface, string, error) {
	var keys []beKeyInterface
	var cur string
	f := func() error {
		g := func() error {
			got, n, err := r.b2i.listKeys(ctx, max, next)
			if err != nil {
				return err
			}
			cur = n
			for _, g := range got {
				keys = append(keys, &beKey{
					b2i: r,
					k:   g,
				})
			}
			return nil
		}
		return withReauth(ctx, r, g)
	}
	if err := withBackoff(ctx, r, f); err != nil {
		return nil, "", err
	}
	return keys, cur, nil
}

func (b *beBucket) name() string        { return b.b2bucket.name() }
func (b *beBucket) btype() BucketType   { return BucketType(b.b2bucket.btype()) }
func (b *beBucket) attrs() *BucketAttrs { return b.b2bucket.attrs() }
func (b *beBucket) id() string          { return b.b2bucket.id() }

func (b *beBucket) updateBucket(ctx context.Context, attrs *BucketAttrs) error {
	f := func() error {
		g := func() error {
			return b.b2bucket.updateBucket(ctx, attrs)
		}
		return withReauth(ctx, b.ri, g)
	}
	return withBackoff(ctx, b.ri, f)
}

func (b *beBucket) deleteBucket(ctx context.Context) error {
	f := func() error {
		g := func() error {
			return b.b2bucket.deleteBucket(ctx)
		}
		return withReauth(ctx, b.ri, g)
	}
	return withBackoff(ctx, b.ri, f)
}

func (b *beBucket) notificationRules(ctx context.Context) ([]NotificationRule, error) {
	var rules []NotificationRule
	f := func() error {
		g := func() error {
			var err error
			rules, err = b.b2bucket.notificationRules(ctx)
			return err
		}
		return withReauth(ctx, b.ri, g)
	}
	if err := withBackoff(ctx, b.ri, f); err != nil {
		return nil, err
	}
	return rules, nil
}

func (b *beBucket) setNotificationRules(ctx context.Context, rules []NotificationRule) error {
	f := func() error {
		g := func() error {
			return b.b2bucket.setNotificationRules(ctx, rules)
		}
		return withReauth(ctx, b.ri, g)
	}
	return withBackoff(ctx, b.ri, f)
}

func (b *beBucket) getUploadURL(ctx context.Context) (beURLInterface, error) {
	var url beURLInterface
	f := func() error {
		g := func() error {
			u, err := b.b2bucket.getUploadURL(ctx)
			if err != nil {
				return err
			}
			url = &beURL{
				b2url: u,
				ri:    b.ri,
			}
			return nil
		}
		return withReauth(ctx, b.ri, g)
	}
	if err := withBackoff(ctx, b.ri, f); err != nil {
		return nil, err
	}
	return url, nil
}

func (b *beBucket) startLargeFile(ctx context.Context, name, ct string, info map[string]string, retention *FileRetention, legalHold LegalHold, encryption *uploadEncryption) (beLargeFileInterface, error) {
	var file beLargeFileInterface
	f := func() error {
		g := func() error {
			f, err := b.b2bucket.startLargeFile(ctx, name, ct, info, retention, legalHold, encryption)
			if err != nil {
				return err
			}
			file = &beLargeFile{
				b2largeFile: f,
				ri:          b.ri,
			}
			return nil
		}
		return withReauth(ctx, b.ri, g)
	}
	if err := withBackoff(ctx, b.ri, f); err != nil {
		return nil, err
	}
	return file, nil
}

func (b *beBucket) listFileNames(ctx context.Context, count int, continuation, prefix, delimiter string) ([]beFileInterface, string, error) {
	var cont string
	var files []beFileInterface
	f := func() error {
		g := func() error {
			fs, c, err := b.b2bucket.listFileNames(ctx, count, continuation, prefix, delimiter)
			if err != nil {
				return err
			}
			cont = c
			for _, f := range fs {
				files = append(files, &beFile{
					b2file: f,
					ri:     b.ri,
				})
			}
			return nil
		}
		return withReauth(ctx, b.ri, g)
	}
	if err := withBackoff(ctx, b.ri, f); err != nil {
		return nil, "", err
	}
	return files, cont, nil
}

func (b *beBucket) getFileInfoByName(ctx context.Context, name string) (beFileInfoInterface, error) {
	var result beFileInfoInterface
	err := withBackoff(ctx, b.ri, func() error {
		return withReauth(ctx, b.ri, func() error {
			info, err := b.b2bucket.getFileInfoByName(ctx, name)
			if err != nil {
				return err
			}
			fileName, sha, size, contentType, metadata, status, timestamp := info.stats()
			retention, legalHold := info.fileLock()
			encryption := info.serverSideEncryption()
			result = &beFileInfo{
				name: fileName, sha: sha, size: size, ct: contentType,
				info: metadata, status: status, stamp: timestamp,
				retention: retention, legalHold: legalHold, encryption: encryption,
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (b *beBucket) listFileVersions(ctx context.Context, count int, nextName, nextID, prefix, delimiter string) ([]beFileInterface, string, string, error) {
	var name, id string
	var files []beFileInterface
	f := func() error {
		g := func() error {
			fs, n, d, err := b.b2bucket.listFileVersions(ctx, count, nextName, nextID, prefix, delimiter)
			if err != nil {
				return err
			}
			name = n
			id = d
			for _, f := range fs {
				files = append(files, &beFile{
					b2file: f,
					ri:     b.ri,
				})
			}
			return nil
		}
		return withReauth(ctx, b.ri, g)
	}
	if err := withBackoff(ctx, b.ri, f); err != nil {
		return nil, "", "", err
	}
	return files, name, id, nil
}

func (b *beBucket) listUnfinishedLargeFiles(ctx context.Context, count int, continuation string) ([]beFileInterface, string, error) {
	var cont string
	var files []beFileInterface
	f := func() error {
		g := func() error {
			fs, c, err := b.b2bucket.listUnfinishedLargeFiles(ctx, count, continuation)
			if err != nil {
				return err
			}
			cont = c
			for _, f := range fs {
				files = append(files, &beFile{
					b2file: f,
					ri:     b.ri,
				})
			}
			return nil
		}
		return withReauth(ctx, b.ri, g)
	}
	if err := withBackoff(ctx, b.ri, f); err != nil {
		return nil, "", err
	}
	return files, cont, nil
}

func (b *beBucket) downloadFileByName(ctx context.Context, name string, offset, size int64, header bool, ssec *SSECKey) (beFileReaderInterface, error) {
	var reader beFileReaderInterface
	f := func() error {
		g := func() error {
			fr, err := b.b2bucket.downloadFileByName(ctx, name, offset, size, header, ssec)
			if err != nil {
				return err
			}
			reader = &beFileReader{
				b2fileReader: fr,
				ri:           b.ri,
			}
			return nil
		}
		return withReauth(ctx, b.ri, g)
	}
	if err := withBackoff(ctx, b.ri, f); err != nil {
		return nil, err
	}
	return reader, nil
}

func (b *beBucket) downloadFileByID(ctx context.Context, id string, offset, size int64, header bool, ssec *SSECKey) (beFileReaderInterface, error) {
	var reader beFileReaderInterface
	f := func() error {
		g := func() error {
			fr, err := b.b2bucket.downloadFileByID(ctx, id, offset, size, header, ssec)
			if err != nil {
				return err
			}
			reader = &beFileReader{
				b2fileReader: fr,
				ri:           b.ri,
			}
			return nil
		}
		return withReauth(ctx, b.ri, g)
	}
	if err := withBackoff(ctx, b.ri, f); err != nil {
		return nil, err
	}
	return reader, nil
}

func (b *beBucket) hideFile(ctx context.Context, name string) (beFileInterface, error) {
	var file beFileInterface
	f := func() error {
		g := func() error {
			f, err := b.b2bucket.hideFile(ctx, name)
			if err != nil {
				return err
			}
			file = &beFile{
				b2file: f,
				ri:     b.ri,
			}
			return nil
		}
		return withReauth(ctx, b.ri, g)
	}
	if err := withBackoff(ctx, b.ri, f); err != nil {
		return nil, err
	}
	return file, nil
}

func (b *beBucket) copyFile(ctx context.Context, sourceFileID, name, destinationBucketID, byteRange, metadataDirective, contentType string, info map[string]string, sourceSSEC, destinationSSEC *SSECKey) (beFileInterface, error) {
	var file beFileInterface
	f := func() error {
		g := func() error {
			copied, err := b.b2bucket.copyFile(ctx, sourceFileID, name, destinationBucketID, byteRange, metadataDirective, contentType, info, sourceSSEC, destinationSSEC)
			if err != nil {
				return err
			}
			file = &beFile{b2file: copied, ri: b.ri}
			return nil
		}
		return withReauth(ctx, b.ri, g)
	}
	if err := withBackoff(ctx, b.ri, f); err != nil {
		return nil, err
	}
	return file, nil
}

func (b *beBucket) getDownloadAuthorization(ctx context.Context, p string, v time.Duration, s string) (string, error) {
	var tok string
	f := func() error {
		g := func() error {
			t, err := b.b2bucket.getDownloadAuthorization(ctx, p, v, s)
			if err != nil {
				return err
			}
			tok = t
			return nil
		}
		return withReauth(ctx, b.ri, g)
	}
	if err := withBackoff(ctx, b.ri, f); err != nil {
		return "", err
	}
	return tok, nil
}

func (b *beBucket) baseURL() string {
	return b.b2bucket.baseURL()
}

func (b *beBucket) s3URL() string {
	return b.b2bucket.s3URL()
}

func (b *beBucket) file(id, name string) beFileInterface {
	return &beFile{
		b2file: b.b2bucket.file(id, name),
		ri:     b.ri,
	}
}

func (b *beURL) uploadFile(ctx context.Context, r readResetter, size int, name, ct, sha1 string, info map[string]string, retention *FileRetention, legalHold LegalHold, encryption *uploadEncryption) (beFileInterface, error) {
	var file beFileInterface
	f := func() error {
		if err := r.Reset(); err != nil {
			return err
		}
		f, err := b.b2url.uploadFile(ctx, r, size, name, ct, sha1, info, retention, legalHold, encryption)
		if err != nil {
			return err
		}
		file = &beFile{
			b2file: f,
			url:    b,
			ri:     b.ri,
		}
		return nil
	}
	if err := withBackoff(ctx, b.ri, f); err != nil {
		return nil, err
	}
	return file, nil
}

func (b *beFile) deleteFileVersion(ctx context.Context, bypassGovernance bool) error {
	f := func() error {
		g := func() error {
			return b.b2file.deleteFileVersion(ctx, bypassGovernance)
		}
		return withReauth(ctx, b.ri, g)
	}
	return withBackoff(ctx, b.ri, f)
}

func (b *beFile) updateFileRetention(ctx context.Context, retention *FileRetention, bypassGovernance bool) error {
	return withBackoff(ctx, b.ri, func() error {
		return withReauth(ctx, b.ri, func() error {
			return b.b2file.updateFileRetention(ctx, retention, bypassGovernance)
		})
	})
}

func (b *beFile) updateFileLegalHold(ctx context.Context, legalHold LegalHold) error {
	return withBackoff(ctx, b.ri, func() error {
		return withReauth(ctx, b.ri, func() error {
			return b.b2file.updateFileLegalHold(ctx, legalHold)
		})
	})
}

func (b *beFile) size() int64 {
	return b.b2file.size()
}

func (b *beFile) name() string {
	return b.b2file.name()
}

func (b *beFile) id() string {
	return b.b2file.id()
}

func (b *beFile) timestamp() time.Time {
	return b.b2file.timestamp()
}

func (b *beFile) status() string {
	return b.b2file.status()
}

func (b *beFile) getFileInfo(ctx context.Context) (beFileInfoInterface, error) {
	var fileInfo beFileInfoInterface
	f := func() error {
		g := func() error {
			fi, err := b.b2file.getFileInfo(ctx)
			if err != nil {
				return err
			}
			name, sha, size, ct, info, status, stamp := fi.stats()
			retention, legalHold := fi.fileLock()
			encryption := fi.serverSideEncryption()
			fileInfo = &beFileInfo{
				name:       name,
				sha:        sha,
				size:       size,
				ct:         ct,
				info:       info,
				status:     status,
				stamp:      stamp,
				retention:  retention,
				legalHold:  legalHold,
				encryption: encryption,
			}
			return nil
		}
		return withReauth(ctx, b.ri, g)
	}
	if err := withBackoff(ctx, b.ri, f); err != nil {
		return nil, err
	}
	return fileInfo, nil
}

func (b *beFile) encryption() (mode, algorithm, keyMD5 string) {
	return b.b2file.encryption()
}

func (b *beFile) listParts(ctx context.Context, next, count int) ([]beFilePartInterface, int, error) {
	var fpi []beFilePartInterface
	var rnxt int
	f := func() error {
		g := func() error {
			ps, n, err := b.b2file.listParts(ctx, next, count)
			if err != nil {
				return err
			}
			rnxt = n
			for _, p := range ps {
				fpi = append(fpi, &beFilePart{
					b2filePart: p,
					ri:         b.ri,
				})
			}
			return nil
		}
		return withReauth(ctx, b.ri, g)
	}
	if err := withBackoff(ctx, b.ri, f); err != nil {
		return nil, 0, err
	}
	return fpi, rnxt, nil
}

func (b *beFile) compileParts(size int64, seen map[int]string, encryption *uploadEncryption) beLargeFileInterface {
	return &beLargeFile{
		b2largeFile: b.b2file.compileParts(size, seen, encryption),
		ri:          b.ri,
	}
}

// AsLargeFile returns a beLargeFileInterface with the same fields as this beFile
func (b *beFile) AsLargeFile() beLargeFileInterface {
	b2file := b.b2file.(*b2File)
	return &beLargeFile{
		b2largeFile: &b2LargeFile{
			b: b2file.b.AsLargeFile(),
		},
		ri: b.ri,
	}
}

func (b *beLargeFile) getUploadPartURL(ctx context.Context) (beFileChunkInterface, error) {
	var chunk beFileChunkInterface
	f := func() error {
		g := func() error {
			fc, err := b.b2largeFile.getUploadPartURL(ctx)
			if err != nil {
				return err
			}
			chunk = &beFileChunk{
				b2fileChunk: fc,
				ri:          b.ri,
			}
			return nil
		}
		return withReauth(ctx, b.ri, g)
	}
	if err := withBackoff(ctx, b.ri, f); err != nil {
		return nil, err
	}
	return chunk, nil
}

func (b *beLargeFile) copyPart(ctx context.Context, sourceFileID string, partNumber int, byteRange string) (beFilePartInterface, error) {
	var part beFilePartInterface
	f := func() error {
		g := func() error {
			copied, err := b.b2largeFile.copyPart(ctx, sourceFileID, partNumber, byteRange)
			if err != nil {
				return err
			}
			part = &beFilePart{b2filePart: copied, ri: b.ri}
			return nil
		}
		return withReauth(ctx, b.ri, g)
	}
	if err := withBackoff(ctx, b.ri, f); err != nil {
		return nil, err
	}
	return part, nil
}

func (b *beLargeFile) finishLargeFile(ctx context.Context) (beFileInterface, error) {
	var file beFileInterface
	f := func() error {
		g := func() error {
			f, err := b.b2largeFile.finishLargeFile(ctx)
			if err != nil {
				return err
			}
			file = &beFile{
				b2file: f,
				ri:     b.ri,
			}
			return nil
		}
		return withReauth(ctx, b.ri, g)
	}
	if err := withBackoff(ctx, b.ri, f); err != nil {
		return nil, err
	}
	return file, nil
}

func (b *beLargeFile) cancel(ctx context.Context) error {
	f := func() error {
		g := func() error {
			return b.b2largeFile.cancel(ctx)
		}
		return withReauth(ctx, b.ri, g)
	}
	return withBackoff(ctx, b.ri, f)
}

func (b *beFileChunk) reload(ctx context.Context) error {
	f := func() error {
		g := func() error {
			return b.b2fileChunk.reload(ctx)
		}
		return withReauth(ctx, b.ri, g)
	}
	return withBackoff(ctx, b.ri, f)
}

func (b *beFileChunk) uploadPart(ctx context.Context, r readResetter, sha1 string, size, index int) (int, error) {
	// no re-auth; pass it back up to the caller so they can get an new upload URI and token
	// TODO: we should handle that here probably
	var i int
	f := func() error {
		if err := r.Reset(); err != nil {
			return err
		}
		j, err := b.b2fileChunk.uploadPart(ctx, r, sha1, size, index)
		if err != nil {
			return err
		}
		i = j
		return nil
	}
	if err := withBackoff(ctx, b.ri, f); err != nil {
		return 0, err
	}
	return i, nil
}

func (b *beFileReader) Read(p []byte) (int, error) {
	return b.b2fileReader.Read(p)
}

func (b *beFileReader) Close() error {
	return b.b2fileReader.Close()
}

func (b *beFileReader) stats() (int, string, string, map[string]string) {
	return b.b2fileReader.stats()
}

func (b *beFileReader) id() string { return b.b2fileReader.id() }

func (b *beFileInfo) stats() (string, string, int64, string, map[string]string, string, time.Time) {
	return b.name, b.sha, b.size, b.ct, b.info, b.status, b.stamp
}

func (b *beFileInfo) fileLock() (*FileRetention, LegalHold) { return b.retention, b.legalHold }

func (b *beFileInfo) serverSideEncryption() *ServerSideEncryption { return b.encryption }

func (b *beFilePart) number() int  { return b.b2filePart.number() }
func (b *beFilePart) sha1() string { return b.b2filePart.sha1() }
func (b *beFilePart) size() int64  { return b.b2filePart.size() }

func (b *beKey) del(ctx context.Context) error {
	f := func() error {
		return b.k.del(ctx)
	}
	return withBackoff(ctx, b.b2i, f)
}

func (b *beKey) caps() []string     { return b.k.caps() }
func (b *beKey) name() string       { return b.k.name() }
func (b *beKey) expires() time.Time { return b.k.expires() }
func (b *beKey) secret() string     { return b.k.secret() }
func (b *beKey) id() string         { return b.k.id() }

func withBackoff(ctx context.Context, ri beRootInterface, f func() error) error {
	return retry.Do(
		ctx,
		f,
		retry.DynamicAttempts(func(attempt uint, attempts uint, err error) uint {
			if attempt == 1 {
				return ri.maxRetries(err) + 1
			}
			return attempts
		}),
		retry.DynamicDelay(func(attempt uint, delay time.Duration, err error) time.Duration {
			bo := ri.backoff(err)
			if bo > 0 {
				return bo
			} else {
				if attempt == 1 {
					return retry.Backoff(500 * time.Millisecond)
				}
				return retry.Backoff(delay)
			}
		}),
		retry.RetryIf(func(attempt uint, err error) bool {
			return ri.retry(err)
		}),
		retry.WithAfter(retryAfterFor(ri)),
	)
}

func retryAfterFor(ri beRootInterface) func(time.Duration) <-chan time.Time {
	if timer, ok := ri.(interface {
		retryAfter(time.Duration) <-chan time.Time
	}); ok {
		return timer.retryAfter
	}
	return time.After
}

func withReauth(ctx context.Context, ri beRootInterface, f func() error) error {
	// Existing fake backends need not implement refresh coordination.
	coordinated, ok := ri.(interface {
		authGeneration() uint64
		reauthorizeIfUnchanged(context.Context, uint64) error
	})
	var generation uint64
	if ok {
		generation = coordinated.authGeneration()
	}
	err := f()
	if ri.reauth(err) {
		var refreshErr error
		if ok {
			refreshErr = coordinated.reauthorizeIfUnchanged(ctx, generation)
		} else {
			refreshErr = ri.reauthorizeAccount(ctx)
		}
		if refreshErr != nil {
			return refreshErr
		}
		err = f()
	}
	return err
}
