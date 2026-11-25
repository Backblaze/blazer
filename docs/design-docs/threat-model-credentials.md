# Credential threat model

## Assets and flow

- The application key ID and secret enter through `b2.NewClient` ([b2/b2.go:61](../../b2/b2.go#L61)). Authorization uses them to build a Basic authorization header ([base/base.go:842](../../base/base.go#L842)).
- B2 returns an authorization token. Blazer stores authorized session data in `AuthData` ([base/base.go:521](../../base/base.go#L521)). `AccountInfoFile` can persist it ([b2/accountinfo.go:101](../../b2/accountinfo.go#L101)).
- `NewSSECKey` copies a 32-byte SSE-C key ([b2/ssec.go:40](../../b2/ssec.go#L40)). Blazer encodes the key for customer-encryption headers ([b2/ssec.go:94](../../b2/ssec.go#L94), [base/base.go:1405](../../base/base.go#L1405)).

## Exposure paths

Credentials can leak through logs, error messages, formatted values, HTTP endpoints or response bodies. Callers can format values with `String`, `%v`, `%+v` or `%x`. Callers can also log data outside Blazer's controls.

## Existing controls

- Request and response logging redacts sensitive JSON fields. It omits error bodies and authorization responses ([base/base.go:329](../../base/base.go#L329), [base/base.go:363](../../base/base.go#L363), [base/base.go:396](../../base/base.go#L396)). The private logger removes line breaks ([internal/blog/blog.go:39](../../internal/blog/blog.go#L39)).
- SSE-C key formatting redacts every verb ([b2/ssec.go:76](../../b2/ssec.go#L76)). Credential containers use redacting formatters ([internal/credentialformat/format.go:25](../../internal/credentialformat/format.go#L25)). `AuthData` redacts its token ([base/base.go:553](../../base/base.go#L553)).
- Blazer redacts download IDs from error text ([base/base.go:433](../../base/base.go#L433)). Transport logs omit error text ([base/base.go:670](../../base/base.go#L670)).
- The default API endpoint uses HTTPS ([base/base.go:47](../../base/base.go#L47)). `APIBase` allows HTTP for loopback; `InsecureAPIBase` permits an explicit exception ([b2/b2.go:161](../../b2/b2.go#L161), [b2/b2.go:184](../../b2/b2.go#L184)).
- The session file is written with mode 0600 ([b2/accountinfo.go:195](../../b2/accountinfo.go#L195)). On non-Windows systems, reads check mode 0600 ([b2/accountinfo.go:285](../../b2/accountinfo.go#L285)).

## Known gaps

- Windows mode bits do not enforce a private directory ACL. Use a private profile directory ([b2/accountinfo.go:105](../../b2/accountinfo.go#L105)). Tracking: no issue named in the code.
- Wrapped transport errors retain their underlying cause ([base/base.go:80](../../base/base.go#L80), [base/base.go:707](../../base/base.go#L707)). Callers who inspect or log that cause can expose its text. Tracking: no issue named in the code.
