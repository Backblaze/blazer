"""Baseline how much human review time pull requests take. Needs the GitHub CLI (`gh`) logged in.

Usage:
  python3 review_metrics.py --repo owner/name --limit 200
  python3 review_metrics.py --repo owner/name --limit 200 --since-days 90 --json

Reports, over merged PRs: human approvals per PR (median and p95), time to first human review
(median and p95), time open before merge (median and p95), human reviews with a written body per PR, and the
share of merged PRs that are reverts. Revert and incident rates are the guardrail: if they rise,
human time was moved, not saved. Reviews from bots, from the PR's own author and those submitted
after the merge do not count as human approvals, and a reviewer counts as approving only if their latest approve or
changes-requested review is an approval (a later dismissal also removes it). Read-only.

Bots are recognized by GitHub's own account type (GraphQL reports Copilot as
`copilot-pull-request-reviewer`, with no `[bot]` suffix), with a name check as a fallback.
Every PR's reviews are fetched in full (paginated). The result says `truncated` whenever --limit
stopped the fetch while GitHub still had more merged PRs, since the window is by merge time and
the list is by creation time.
"""
import argparse
import json
import statistics
import subprocess
import sys
from datetime import datetime, timedelta, timezone


def ts(value):
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


def percentile(values, pct):
    if not values:
        return None
    values = sorted(values)
    k = max(0, min(len(values) - 1, round(pct / 100 * (len(values) - 1))))
    return values[k]


BOT_NAMES = ("copilot", "github-actions", "dependabot", "renovate", "claude", "github-code-quality")


def is_bot(author):
    """author is GitHub's author object ({"login", "type"}) or a login; None is a deleted account."""
    if isinstance(author, dict):
        if author.get("type") == "Bot" or author.get("__typename") == "Bot":
            return True
        author = author.get("login")
    if author is None:
        return True
    login = author.lower()
    return login.endswith("[bot]") or login.endswith("-bot") or any(login.startswith(n) for n in BOT_NAMES)


REVIEW_FIELDS = "pageInfo { hasNextPage endCursor } nodes { author { login __typename } state submittedAt body }"
QUERY = """query($owner: String!, $name: String!, $n: Int!, $endCursor: String) {
  repository(owner: $owner, name: $name) {
    pullRequests(states: MERGED, first: $n, after: $endCursor, orderBy: {field: CREATED_AT, direction: DESC}) {
      pageInfo { hasNextPage endCursor }
      nodes { number title createdAt mergedAt author { login __typename } reviews(first: 100) { %s } }
    }
  }
}""" % REVIEW_FIELDS
REVIEWS_QUERY = """query($owner: String!, $name: String!, $number: Int!, $endCursor: String) {
  repository(owner: $owner, name: $name) { pullRequest(number: $number) { reviews(first: 100, after: $endCursor) { %s } } }
}""" % REVIEW_FIELDS


def graphql(query, strings, ints):
    cmd = ["gh", "api", "graphql", "-f", "query=" + query]
    cmd += [a for k, v in strings.items() if v is not None for a in ("-f", f"{k}={v}")]
    cmd += [a for k, v in ints.items() for a in ("-F", f"{k}={v}")]
    out = subprocess.run(cmd, capture_output=True, text=True)
    if out.returncode != 0:
        sys.exit(out.stderr.strip() or "gh failed")
    return json.loads(out.stdout)["data"]["repository"]


def fetch(repo, limit):
    """Returns (prs, complete): complete is True only when GitHub reported no more merged PRs."""
    owner, name = repo.split("/", 1)
    prs, cursor, complete = [], None, False
    while len(prs) < limit:
        page = graphql(QUERY, {"owner": owner, "name": name, "endCursor": cursor}, {"n": min(50, limit - len(prs))})["pullRequests"]
        for pr in page["nodes"]:
            reviews = pr["reviews"]
            pr["reviews"] = list(reviews["nodes"])
            while reviews["pageInfo"]["hasNextPage"]:   # a superseding review can be past the first 100
                reviews = graphql(REVIEWS_QUERY, {"owner": owner, "name": name, "endCursor": reviews["pageInfo"]["endCursor"]},
                                  {"number": pr["number"]})["pullRequest"]["reviews"]
                pr["reviews"] += reviews["nodes"]
            prs.append(pr)
        if not page["pageInfo"]["hasNextPage"]:
            complete = True
            break
        cursor = page["pageInfo"]["endCursor"]
    return prs, complete


def summarize(prs, since_days=None, now=None, truncated=False):
    now = now or datetime.now(timezone.utc)
    cutoff = now - timedelta(days=since_days) if since_days else None
    approvals, first_review_h, open_h, comments, reverts, counted = [], [], [], [], 0, 0
    for pr in prs:
        merged = pr.get("mergedAt")
        if not merged:
            continue
        if cutoff and ts(merged) < cutoff:
            continue
        counted += 1
        author = (pr.get("author") or {}).get("login")
        # Only reviews submitted by the merge count: a later approval did not gate this merge.
        human = [r for r in pr.get("reviews", [])
                 if not is_bot(r.get("author"))
                 and (r.get("author") or {}).get("login") != author
                 and r.get("submittedAt") and ts(r["submittedAt"]) <= ts(merged)]
        latest = {}   # each reviewer's last deciding review, as GitHub counts it
        for r in sorted(human, key=lambda r: r.get("submittedAt") or ""):
            if r.get("state") in ("APPROVED", "CHANGES_REQUESTED", "DISMISSED"):
                latest[r["author"]["login"]] = r["state"]
        approvers = {login for login, state in latest.items() if state == "APPROVED"}
        approvals.append(len(approvers))
        comments.append(sum(1 for r in human if (r.get("body") or "").strip()))
        created = ts(pr["createdAt"])
        open_h.append((ts(merged) - created).total_seconds() / 3600)
        times = sorted(ts(r["submittedAt"]) for r in human if r.get("submittedAt"))
        if times:
            first_review_h.append((times[0] - created).total_seconds() / 3600)
        if pr.get("title", "").lower().startswith("revert"):
            reverts += 1

    def stat(values):
        if not values:
            return {"median": None, "p95": None}
        return {"median": round(statistics.median(values), 2), "p95": round(percentile(values, 95), 2)}

    return {
        "merged_prs": counted,
        "truncated": bool(truncated),
        "human_approvals_per_pr": stat(approvals),
        "prs_merged_with_no_human_approval": (round(sum(1 for a in approvals if a == 0) / counted, 3) if counted else None),
        "hours_to_first_human_review": stat(first_review_h),
        "hours_open_before_merge": stat(open_h),
        "human_reviews_with_body_per_pr": stat(comments),
        "revert_share": round(reverts / counted, 3) if counted else None,
    }


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--repo", required=True)
    ap.add_argument("--limit", type=int, default=200)
    ap.add_argument("--since-days", type=int)
    ap.add_argument("--json", action="store_true")
    args = ap.parse_args()
    prs, complete = fetch(args.repo, args.limit)
    result = summarize(prs, args.since_days, truncated=not complete)
    if result["truncated"]:
        print(f"warning: stopped at --limit {args.limit} with more merged PRs on GitHub; raise it to cover the window", file=sys.stderr)
    if args.json:
        print(json.dumps(result, indent=2))
        return
    for key, value in result.items():
        print(f"{key}: {value}")


if __name__ == "__main__":
    main()
