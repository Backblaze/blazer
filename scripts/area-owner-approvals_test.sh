#!/usr/bin/env bash
# Offline tests for the review-state rules in scripts/area-owner-approvals.py (used by
# .github/workflows/require-area-approvals.yml). No network and no pathspec: the tests call the
# review and evaluation functions directly with hand-made review lists.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
python3 - "$root/scripts/area-owner-approvals.py" <<'PY'
import importlib.util
import sys

spec = importlib.util.spec_from_file_location("area_owner_approvals", sys.argv[1])
gate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(gate)

HEAD, OLD = "a" * 40, "b" * 40
AREA = {"/.github/": {"alice", "bob"}}
WRITERS = {"alice", "bob"}


def review(user, state, commit, at):
    r = {"user": {"login": user}, "state": state, "submitted_at": at}
    if commit is not None:
        r["commit_id"] = commit
    return r


def check(name, reviews, head, want_state, want_desc):
    states = gate.review_states(reviews, head)
    state, desc = gate.evaluate(AREA, WRITERS, "author", states)
    if (state, desc) != (want_state, want_desc):
        print(f"FAIL {name}: got ({state!r}, {desc!r}), want ({want_state!r}, {want_desc!r})")
        sys.exit(1)
    print(f"ok   {name}")


OK = ("success", "All required area owners have approved.")
AWAITING = ("failure", "/.github/: awaiting alice,bob")

check("approval on the head commit counts",
      [review("alice", "APPROVED", HEAD, "2026-11-01T00:00:00Z")], HEAD, *OK)
check("approval on an older commit does not count",
      [review("alice", "APPROVED", OLD, "2026-11-01T00:00:00Z")], HEAD, *AWAITING)
check("approval with no commit_id does not count",
      [review("alice", "APPROVED", None, "2026-11-01T00:00:00Z")], HEAD, *AWAITING)
check("dismissed approval does not count",
      [review("alice", "DISMISSED", HEAD, "2026-11-01T00:00:00Z")], HEAD, *AWAITING)
check("later change request overrides an approval on the head commit",
      [review("alice", "APPROVED", HEAD, "2026-11-01T00:00:00Z"),
       review("alice", "CHANGES_REQUESTED", HEAD, "2026-11-02T00:00:00Z")],
      HEAD, "failure", "/.github/: changes requested by alice")
check("change request on an older commit still blocks",
      [review("alice", "CHANGES_REQUESTED", OLD, "2026-11-01T00:00:00Z"),
       review("bob", "APPROVED", HEAD, "2026-11-02T00:00:00Z")],
      HEAD, "failure", "/.github/: changes requested by alice")
moved = [review("alice", "APPROVED", OLD, "2026-11-01T00:00:00Z")]
check("approval goes stale when the head moves away from its commit", moved, HEAD, *AWAITING)
check("approval counts again when the head returns to its commit", moved, OLD, *OK)
PY
