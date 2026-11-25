# Credential threat model

## Assets and flow

- The application key ID and secret enter through `b2.NewClient` ([b2/b2.go:61](../../b2/b2.go#L61)). Authorization uses them to build a Basic authorization header ([base/base.go:842](../../base/base.go#L842)).
- B2 returns an authorization token. Blazer stores authorized session data in `AuthData` ([base/base.go:539](../../base/base.go#L539)). `AccountInfoFile` can persist it ([b2/accountinfo.go:106](../../b2/accountinfo.go#L106)).
- `NewSSECKey` copies a 32-byte SSE-C key ([b2/ssec.go:40](../../b2/ssec.go#L40)). Blazer encodes the key for customer-encryption headers ([b2/ssec.go:94](../../b2/ssec.go#L94), [base/base.go:1406](../../base/base.go#L1406)).

- `Bucket.AuthToken` returns a download authorization token for a prefix. `Object.AuthURL` puts that token in the query string of an object URL ([b2/b2.go:1081](../../b2/b2.go#L1081), [b2/b2.go:1091](../../b2/b2.go#L1091)).

## Exposure paths

Credentials can leak through logs, error messages, formatted values, HTTP endpoints or response bodies. Callers can format values with `String`, `%v`, `%+v` or `%x`. Callers can also log data outside Blazer's controls.

A URL from `Object.AuthURL` carries its token in the query string. Logs, browser history and referrer headers can record that URL. Share it only as long as the token's lifetime allows.

## Existing controls

- Request and response logging redacts sensitive JSON fields. It omits error bodies and authorization responses ([base/base.go:328](../../base/base.go#L328), [base/base.go:363](../../base/base.go#L363), [base/base.go:395](../../base/base.go#L395)). The private logger removes line breaks ([internal/blog/blog.go:45](../../internal/blog/blog.go#L45)).
- SSE-C key formatting redacts every verb ([b2/ssec.go:80](../../b2/ssec.go#L80)). Credential containers use redacting formatters ([internal/credentialformat/format.go:25](../../internal/credentialformat/format.go#L25)). `AuthData` redacts its token ([base/base.go:553](../../base/base.go#L553)).
- Blazer redacts download IDs from error text ([base/base.go:433](../../base/base.go#L433)). Transport logs omit error text ([base/base.go:670](../../base/base.go#L670)).
- The default endpoint for client authorization uses HTTPS ([base/base.go:47](../../base/base.go#L47)). `APIBase` requires HTTPS except for loopback HTTP. `InsecureAPIBase` explicitly permits non-loopback HTTP ([b2/b2.go:161](../../b2/b2.go#L161), [b2/b2.go:189](../../b2/b2.go#L189)).
- The session file is written with mode 0600 ([b2/accountinfo.go:195](../../b2/accountinfo.go#L195)). On non-Windows systems, reads check mode 0600 ([b2/accountinfo.go:292](../../b2/accountinfo.go#L292)).

## Known gaps

- Blazer reuses the API, download and S3 addresses of a restored session after checking only that they are not empty. The HTTPS rule of `APIBase` does not apply to them ([b2/backend.go:276](../../b2/backend.go#L276), [b2/backend.go:303](../../b2/backend.go#L303), [base/base.go:594](../../base/base.go#L594)). Changing them needs write access to the session store, which already exposes the stored token. Tracking: [#540](https://github.com/Backblaze/blazer/issues/540).
- Windows mode bits do not enforce a private directory ACL. Use a private profile directory ([b2/accountinfo.go:104](../../b2/accountinfo.go#L104)). Tracking: no issue named in the code.
- Wrapped transport errors retain their underlying cause ([base/base.go:80](../../base/base.go#L80), [base/base.go:710](../../base/base.go#L710)). Callers who inspect or log that cause can expose its text. Tracking: no issue named in the code.
