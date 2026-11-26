#!/usr/bin/env python3
"""Compute the area-owner-approvals status for .github/workflows/require-area-approvals.yml.

Reads AUTHOR, HEAD_SHA, WRITERS_FILE, CODEOWNERS_FILE and RUNNER_TEMP (files.txt and reviews.json in it)
from the environment, and writes the status state and description, one per line, to RUNNER_TEMP/gate.out.
The rules are described in the workflow's header comment. scripts/area-owner-approvals_test.sh tests them.
"""
import json
import os


def parse_codeowners(lines):
    """Return CODEOWNERS as ordered (pattern, [owners_lc]) rules."""
    rules = []
    for line in lines:
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        parts = line.split()
        rules.append((parts[0], [o.lstrip("@").lower() for o in parts[1:]]))
    return rules


def touched_areas(rules, paths, matches):
    """Map each matched CODEOWNERS pattern to the owners of the changed paths it covers (last match wins)."""
    areas = {}  # pattern -> set(owners_lc)
    for path in paths:
        path = path.strip()
        if not path:
            continue
        chosen = None
        for pattern, owners in rules:
            if matches(pattern, path):
                chosen = (pattern, owners)
        if not chosen:
            continue
        pattern, owners = chosen
        if owners:
            areas.setdefault(pattern, set()).update(owners)
    return areas


def review_states(reviews, head_sha):
    """Return each user's latest review state. An APPROVED review on any commit but head_sha is stale."""
    state_by_user = {}
    for r in sorted(reviews, key=lambda r: r.get("submitted_at") or ""):
        login = ((r.get("user") or {}).get("login") or "").lower()
        st = r.get("state")
        if st in ("APPROVED", "CHANGES_REQUESTED", "DISMISSED"):
            if st == "APPROVED" and r.get("commit_id") != head_sha:
                st = "STALE_APPROVED"  # not the head commit: does not count
            state_by_user[login] = st
    return state_by_user


def evaluate(areas, writers, author, state_by_user):
    """Return (state, description) for the commit status."""
    blocked = []  # (pattern, "changes"|"awaiting"|"nowriters", [users])
    for pattern, owners in areas.items():
        o = set(owners) & writers
        if not o:
            blocked.append((pattern, "nowriters", sorted(owners)))
            continue
        o.discard(author)  # author implicitly approves their own area
        if not o:
            continue
        changes = sorted(u for u in o if state_by_user.get(u) == "CHANGES_REQUESTED")
        approved = [u for u in o if state_by_user.get(u) == "APPROVED"]
        if changes:
            blocked.append((pattern, "changes", changes))
        elif approved:
            continue
        else:
            blocked.append((pattern, "awaiting", sorted(o)))

    if not areas:
        # Fail-safe: with the `*` catch-all this is near-unreachable, but if no rule
        # matched we go red rather than waving the PR through (defense-in-depth).
        return "failure", "No CODEOWNERS rule matched the changed files, add a rule in .github/CODEOWNERS."
    if not blocked:
        return "success", "All required area owners have approved."
    parts = []
    for pattern, kind, users in blocked:
        if kind == "changes":
            parts.append(f"{pattern}: changes requested by {','.join(users)}")
        elif kind == "nowriters":
            parts.append(f"{pattern}: no owner with write access ({','.join(users)})")
        else:
            parts.append(f"{pattern}: awaiting {','.join(users)}")
    return "failure", "; ".join(parts)


def main():
    # GitHub's CODEOWNERS engine is gitignore-style; defer to pathspec's
    # gitwildmatch so `*`, `genblaze-*`, anchored dirs, etc. all match correctly.
    # Imported here so the tests can load this file without pathspec installed.
    from pathspec.patterns import GitWildMatchPattern

    def matches(pattern, path):
        return GitWildMatchPattern(pattern).match_file(path)

    tmp = os.environ["RUNNER_TEMP"]
    author = os.environ.get("AUTHOR", "").lower()
    head_sha = os.environ["HEAD_SHA"]
    with open(os.environ["WRITERS_FILE"]) as f:
        writers = {line.strip().lower() for line in f if line.strip()}
    with open(os.environ["CODEOWNERS_FILE"]) as f:
        rules = parse_codeowners(f)
    with open(os.path.join(tmp, "files.txt")) as f:
        areas = touched_areas(rules, f, matches)
    with open(os.path.join(tmp, "reviews.json")) as f:
        reviews = json.load(f)

    state, desc = evaluate(areas, writers, author, review_states(reviews, head_sha))
    with open(os.path.join(tmp, "gate.out"), "w") as f:
        f.write(state + "\n")
        f.write(desc + "\n")


if __name__ == "__main__":
    main()
