package b2_test

import (
	"context"

	"github.com/Backblaze/blazer/b2"
	"github.com/Backblaze/blazer/base"
)

// Compile the updated method-type declarations from docs/delete-options.md in CI.
var _ interface {
	Delete(context.Context, ...b2.DeleteOption) error
} = (*b2.Object)(nil)
var _ interface {
	DeleteFileVersion(context.Context, ...bool) error
} = (*base.File)(nil)
var _ func(context.Context, ...b2.DeleteOption) error = (*b2.Object)(nil).Delete
var _ func(context.Context, ...bool) error = (*base.File)(nil).DeleteFileVersion

func deleteCallback(object *b2.Object) func(context.Context) error {
	return func(ctx context.Context) error { return object.Delete(ctx) }
}

func deleteFileCallback(file *base.File) func(context.Context) error {
	return func(ctx context.Context) error { return file.DeleteFileVersion(ctx) }
}

func ExampleObject_Delete_callbacks() {
	var object *b2.Object // Supply the object whose version should be deleted.
	var file *base.File   // Or the low-level file version.
	objectCallback := deleteCallback(object)
	fileCallback := deleteFileCallback(file)
	_ = objectCallback
	_ = fileCallback
}
