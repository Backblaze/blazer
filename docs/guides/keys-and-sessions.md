# Keys and sessions

Use an application key with only the capabilities and bucket scope your application needs. Avoid master keys for routine work. Protect the key ID and secret.

`WithAccountInfo` accepts a session store. `NewAccountInfoFile` persists authorization data; protect its file and parent directory. The store validates file permissions and exposes persistence errors through `Err`. The default is an in-memory store.

```go
package main

import (
    "context"
    "github.com/Backblaze/blazer/b2"
)

func connect(ctx context.Context, id, secret, path string) (*b2.Client, error) {
    store := b2.NewAccountInfoFile(path)
    client, err := b2.NewClient(ctx, id, secret, b2.WithAccountInfo(store))
    if err != nil { return nil, err }
    if err := store.Err(); err != nil { return nil, err }
    return client, nil
}
func main() {}
```
