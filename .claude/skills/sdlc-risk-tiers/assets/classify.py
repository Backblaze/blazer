"""Risk classifier for a pull request. Standard library only.

Usage:
  git diff -z --name-only --no-renames origin/main...HEAD | python3 classify.py --config risk-tiers.json --author "$PR_AUTHOR"
  python3 classify.py --config risk-tiers.json --files a.py docs/x.md --author alice --lines 120

The change sets the tier, not the author. The highest tier of any changed file wins, and a
large diff moves the tier up one step. A second rule can require a human whatever the tier
says (sensitive paths, CI workflows). Authors outside `trusted_authors` need one more
approver than the tier says, up to two, and so does a missing --author: an unknown author is
never trusted. An agent's approval never counts as a human one,
and agent-authored changes require the person who prompted the agent.

Prints JSON: tier, humans_required, force_human, reasons. Exit code is always 0 unless the
input is invalid, so a workflow can publish the result as a check and decide separately.
"""
import argparse
import codecs
import json
import re
import sys
from pathlib import Path

ORDER = ["low", "medium", "high"]
HUMANS = {"low": 0, "medium": 1, "high": 2}


def glob_to_regex(pattern):
    """fnmatch-style glob where `**` crosses directories and `*` does not."""
    out, i = "", 0
    while i < len(pattern):
        if pattern[i:i + 3] == "**/":
            out += "(?:.*/)?"
            i += 3
        elif pattern[i:i + 2] == "**":
            out += ".*"
            i += 2
        elif pattern[i] == "*":
            out += "[^/]*"
            i += 1
        elif pattern[i] == "?":
            out += "[^/]"
            i += 1
        else:
            out += re.escape(pattern[i])
            i += 1
    return re.compile("^" + out + "$", re.IGNORECASE)   # case-insensitive: Auth/ and auth/ both count


def matches(path, patterns):
    return any(glob_to_regex(p).match(path) for p in patterns)


def bump(tier):
    return ORDER[min(ORDER.index(tier) + 1, len(ORDER) - 1)]


BOT = re.compile(r"(\[bot\]|-bot)$|^(claude|copilot|codex|openai-codex|gemini|github-actions)", re.IGNORECASE)   # AI assistant accounts


def classify(files, cfg, author=None, lines=0, agent_authored=False):
    if author is not None and BOT.search(author):
        agent_authored = True   # a bot or agent account authored it: a person must review
    reasons = []
    tier = None
    forced = []
    keywords = [k.lower() for k in cfg.get("always_human_keywords", [])]
    for path in files:
        segments = path.lower().split("/")
        if matches(path, cfg.get("always_human_paths", [])) or any(k in seg for k in keywords for seg in segments):
            forced.append(path)
        if matches(path, cfg.get("high_paths", [])):
            file_tier = "high"
        elif matches(path, cfg.get("medium_paths", [])):
            file_tier = "medium"
        elif matches(path, cfg.get("low_paths", [])):
            file_tier = "low"
        else:
            file_tier = cfg.get("default_tier", "medium")
        if tier is None or ORDER.index(file_tier) > ORDER.index(tier):
            tier = file_tier
            reasons = [f"{path} is {file_tier}"]
        elif file_tier == tier and len(reasons) < 5:
            reasons.append(f"{path} is {file_tier}")
    if tier is None:   # fail closed: an empty list usually means the diff command failed
        return {"tier": "low", "humans_required": 1, "force_human": True,
                "reasons": ["no changed files were read; a person must confirm the diff"], "agent_authored": agent_authored}

    threshold = cfg.get("large_diff_lines")
    if threshold and lines > threshold:
        tier = bump(tier)
        reasons.append(f"{lines} changed lines is over {threshold}, tier raised one step")

    humans = HUMANS[tier]
    force_human = bool(forced)
    if force_human:
        humans = max(humans, 1)
        reasons.append("human required for: " + ", ".join(forced[:5]))

    trusted = [t for t in cfg.get("trusted_authors", []) if not BOT.search(t)]   # bots are never trusted
    if not author:   # an unknown author never gets the trusted path
        humans = min(2, humans + 1)
        reasons.append("no author given, so not a trusted author: one more approver")
    elif author not in trusted:
        humans = min(2, humans + 1)
        reasons.append(f"{author} is not a trusted author, one more approver")

    result = {"tier": tier, "humans_required": humans, "force_human": force_human,
              "reasons": reasons, "agent_authored": agent_authored}
    if agent_authored:
        if result["humans_required"] < 1:
            result["humans_required"] = 1
            reasons.append("agent-authored, so the person who prompted it must review")
        result["note"] = ("the person who prompted the agent is a required reviewer; "
                          "agent approvals never count as human approvals")
    return result


def read_paths(data):
    """Paths from stdin: NUL-separated (git -z, preferred) or one per line, decoding git's C-quoted names."""
    if "\0" in data:
        return [p for p in data.split("\0") if p]
    paths = []
    for line in data.splitlines():
        if len(line) > 1 and line.startswith('"') and line.endswith('"'):
            line = codecs.escape_decode(line[1:-1].encode("utf-8"))[0].decode("utf-8", "surrogateescape")
        if line:
            paths.append(line)
    return paths


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--config", required=True, help="path to risk-tiers.json")
    ap.add_argument("--files", nargs="*", help="changed paths (default: read from stdin, NUL-separated or one per line)")
    ap.add_argument("--author", help="PR author login")
    ap.add_argument("--lines", type=int, default=0, help="added plus deleted lines")
    ap.add_argument("--agent-authored", action="store_true")
    args = ap.parse_args()
    try:
        cfg = json.loads(Path(args.config).read_text())
    except (OSError, ValueError) as err:
        sys.exit(f"cannot read config: {err}")
    files = args.files if args.files is not None else read_paths(sys.stdin.read())
    print(json.dumps(classify(files, cfg, args.author, args.lines, args.agent_authored)))


if __name__ == "__main__":
    main()
