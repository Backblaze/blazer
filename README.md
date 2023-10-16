Blazer
====

[![GoDoc](https://godoc.org/github.com/Backblaze/blazer/b2?status.svg)](https://godoc.org/github.com/Backblaze/blazer/b2)

Blazer is a Golang client library for Backblaze B2 Cloud Object Storage.

```go
import "github.com/Backblaze/blazer/b2"
```

Blazer targets the Backblaze B2 Native API. Unless you specifically need to access Backblaze B2 via its Native API, you should use the [MinIO Go Client SDK](https://github.com/minio/minio-go) with Backblaze B2's S3 Compatible SDK. 

_Many thanks to Toby Burress ([kurin](https://github.com/kurin)) for creating and maintaining Blazer for its first six years._

## Examples

### Getting started
```go
import "os"

id := os.Getenv("B2_APPLICATION_KEY_ID")
key := os.Getenv("B2_APPLICATION_KEY")

ctx := context.Background()

// b2_authorize_account
b2, err := b2.NewClient(ctx, id, key)
if err != nil {
	log.Fatalln(e)
}

buckets, err := b2.ListBuckets(ctx)
if err != nil {
	log.Fatalln(e)
}
```

### Copy a file into B2

```go
func copyFile(ctx context.Context, bucket *b2.Bucket, src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	obj := bucket.Object(dst)
	w := obj.NewWriter(ctx)
	if _, err := io.Copy(w, f); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}
```

If the file is less than 100MB, Blazer will simply buffer the file and use the
`b2_upload_file` API to send the file to Backblaze.  If the file is greater
than 100MB, Blazer will use B2's large file support to upload the file in 100MB
chunks.

### Copy a file into B2, with multiple concurrent uploads

Uploading a large file with multiple HTTP connections is simple:

```go
func copyFile(ctx context.Context, bucket *b2.Bucket, writers int, src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bucket.Object(dst).NewWriter(ctx)
	w.ConcurrentUploads = writers
	if _, err := io.Copy(w, f); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}
```

This will automatically split the file into `writers` chunks of 100MB uploads.
Note that 100MB is the smallest chunk size that B2 supports.

### Download a file from B2

Downloading is as simple as uploading:

```go
func downloadFile(ctx context.Context, bucket *b2.Bucket, downloads int, src, dst string) error {
	r := bucket.Object(src).NewReader(ctx)
	defer r.Close()

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	r.ConcurrentDownloads = downloads
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
```

### List all objects in a bucket

```go
func printObjects(ctx context.Context, bucket *b2.Bucket) error {
	iterator := bucket.List(ctx)
	for iterator.Next() {
		fmt.Println(iterator.Object())
	}
	return iterator.Err()
}
```

### Grant temporary auth to a file

Say you have a number of files in a private bucket, and you want to allow other
people to download some files.  This is possible to do by issuing a temporary
authorization token for the prefix of the files you want to share.

```go
token, err := bucket.AuthToken(ctx, "photos", time.Hour)
```

If successful, `token` is then an authorization token valid for one hour, which
can be set in HTTP GET requests.

The hostname to use when downloading files via HTTP is account-specific and can
be found via the BaseURL method:

```go
base := bucket.BaseURL()
```


### Licenses
The b2 package currently does not consume any third party packages and entirely depends on imports of the Go stdlib or from sources provided within the `blazer` repository itself.
A report of used licenses can be found at `./b2/licenses.csv` which was generated with https://github.com/google/go-licenses . Please double check yourself if this is a concern as this may change over time and the licenses report could become stale