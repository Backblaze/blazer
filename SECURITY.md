# Security Policy

Security-response owner: Backblaze Security. Repository security-review owners: @goanpeca and
@sophiecarreras.

## Supported Versions

The latest minor release line (currently `0.9.x`) is supported. Earlier releases are not patched in
place; upgrade to the newest `v0.x` tag to receive fixes. Once 1.0 ships, only the latest minor release
receives security patches. See [`RELEASE.md`](RELEASE.md).

| Version | Supported |
|---|---|
| Latest minor (`0.9.x`) | Yes |
| Older releases | No |

## Reporting a Vulnerability

**Do not file a public GitHub issue for a security vulnerability.**

Instead, report it privately via GitHub Security Advisories:

1. Go to the [Security tab](https://github.com/Backblaze/blazer/security/advisories/new) of this repository
2. Click **Report a vulnerability**
3. Provide a clear description, the affected version, reproduction steps, and impact assessment

You can also email **security@backblaze.com** if you cannot use GitHub Security Advisories.

## What to expect

- Acknowledgement within 3 business days
- Initial assessment within 7 business days
- Coordinated disclosure once a fix is available, shipped as a patch release with a `Security` changelog
  entry and a GitHub security advisory

## Scope

In scope:

- Credential-handling flaws in the library (`b2`, `base`, `internal`, `x`) or `bin/b2keys`: application
  keys, authorization tokens or SSE-C keys exposed in logs, errors or to unintended hosts
- Transport flaws: TLS verification bypassed, or requests sent outside the key's bucket or prefix scope
- Denial of service reachable through the public API, such as unbounded retries or goroutine leaks
  triggered by server responses

Out of scope:

- Vulnerabilities in the Backblaze B2 service itself — report those to Backblaze directly
- Misuse of credentials by the calling application, such as embedding a master key in client code
- Issues requiring root or local access on the machine running the application

## Security-relevant areas

- **Credentials**: application keys are passed to `b2.NewClient` and sent only to the B2 authorize
  endpoint; the authorization token returned is held in memory and sent in the `Authorization` header.
- **SSE-C keys**: customer-provided encryption keys travel in request headers on uploads and downloads,
  and in JSON request bodies on large-file start (`serverSideEncryption`) and server-side copy
  (`sourceServerSideEncryption`, `destinationServerSideEncryption`). They are not persisted. If you log
  requests, redact both headers and bodies.
- **Transport**: all B2 API traffic uses HTTPS. Certificate verification failures stop retries and
  return the original error; they are never treated as retryable.
- **Tests and CI**: live tests read `B2_APPLICATION_KEY_ID` and `B2_APPLICATION_KEY` from the
  environment; CI keeps them in repository secrets used only by `test-live.yml`. The `.sdkharness`
  checks refuse any non-loopback endpoint and drop ambient `B2_*` variables.

## Best practices for users

- Use application keys scoped to specific buckets, prefixes and capabilities.
- Never embed an application key in code shipped to end users; issue download authorizations from a
  backend instead.
- Rotate application keys regularly.
- Use SSE-B2 or SSE-C for encryption at rest, and Object Lock for compliance-sensitive data.
