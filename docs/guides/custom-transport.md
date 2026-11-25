# Custom transport

Pass an `http.RoundTripper` with `b2.Transport`. For example, configure connection and response-header timeouts:

```go
package main

import (
    "context"
    "net"
    "net/http"
    "time"
    "github.com/Backblaze/blazer/b2"
)

func connect(ctx context.Context, id, secret string) (*b2.Client, error) {
    transport := http.DefaultTransport.(*http.Transport).Clone()
    transport.DialContext = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
    transport.ResponseHeaderTimeout = 30 * time.Second
    return b2.NewClient(ctx, id, secret, b2.Transport(transport))
}
func main() {}
```

You can also configure a proxy or wrap a transport for instrumentation. Blazer's retry and reauthorization logic calls the supplied transport for each attempt. Avoid logging credentials or request bodies in your wrapper. Use context deadlines to bound the overall operation.
