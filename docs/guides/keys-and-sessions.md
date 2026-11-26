---
type: guide
title: "Keys and sessions"
description: "Use an application key with only the capabilities and bucket scope your application needs."
status: active
owner: ["@goanpeca", "@sophiecarreras", "@ffumero2003", "@david-santacruz-backblaze"]
stale_after: 2027-05-26T00:00:00+00:00
---
# Keys and sessions

Use an application key with only the capabilities and bucket scope your application needs. Avoid master keys for routine work. Protect the key ID and secret.

`WithAccountInfo` accepts a session store. `NewAccountInfoFile` persists authorization data. Protect its file and parent directory. The store validates file permissions and exposes persistence errors through `Err`. The default is an in-memory store.

```go
package main

import (
	"context"

	"github.com/Backblaze/blazer/b2"
)

func connect(ctx context.Context, id, secret, path string) (*b2.Client, error) {
	store := b2.NewAccountInfoFile(path)
	client, err := b2.NewClient(ctx, id, secret, b2.WithAccountInfo(store))
	if err != nil {
		return nil, err
	}
	if err := store.Err(); err != nil {
		return nil, err
	}
	return client, nil
}
func main() {}
```
