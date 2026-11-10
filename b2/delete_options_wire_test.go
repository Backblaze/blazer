package b2

import (
	"context"
	"testing"
)

func TestObjectDeleteRejectsNilOptionBeforeRequest(t *testing.T) {
	for _, test := range []struct {
		name string
		opts []DeleteOption
	}{
		{name: "nil", opts: []DeleteOption{nil}},
		{name: "nil first", opts: []DeleteOption{nil, BypassGovernance()}},
		{name: "nil last", opts: []DeleteOption{BypassGovernance(), nil}},
	} {
		t.Run(test.name, func(t *testing.T) {
			srv, bucket := liveOnlyBucket(t)
			srv.Handle("gone", liveOnlyHead("file-id", "gone"))
			srv.StrictReplySequence("b2_delete_file_version")
			before := len(srv.Requests())

			err := bucket.Object("gone").Delete(context.Background(), test.opts...)
			if err == nil || err.Error() != "b2: nil delete option" {
				t.Fatalf("Delete error = %v, want b2: nil delete option", err)
			}
			if got := len(srv.Requests()) - before; got != 0 {
				t.Errorf("requests after nil option = %d, want 0", got)
			}
		})
	}
}
