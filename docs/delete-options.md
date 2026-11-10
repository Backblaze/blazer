# Governance-bypass deletion

`object.Delete(ctx)` deletes an ordinary version without requesting a retention bypass.
`object.Delete(ctx, b2.BypassGovernance())` requests a governance-retention bypass. The
application key must have the `bypassGovernance` capability; the server enforces it.
At the base layer, `file.DeleteFileVersion(ctx)` and `file.DeleteFileVersion(ctx, false)`
omit the wire flag, while `file.DeleteFileVersion(ctx, true)` sends `bypassGovernance: true`.
A nil `b2.DeleteOption` returns an error before any request is sent.

## Migrating method types from before v0.9.0

The optional parameters were introduced in **v0.9.0**, not v0.10.0. Calls without options
still compile, but adding a variadic parameter changed each method's type. Interfaces and
function values must include the trailing parameter too.

For example, these pre-v0.9.0 declarations do not compile against v0.9.0 or later:

```go
package migration

import (
	"context"

	"github.com/Backblaze/blazer/b2"
	"github.com/Backblaze/blazer/base"
)

var _ interface{ Delete(context.Context) error } = (*b2.Object)(nil)
var _ interface{ DeleteFileVersion(context.Context) error } = (*base.File)(nil)
var _ func(context.Context) error = (*b2.Object)(nil).Delete
var _ func(context.Context) error = (*base.File)(nil).DeleteFileVersion
```

Update interfaces and function values to match the selected method signatures:

```go
package migration

import (
	"context"

	"github.com/Backblaze/blazer/b2"
	"github.com/Backblaze/blazer/base"
)

var _ interface {
	Delete(context.Context, ...b2.DeleteOption) error
} = (*b2.Object)(nil)
var _ interface {
	DeleteFileVersion(context.Context, ...bool) error
} = (*base.File)(nil)
var _ func(context.Context, ...b2.DeleteOption) error = (*b2.Object)(nil).Delete
var _ func(context.Context, ...bool) error = (*base.File)(nil).DeleteFileVersion

// A callback that must keep its old type can wrap the option-free call.
func deleteCallback(object *b2.Object) func(context.Context) error {
	return func(ctx context.Context) error { return object.Delete(ctx) }
}

func deleteFileCallback(file *base.File) func(context.Context) error {
	return func(ctx context.Context) error { return file.DeleteFileVersion(ctx) }
}
```

Other implementations of a shared deletion interface must also match its chosen signature.
An adapter with the old method signature is another option for code that must keep a shared
interface unchanged. Pre-1.0 breaking changes are permitted in minor releases, with migration
guidance. This follow-up preserves the shipped signatures. The separate base API-design
decision remains [#102](https://github.com/Backblaze/blazer/issues/102); the general upgrade
guide is tracked in [#314](https://github.com/Backblaze/blazer/issues/314).

## Verification boundaries

`TestObjectDeleteBypassGovernanceReachesTheWire` and
`TestDeleteFileVersionBypassGovernanceOnTheWire` pin the request fields. They do not prove
that a service enforces retention or capabilities. The in-process `x/b2sim` explicitly
does not enforce either policy, so it cannot prove a governance refusal.

`TestDeleteBypassGovernanceLive` in `b2/lock_test.go` checks both API layers against real B2:
active retention readback, a `401/access_denied` without bypass, successful deletion with
bypass, and absence of the deleted version from a fresh version listing. It requires
`B2_APPLICATION_KEY_ID` and `B2_APPLICATION_KEY`, creates its own Object Lock bucket, and
cleans up with governance bypass on a fresh bounded context. Run it with:

```sh
go test -count=1 -v -run '^TestDeleteBypassGovernanceLive$' ./b2
```

**Compliance enforcement is UNVERIFIED by this live check.** The live validation policy
forbids creating compliance-retained test objects because cleanup cannot delete them before
expiry. This exception does not change the B2 contract: governance bypass never overrides
compliance retention. A scripted rejection is not evidence of service enforcement.

Sources: [B2 delete file version](https://www.backblaze.com/apidocs/b2-delete-file-version)
and [B2 Object Lock](https://www.backblaze.com/docs/cloud-storage-object-lock).
