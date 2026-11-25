# Design documents

## Accepted decisions

- [Use functional options for new knobs](../../AGENTS.md#conventions). Conflicting options return an error.

## Accepted nos

- [Do not derive an SSE-C key ID from the key](../b2-contracts.md). An ID must not expose key material.
- [Do not resolve bucket IDs to names](../product-specs/key-restrictions/spec.md#rejected-alternatives). Resolution needs extra calls and can fail.

## Threat models

- [Credentials](threat-model-credentials.md)
