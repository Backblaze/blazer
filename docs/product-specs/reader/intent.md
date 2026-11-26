---
type: product-intent
title: "Intent: Reader download contract"
description: "Intent behind the spec for: Reader download contract."
status: draft
owner: ["@goanpeca", "@sophiecarreras", "@ffumero2003", "@david-santacruz-backblaze"]
stale_after: 2027-05-26T00:00:00+00:00
---
# Intent: Reader download contract
Author: Felipe Fumero. Issue: [#528](https://github.com/Backblaze/blazer/issues/528). Status: draft.

## Problem

The Reader contract spans code, godoc and tests. Callers need one place to find its download behavior and limits.

## Proposed outcome

The Reader provides object bytes to Go applications. Callers can read an object or a range and check the download result.
This standing spec records current behavior and identifies requirements without a test pin.

## Affected users and APIs

Applications using `Object.NewReader`, `Object.NewRangeReader`, `Reader.Read`, `Reader.Close` and `Reader.Verify` use this contract.

## Constraints

- Describe the code at `286e90d`. Do not change download behavior.
- Distinguish local validation from B2 service behavior.
- Cite code and tests for each requirement. State when no test pins a requirement.

## Open questions

See the standing spec's concerns for concurrency and test coverage.
