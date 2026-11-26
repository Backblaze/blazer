// Copyright 2017, the Blazer authors
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

// This is a simple program that will copy named files into or out of B2.
//
// To copy a file into B2:
//
//	B2_APPLICATION_KEY_ID=foo B2_APPLICATION_KEY=bar simple /path/to/file b2://bucket/path/to/dst
//
// To copy a file out:
//
//	B2_APPLICATION_KEY_ID=foo B2_APPLICATION_KEY=bar simple b2://bucket/path/to/file /path/to/dst
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Backblaze/blazer/b2"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	flag.Parse()
	b2id := os.Getenv("B2_APPLICATION_KEY_ID")
	b2key := os.Getenv("B2_APPLICATION_KEY")
	// The old names remain as a deprecated fallback.
	if b2id == "" {
		b2id = os.Getenv("B2_ACCOUNT_ID")
	}
	if b2key == "" {
		b2key = os.Getenv("B2_ACCOUNT_KEY")
	}
	if b2key == "" {
		b2key = os.Getenv("B2_SECRET_KEY")
	}

	args := flag.Args()
	if len(args) != 2 {
		return fmt.Errorf("usage: simple [src] [dst]")
	}
	src, dst := args[0], args[1]

	ctx := context.Background()
	c, err := b2.NewClient(ctx, b2id, b2key)
	if err != nil {
		return err
	}

	var r io.ReadCloser

	if strings.HasPrefix(src, "b2://") {
		reader, err := b2Reader(ctx, c, src)
		if err != nil {
			return err
		}
		r = reader
	} else {
		f, err := os.Open(src)
		if err != nil {
			return err
		}
		r = f
	}
	// Readers do not need their errors checked on close.
	defer r.Close()

	if !strings.HasPrefix(dst, "b2://") {
		return downloadToFile(r, dst)
	}
	w, err := b2Writer(ctx, c, dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, r); err != nil {
		return err
	}
	return w.Close()
}

func downloadToFile(r io.Reader, dst string) error {
	f, err := os.CreateTemp(filepath.Dir(dst), ".download-*")
	if err != nil {
		return err
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	if _, err := io.Copy(f, r); err != nil {
		return err
	}
	if info, err := os.Stat(dst); err == nil {
		if err := f.Chmod(info.Mode().Perm()); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), dst)
}

func b2Reader(ctx context.Context, c *b2.Client, path string) (io.ReadCloser, error) {
	o, err := b2Obj(ctx, c, path)
	if err != nil {
		return nil, err
	}
	return o.NewReader(ctx), nil
}

func b2Writer(ctx context.Context, c *b2.Client, path string) (io.WriteCloser, error) {
	o, err := b2Obj(ctx, c, path)
	if err != nil {
		return nil, err
	}
	return o.NewWriter(ctx), nil
}

func b2Obj(ctx context.Context, c *b2.Client, path string) (*b2.Object, error) {
	uri, err := url.Parse(path)
	if err != nil {
		return nil, err
	}
	bucket, err := c.Bucket(ctx, uri.Host)
	if err != nil {
		return nil, err
	}
	// B2 paths must not begin with /, so trim it here.
	return bucket.Object(strings.TrimPrefix(uri.Path, "/")), nil
}
