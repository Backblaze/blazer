# SDK harness contracts

`tests.tsv` is the versioned entry-point contract used by the centralized SDK quality
harness. This repository owns the executable health, conformance and resilience
assertions in `tests/`; the harness supplies a fresh local B2 simulator, orchestration,
evidence retention and reporting. Each assertion builds a tiny Go program against **this
checkout** (a local `replace`, so the revision under test is the one on disk) and runs it
against the simulator.

## What these checks can and cannot reach

- **Only the loopback simulator.** Every check refuses to run unless its simulator URL is a
  bare IPv4 loopback HTTP origin (`http://127.0.0.1:<port>`). There is no staging or
  production mode and no default realm.
- **Only the simulator's fixed test credential** (`test-key-id` / `test-key`). Ambient
  `B2_*` variables are dropped before any check runs; a real key cannot reach the program.
- **No network for the build.** The module graph is this checkout plus the standard
  library, so nothing is fetched (`GOPROXY=off`, `GOTOOLCHAIN=local`), and the check's
  standard HTTP transport refuses any connection that is not to a loopback address
  (`tests/lib/loopback_guard.go.txt`).
- A check that reports `COULD-NOT-RUN` (or `SKIP`) must exit 0; the dispatcher reports a
  nonzero exit after such a verdict as a `FAIL`, never as amber evidence.
- **Only a missing Go toolchain is amber.** `COULD-NOT-RUN (missing-runtime ...)` (health: `SKIP`)
  is reserved for no usable `go` on `PATH`, or a Go older than this checkout requires with
  `GOTOOLCHAIN=local` forbidding a download. A checkout or check that does not compile, a failing
  `go mod tidy` and a dependency that cannot be resolved offline are a `FAIL` carrying the compiler
  output, so a Blazer API or compile regression is never hidden as harmless evidence. This holds at
  every level: because a check only runs against the simulator URL it was given, a `401`/`403`, a
  refused connection, a deadline or a reset is a `FAIL` (a blazer auth, hang or retry regression, or a
  broken simulator), never `COULD-NOT-RUN`.
- **Reusing a simulator.** The resilience checks clear every armed fault (`DELETE /faults`) and
  ignore journal entries that predate them, so running several back to back on one simulator does not
  produce false FAILs. They cannot rewind the simulator clock (`POST /clock` only moves forward), so
  `auth.clock_expiry` still wants a fresh simulator if you run it repeatedly. The harness starts a fresh
  simulator per scenario regardless.

`tests/selftest` pins these properties and needs neither a simulator nor the network.

## Prerequisites

`bash` and a Go toolchain (the module declares Go 1.18; the harness CI
uses Go 1.25.x). Nothing else is installed or downloaded (`curl` and `python3` only for the optional health-bucket snippet below).

## Run one check by hand

Start a simulator (the standalone one from `backblaze-labs/b2-simulator`; the harness's
embedded `sdkharness/bin/simulator/serve.mjs` behaves the same). Resilience needs `--control`:

```bash
node bin/simulator/serve.mjs --control
# SIMULATOR-LISTENING http://127.0.0.1:<port>
# SIMULATOR-LISTENING https://127.0.0.1:<port>
# SIMULATOR-CONTROL   http://127.0.0.1:<control-port>
```

Export what the dispatchers read. (`b2-simulator`'s shell helper `bin/lib/simulator.sh` sets
the same values as `B2SIM_URL`, `B2SIM_HTTPS_URL`, `B2SIM_CA` and `B2SIM_CONTROL_URL`; the
harness maps them onto these names.)

```bash
export SDKHARNESS_SIMULATOR_URL=http://127.0.0.1:<port>
export SDKHARNESS_SIMULATOR_HTTPS_URL=https://127.0.0.1:<https-port>   # optional
export SDKHARNESS_SIMULATOR_CA=/path/to/loopback-cert.pem                # optional
export SDKHARNESS_SIMULATOR_CONTROL_URL=http://127.0.0.1:<control-port> # resilience only
```

Then, from the repository root:

```bash
# conformance
SDKHARNESS_TEST_LEVEL=conformance SDKHARNESS_SCENARIO=files.upload \
  .sdkharness/tests/run-conformance

# resilience (needs the control URL)
SDKHARNESS_TEST_LEVEL=resilience SDKHARNESS_SCENARIO=upload.retry_503 \
  .sdkharness/tests/run-resilience

# customer health: the bucket must already exist in the simulator
SDKHARNESS_TEST_LEVEL=health SDKHARNESS_SCENARIO=golden-path B2_BUCKET_NAME=sdkharness-healthcheck \
  .sdkharness/tests/run-health
```

Each prints one `SDKHARNESS_RESULT<TAB>level<TAB>scenario<TAB>PASS|FAIL|SKIP<TAB>detail`
line (plus diagnostics) and exits 0 for `PASS` and `SKIP`, 1 for `FAIL`. Create the health
bucket against a fresh simulator with:

```bash
auth=$(curl -s -u test-key-id:test-key "$SDKHARNESS_SIMULATOR_URL/b2api/v4/b2_authorize_account")
token=$(printf '%s' "$auth" | python3 -c 'import sys,json; print(json.load(sys.stdin)["authorizationToken"])')
account=$(printf '%s' "$auth" | python3 -c 'import sys,json; print(json.load(sys.stdin)["accountId"])')
curl -s -H "Authorization: $token" \
  -d "{\"accountId\":\"$account\",\"bucketName\":\"sdkharness-healthcheck\",\"bucketType\":\"allPrivate\"}" \
  "$SDKHARNESS_SIMULATOR_URL/b2api/v4/b2_create_bucket"
```

The leaf files under `tests/conformance/`, `tests/resilience/` and `tests/health/` are
not entry points: run directly they refuse anything but the loopback simulator, but the
dispatchers above are the supported way to run them.
