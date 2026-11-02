package window

import (
	"fmt"
	"testing"
	"time"
)

func sum(i, j interface{}) interface{} {
	if i == nil && j == nil {
		return nil
	}
	var total int
	if i != nil {
		total += i.(int)
	}
	if j != nil {
		total += j.(int)
	}
	return total
}

func TestNewOneBucketExpiresAcrossResolutionBoundary(t *testing.T) {
	start := time.Unix(0, 0)
	w := New(500*time.Millisecond, time.Second, sum)
	w.insertAt(start, 1)
	if got := w.reducedAt(start.Add(900 * time.Millisecond)); got != 1 {
		t.Fatalf("Reduce before resolution boundary = %v, want 1", got)
	}
	if got := w.reducedAt(start.Add(1800 * time.Millisecond)); got != nil {
		t.Fatalf("Reduce after resolution boundary = %v, want nil", got)
	}
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
