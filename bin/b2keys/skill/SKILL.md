---
name: b2keys
description: Create a scoped B2 application key with the b2keys tool. Use when asked to provision an application key.
---

# Create a B2 application key

Use the `b2keys` tool to create a key. Install it with `go install github.com/Backblaze/blazer/bin/b2keys@latest`, or run it from a checkout with `go run ./bin/b2keys`. Never use a master key for routine work.

## Flags

- `-bucket`: restrict the key to one bucket by name.
- `-duration`: set the key lifetime as a Go duration, such as `1h`. The value must be zero or positive. If omitted or zero, the tool sends no lifetime and the key does not expire. Use a positive duration for a key that must expire. For a negative value, the tool reports `duration must not be negative` and creates no key.
- `-prefix`: restrict the key to file names starting with the given prefix.

## Workflow

1. Identify the operations and choose only the capabilities they need. Use the `Needs:` lines in the README examples. The issuing key needs `writeKeys`, and bucket lookup needs `listBuckets`.
2. Choose one bucket and a matching name prefix. For routine keys, prefer a short positive duration. Pass the key name and at least one capability after the flags.
3. Supply the issuing key ID and secret through `B2_APPLICATION_KEY_ID` and `B2_APPLICATION_KEY` in the environment. Obtain them from a secure source without printing them. The deprecated fallbacks are `B2_ACCOUNT_ID`, `B2_SECRET_KEY` and `B2_ACCOUNT_KEY` (in that secret fallback order).
4. The tool prints the new key ID and secret to standard output. Redirect it to a restricted file outside the repository; deliver it through an approved secure channel. Never print, log, echo, or commit a key secret.

```sh
umask 077
b2keys create -bucket BUCKET -prefix PREFIX -duration 1h KEY_NAME readFiles > /secure/path/b2keys-output
```

The output file contains a secret. Keep it private and delete it when no longer needed.
