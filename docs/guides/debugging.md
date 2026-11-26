---
type: guide
title: "Debugging"
description: "Set B2_LOG_LEVEL=2 in the environment before starting the program to enable request and response diagnostics."
status: active
owner: ["@goanpeca", "@sophiecarreras", "@ffumero2003", "@david-santacruz-backblaze"]
stale_after: 2027-05-26T00:00:00+00:00
---
# Debugging

Set `B2_LOG_LEVEL=2` in the environment before starting the program to enable request and response diagnostics. Level 1 includes selected warnings; the default is level 0. Blazer reads the variable during package initialization and writes through Go's standard `log` package. There is no exported blazer function to change the level at runtime.

The logger excludes sensitive header names and redacts JSON fields whose names indicate credentials, tokens, customer keys, cookies or authentication. It redacts custom-header values, omits error response bodies, and redacts authorization responses and download-by-ID URLs and responses. Unparseable JSON is omitted. Other data, including object names and non-sensitive JSON fields, can still appear; treat logs as sensitive.
