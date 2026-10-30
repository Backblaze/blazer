package window

import (
	"fmt"
	"testing"
	"time"
)

func sum(i, j interface{}) interface{} {
	var total int
	if i != nil {
		total += i.(int)
	}
	if j != nil {
		total += j.(int)
	}
	return total
}

func TestNewUsesOneBucketWhenSizeIsLessThanResolution(t *testing.T) {
	w := New(500*time.Millisecond, time.Second, sum)
	w.Insert(2)
	if got := w.Reduce(); got != 2 {
		t.Fatalf("Reduce() = %v, want 2", got)
	}
}

func TestNewRejectsZeroResolutionForFiniteWindow(t *testing.T) {
	defer func() {
		if got := recover(); got == nil || fmt.Sprint(got) != "window: resolution must be positive for a finite window" {
			t.Fatalf("New panic = %v, want documented resolution error", got)
		}
	}()
	_ = New(time.Second, 0, sum)
}

func TestNewForeverAllowsZeroResolution(t *testing.T) {
	w := New(0, 0, sum)
	w.Insert(2)
	w.Insert(3)
	if got := w.Reduce(); got != 5 {
		t.Fatalf("Reduce() = %v, want 5", got)
	}
}
