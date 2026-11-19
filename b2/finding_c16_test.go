package b2

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type c16OffsetBucket struct {
	beBucketInterface
	offsets chan int64
}

func (b c16OffsetBucket) downloadFileByName(_ context.Context, _ string, offset, _ int64, _ bool, _ *SSECKey) (beFileReaderInterface, error) {
	b.offsets <- offset
	return nil, errors.New("offset captured")
}

// The old multiplication overflows only with 32-bit int. CI executes this test
// on linux/386; a 64-bit run alone cannot detect the regression.
func TestFindingC16_ChunkOffsetMultipliedIn64Bit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	offsets := make(chan int64, 1)
	r := &Reader{
		ctx: ctx, cancel: cancel,
		o:     &Object{b: &Bucket{b: c16OffsetBucket{offsets: offsets}}},
		csize: 10_000_000, chwid: 300, chbuf: make(chan *rchunk, 1),
	}
	r.rcond = sync.NewCond(&r.rmux)
	r.thread()
	defer func() {
		cancel()
		r.wg.Wait()
	}()
	r.chbuf <- &rchunk{}
	select {
	case got := <-offsets:
		if want := int64(3_000_000_000); got <= 0 || got != want {
			t.Fatalf("chunk offset = %d, want positive %d", got, want)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for the chunk offset")
	}
}
