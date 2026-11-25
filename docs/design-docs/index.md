# Design documents

## Accepted decisions

- [Use functional options for new knobs](../../AGENTS.md#conventions). Options that conflict within one call return an error.

## Accepted nos

- [Do not derive an SSE-C key ID from the key](../b2-contracts.md). The caller chooses the label separately from the key and its fingerprint.
- [Do not expose bucket names instead of IDs](../product-specs/key-restrictions/spec.md#rejected-alternatives). Resolving names needs an extra call per key and can fail.

## Threat models

- [Credentials](threat-model-credentials.md)
