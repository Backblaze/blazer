package consistent

import (
	"bytes"
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/Backblaze/blazer/b2"
	"github.com/Backblaze/blazer/x/b2sim"
)

func newSimBucket(t *testing.T, ctx context.Context) *b2.Bucket {
	t.Helper()
	client, err := b2.NewClient(ctx, "test-id", "test-key", b2.Transport(b2sim.New()))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "consistent", nil)
	if err != nil {
		t.Fatal(err)
	}
	return bucket
}

func inc(b []byte) ([]byte, error) {
	n := 0
	if len(b) != 0 {
		var err error
		n, err = strconv.Atoi(string(b))
		if err != nil {
			return nil, err
		}
	}
	return []byte(strconv.Itoa(n + 1)), nil
}

func TestOperateAppliesSequentialUpdates(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	g := NewGroup(newSimBucket(t, ctx), "consistent")

	for range 5 {
		if err := g.Operate(ctx, "counter", inc); err != nil {
			t.Fatal(err)
		}
	}

	var got []byte
	if err := g.Operate(ctx, "counter", func(b []byte) ([]byte, error) {
		got = append(got[:0], b...)
		return b, nil
	}); err != nil {
		t.Fatal(err)
	}
	if string(got) != "5" {
		t.Errorf("counter = %q, want %q", got, "5")
	}
}

func TestOperateJSONRoundTrips(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	g := NewGroup(newSimBucket(t, ctx), "consistent")
	type counter struct{ N int }

	for range 3 {
		if err := g.OperateJSON(ctx, "counter", counter{}, func(value interface{}) (interface{}, error) {
			current := value.(*counter)
			return &counter{N: current.N + 1}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	var got int
	if err := g.OperateJSON(ctx, "counter", counter{}, func(value interface{}) (interface{}, error) {
		current := value.(*counter)
		got = current.N
		return current, nil
	}); err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Errorf("counter = %d, want %d", got, 3)
	}
}

func TestMutexLockUnlockUncontended(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	g := NewGroup(newSimBucket(t, ctx), "consistent")
	m := g.Mutex(ctx, "m")

	// Mutex stores []byte{1} while held and nothing once released.
	stored := func() []byte {
		t.Helper()
		var got []byte
		if err := g.Operate(ctx, "m", func(b []byte) ([]byte, error) {
			got = append([]byte(nil), b...)
			return b, nil
		}); err != nil {
			t.Fatal(err)
		}
		return got
	}
	for cycle := 1; cycle <= 2; cycle++ {
		m.Lock()
		if got := stored(); !bytes.Equal(got, []byte{1}) {
			t.Fatalf("cycle %d: stored after Lock = %v, want [1]", cycle, got)
		}
		m.Unlock()
		if got := stored(); len(got) != 0 {
			t.Fatalf("cycle %d: stored after Unlock = %v, want empty", cycle, got)
		}
	}
}
