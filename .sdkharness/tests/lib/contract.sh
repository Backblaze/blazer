#!/usr/bin/env bash

sdkharness_valid_loopback_origin() {
  local value="$1" port
  case "$value" in http://127.0.0.1:[0-9]*|http://127.0.0.1:[0-9]*/) ;; *) return 1 ;; esac
  port="${value#http://127.0.0.1:}"
  port="${port%/}"
  case "$port" in ''|*[!0-9]*) return 1 ;; esac
  [ "$port" -ge 1 ] 2>/dev/null && [ "$port" -le 65535 ] 2>/dev/null
}
