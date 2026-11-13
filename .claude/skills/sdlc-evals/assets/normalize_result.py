#!/usr/bin/env python3
"""Turn one assistant's headless output into {"ok", "final", "why"} before anything grades it. Standard library only.

  normalize_result.py --assistant claude|codex --exit-code "$rc" raw-output-file > result.json

ok is decided from the runtime's own signals, never from what the model wrote:

  claude  `claude -p --output-format json`: one JSON object; ok when the exit code is 0, is_error is false
          and subtype is success. final is .result.
  codex   `codex exec --json`: JSONL events; ok when the exit code is 0, every line parses, a turn.completed
          event arrives and no turn.failed or error event does. final is the text of the last
          item.completed event whose item.type is agent_message.

A run with no final message is not ok either: there is nothing to grade. Copilot and Gemini CLI are not
supported yet: add a parser for their JSON output here (references/ai-assistants.md) instead of guessing.
Exit status: 0 when the input was understood (ok may still be false), 2 for an unsupported assistant.
"""
import argparse
import json
import sys


def claude(raw):
    try:
        out = json.loads(raw)
    except ValueError:
        return False, "", "output is not JSON"
    if not isinstance(out, dict):
        return False, "", "output is not a JSON object"
    final = out.get("result") if isinstance(out.get("result"), str) else ""
    if out.get("is_error") is not False:
        return False, final, "the run reported an error"
    if out.get("subtype", "success") != "success":
        return False, final, "the run stopped early: " + str(out.get("subtype"))
    return True, final, ""


def codex(raw):
    final, completed, failed = "", False, ""
    for n, line in enumerate(raw.splitlines(), 1):
        if not line.strip():
            continue
        try:
            event = json.loads(line)
        except ValueError:
            return False, final, f"line {n} is not JSON (truncated or mixed output)"
        kind = event.get("type") if isinstance(event, dict) else None
        if kind == "turn.completed":
            completed = True
        elif kind in ("turn.failed", "error"):
            failed = failed or kind
        elif kind == "item.completed":
            item = event.get("item") or {}
            if item.get("type") == "agent_message" and isinstance(item.get("text"), str):
                final = item["text"]
    if failed:
        return False, final, "the run reported " + failed
    if not completed:
        return False, final, "no turn.completed event: the run did not finish"
    return True, final, ""


PARSERS = {"claude": claude, "codex": codex}


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--assistant", required=True)
    ap.add_argument("--exit-code", type=int, required=True, help="the headless command's exit status")
    ap.add_argument("raw", help="file holding the command's stdout")
    args = ap.parse_args()
    if args.assistant not in PARSERS:
        print(f"no parser for '{args.assistant}' yet; supported: {', '.join(PARSERS)}", file=sys.stderr)
        return 2
    with open(args.raw, encoding="utf-8", errors="replace") as f:
        ok, final, why = PARSERS[args.assistant](f.read())
    if ok and args.exit_code != 0:
        ok, why = False, f"exit code {args.exit_code}"
    if ok and not final.strip():
        ok, why = False, "no final message to grade"
    print(json.dumps({"ok": ok, "final": final, "why": why}))
    return 0


if __name__ == "__main__":
    sys.exit(main())
