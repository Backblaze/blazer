# Intent: Writer upload contract
Author: Felipe Fumero. Issue: [#528](https://github.com/Backblaze/blazer/issues/528). Status: draft.

## Problem

The Writer contract spans godoc, implementation, tests and change specs. Callers need one place to find its upload behavior and limits.

## Proposed outcome

The Writer sends a source stream to a named object in B2. Callers can upload small objects and large files through one API.
They can select upload attributes, concurrency, buffering, encryption and Object Lock settings. They must check the final upload result.
This standing spec records current behavior and identifies requirements without a test pin.

## Affected users and APIs

Go applications that upload through `Object.NewWriter`, `Writer.Write`, `Writer.ReadFrom` and `Writer.Close` use this contract.
It also covers public Writer fields, Writer options and client default Writer options.

## Constraints

- Describe the code at `2df47dc`. Do not change upload behavior.
- Distinguish SDK validation from B2 service limits.
- Cite code and tests for each requirement. State when no test pins a requirement.
- Keep the [Writer Object Lock change spec](../writer-object-lock/spec.md) and
  [SHA1 upload and verification change spec](../sha1-upload-and-verify/spec.md) as separate records.
- Use the [B2 contracts](../../b2-contracts.md), [Object Lock guide](../../object-lock.md),
  [delete options guide](../../delete-options.md) and [large-upload guide](../../guides/large-and-resumable-uploads.md) as supporting context.

## Open questions

See the standing spec's concerns for differences between documented service limits, local checks and test coverage.
