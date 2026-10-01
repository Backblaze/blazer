#!/usr/bin/env bash

sdkharness_valid_loopback_origin() {
  local value="$1" port
  case "$value" in http://127.0.0.1:[0-9]*|http://127.0.0.1:[0-9]*/) ;; *) return 1 ;; esac
  port="${value#http://127.0.0.1:}"
  port="${port%/}"
  case "$port" in ''|*[!0-9]*) return 1 ;; esac
  [ "$port" -ge 1 ] 2>/dev/null && [ "$port" -le 65535 ] 2>/dev/null
}

# Environment every Go check runs under. The module graph is only blazer itself
# (a local `replace`) and the standard library, so nothing is fetched: forbid it.
sdkharness_go_offline_env() {
  export GOPROXY=off GOSUMDB=off GOFLAGS=-mod=mod GOTOOLCHAIN=local GOWORK=off
}

# sdkharness_leaf_guard <target-var> <url-var>...
# A check may only run against the loopback simulator with the simulator's fixed
# credential. It refuses (returns 1 after printing why on stderr) when the target
# names anything but the simulator, when a named URL is not an IPv4 loopback
# origin, or when it is unset; and it removes every ambient B2_* variable so a
# real key can never reach the program. The dispatcher in this directory is the
# supported entry point; this makes running a leaf directly equally safe.
sdkharness_leaf_guard() {
  local targetvar="$1" t var url name
  shift
  if [ -n "$targetvar" ]; then
    t="${!targetvar:-simulator}"
    [ "$t" = simulator ] || { SDKHARNESS_GUARD_REASON="configuration -- only the loopback simulator is allowed, not '$t'"; return 1; }
  fi
  for var in "$@"; do
    url="${!var:-}"
    sdkharness_valid_loopback_origin "$url" || { SDKHARNESS_GUARD_REASON="configuration -- $var must be an IPv4 loopback HTTP origin"; return 1; }
  done
  for name in $(compgen -e); do
    case "$name" in B2_*) unset "$name" ;; esac
  done
  return 0
}

# sdkharness_go_build <module-dir> <binary-name>
# Builds the generated check in <module-dir> (offline) and CLASSIFIES a failure, because only
# an absent or unusable Go toolchain is a missing runtime (amber). Anything else -- a compile
# error in blazer or in this check, a failing `go mod tidy`, a dependency that cannot be
# resolved offline -- is a defect to see, not harmless evidence.
# Returns 0 on success; 10 when the toolchain is conclusively unusable (the reason is in
# SDKHARNESS_BUILD_REASON); 20 for every other failure. SDKHARNESS_BUILD_OUT holds the tool output.
sdkharness_go_build() {
  local dir="$1" bin="$2" rc
  SDKHARNESS_BUILD_OUT="" SDKHARNESS_BUILD_REASON=""
  if ! command -v go >/dev/null 2>&1; then
    SDKHARNESS_BUILD_REASON="no Go toolchain on PATH"
    return 10
  fi
  if ! SDKHARNESS_BUILD_OUT="$(go version 2>&1)"; then
    SDKHARNESS_BUILD_REASON="the Go toolchain on PATH does not run"
    return 10
  fi
  SDKHARNESS_BUILD_OUT="$( (cd "$dir" && go mod tidy && go build -o "$bin" .) 2>&1 )"
  rc=$?
  [ "$rc" -eq 0 ] && return 0
  # GOTOOLCHAIN=local forbids fetching a newer toolchain: when the module graph wants one the
  # installed Go is too old for this checkout. That is the only toolchain diagnostic Go emits
  # in this exact, conclusive form; match it precisely so a real compile error never lands here.
  case "$SDKHARNESS_BUILD_OUT" in
    *"requires go >= "*"GOTOOLCHAIN=local)"*)
      SDKHARNESS_BUILD_REASON="the installed Go is older than this checkout requires (GOTOOLCHAIN=local forbids a download)"
      return 10 ;;
  esac
  return 20
}
