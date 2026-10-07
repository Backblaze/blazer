package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/Backblaze/blazer/b2"
)

const (
	apiID  = "B2_APPLICATION_KEY_ID"
	apiKey = "B2_APPLICATION_KEY"
)

var bucketNameSuffixes = [...]string{
	"consistobucket",
	"base-tests",
	"replication-target",
}

func main() {
	id := os.Getenv(apiID)
	key := os.Getenv(apiKey)
	ctx := context.Background()
	client, err := b2.NewClient(ctx, id, key)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	buckets, err := client.ListBuckets(ctx)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	var kill []string
	for _, bucket := range buckets {
		if strings.HasPrefix(bucket.Name(), fmt.Sprintf("%s-b2-tests-", id)) {
			kill = append(kill, bucket.Name())
		} else {
			for _, suffix := range bucketNameSuffixes {
				if bucket.Name() == fmt.Sprintf("%s-%s", id, suffix) {
					kill = append(kill, bucket.Name())
					break
				}
			}
		}
	}
	var wg sync.WaitGroup
	failures := make(chan struct{}, len(kill))
	for _, name := range kill {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			fmt.Println("removing bucket", name)
			if err := killBucket(ctx, client, name); err != nil {
				fmt.Println(err)
				failures <- struct{}{}
			}
		}(name)
	}
	wg.Wait()
	close(failures)
	hadFailures := false
	for range failures {
		hadFailures = true
	}
	if hadFailures {
		os.Exit(1)
	}
}

func killBucket(ctx context.Context, client *b2.Client, name string) error {
	bucket, err := client.NewBucket(ctx, name, nil)
	if b2.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	failed := false
	iter := bucket.List(ctx, b2.ListHidden())
	for iter.Next() {
		o := iter.Object()
		fmt.Println("deleting file", o.Name())
		if err := o.Delete(ctx); err != nil {
			fmt.Println(err)
			failed = true
		}
	}
	if err = iter.Err(); err != nil {
		fmt.Println(err)
		failed = true
	}
	iter = bucket.List(ctx, b2.ListUnfinished())
	for iter.Next() {
		o := iter.Object()
		fmt.Println("canceling file", o.Name())
		if err := o.Cancel(ctx); err != nil {
			fmt.Println(err)
			failed = true
		}
	}
	if err = iter.Err(); err != nil {
		fmt.Println(err)
		failed = true
	}
	if err := bucket.Delete(ctx); err != nil {
		fmt.Println(err)
		failed = true
	}
	if failed {
		return fmt.Errorf("cleanup failed for bucket %q", name)
	}
	return nil
}
