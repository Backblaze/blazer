# Review lessons

Problems that earlier reviews of blazer found, grouped by theme, with the PRs where they came up. Use it
as a checklist before asking for review, and add a line when a review finds something that will matter
again. Rules that apply to every change are in [AGENTS.md](../AGENTS.md); B2 facts with sources are in
[b2-contracts.md](b2-contracts.md).

## Secrets and encryption

- An option given `nil` is a request for something, not "no option": `WithSSECKey(nil)`,
  `CopySourceSSECKey(nil)` and `CopyDestinationSSECKey(nil)` must fail before any request. (#122, #160)
- A zero-value secret type must not be usable; build keys only through their constructors. (#160)
- `String` and `GoString` do not cover `%d`, `%x` or a secret inside another struct printed with `%+v`.
  Test every verb on the value, the pointer, and exported and unexported struct fields. (#122, #160)
- Redact request and response headers and JSON bodies alike. Match field names after JSON decoding:
  casing, nesting and escaped names must not bypass redaction. (#117, #160)
- A key that is only meant for one request must not end up in client defaults. (#122)
- Never derive a stored identifier from secret bytes. (#153)
- Before resuming an encrypted upload, verify its encryption settings; when they cannot be verified,
  refuse. (#122, #125, #153)
- A copy has two encryption contexts: the source key decrypts, the destination key encrypts. (#122)
- A corrupt cached session can fall back to authorizing again; a cache that is readable by others or holds
  the wrong identity must fail closed. Suppress the whole authorize response in logs. (#142)

## Compatibility

- Adding a variadic parameter keeps call sites compiling but changes the method type, which breaks code
  that stores the method as a function value. Mark it breaking. (#122, #160)
- Returned maps, `nil` versus empty, and which keys appear in `Attrs.Info` are public behavior. A fix that
  changes them is a second change. (#107, #134)
- New exported identifiers in `base` and other packages need the same review as those in `b2`. (#142)
- A new positional parameter on a `base` function, or a new behavior for an existing field, needs a
  `**Breaking:**` changelog entry that says what callers must do. (#124)
- A per-call option may override a client default, but two conflicting options in one call are an
  error. Test both orders. (#122, #125)

## State, cancellation and cleanup

- Hidden configuration (`isClientAuthorizedToRead: false`) is not absent configuration. Do not build a
  replacement from it. (#137)
- Cached maps belong to the cache: copy before returning, and test repeated calls and failed updates on
  the same handle. (#74, #86, #134)
- Batch operations record per-item failures and continue; only context cancellation stops the run.
  Report what failed. (#135)
- Multi-step operations undo or report what they created, on a fresh context. (#51, #106, #129, #137)
- Cancellation must interrupt blocked bodies, hashing and buffering, not only check before starting.
  (#129, #135, #147)
- Never call user callbacks or do network I/O while holding a lock; every waiter needs an exit on
  cancellation. (#61, #66, #131, #145)
- Shared fields follow one ownership rule; 64-bit atomics need alignment on 32-bit targets. (#131, #145, #155)

## Requests, retries and downloads

- A fake backend or a canned response does not prove the request carries a new field; assert the request
  on the wire. (#107, #124)
- Correct bytes back do not prove a correct request: assert offsets, the number of calls, and zero
  writes when a call is rejected. (#82, #121, #137)
- Retries and reauthorization go through the same wrapper as sibling calls; honor `Retry-After`; a failed
  part upload renews its upload URL. (#63, #81, #129)
- Downloads keep stored bytes: request `identity` encoding so length and SHA1 match what B2 stores. (#127)
- Bounded, zero-length and unbounded reads are different states. (#121, #155)
- Extract shared request and response handling instead of copying a sibling path. (#79, #118, #127)

## Tests

- Revert the fix and the test must fail; prove it in the PR. (#124, #145, #153)
- A fake must model state changes; a fake that always returns the expected value cannot fail. (#79, #124)
- Exercise both `Write` and `ReadFrom`, simple and multipart paths. (#131, #147)
- Use realistic part sizes when testing limits; small simulator defaults do not prove B2 limits. (#129, #147)
- Synchronize concurrency tests with signals, not sleeps. (#63, #66, #143)
- A missing tool or skipped check is NOT RUN, not PASS. (#106)

## PR hygiene

- The description must match the code after the last commit, including behavior changes and new exports.
  (#142, #155)
- Every review finding gets an answer: fixed with a test, or deferred with an issue link. A PR that defers
  part of an issue must not say `Closes`. (#127, #135, #160)
- Stacked PRs conflict on shared files (changelog fragments, harness contract counts, interfaces); state
  the merge order. (#145, #160)
