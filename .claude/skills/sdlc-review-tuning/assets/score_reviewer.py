"""Score an AI reviewer against an incident-derived eval set, and summarize comment noise.
Standard library only.

Commands:
  score  --cases evals/review --results out/review-results [--tolerance 3] [--json]
  noise  --outcomes outcomes.json [--json]

Layout for `score`:
  evals/review/<case>/case.json     {"name": "...", "clean": false,
                                     "expected": [{"file": "src/a.py", "line": 41, "severity": "Important"}]}
  out/review-results/<case>.json    {"findings": [{"file": "src/a.py", "line": 43,
                                                   "severity": "Important", "summary": "..."}]}

A case is **recalled** if every expected finding has its own reported finding in the same file
within `--tolerance` lines (and, when a severity is expected, reports a severity at least that high); one reported
finding cannot cover two expected ones. A **clean** case (a known-good PR) counts as a false
positive if the reviewer reports any finding that is not a Nit or Pre-existing, whatever it calls
the severity ("important", "High", none). A missing result file counts as a miss, so a crashed run
cannot pass.

`noise` reads a JSON list of {"outcome": "fixed" | "wrong" | "intentional" | "not_mattering"}
and reports the share of each class.

Exit status of `score` is 1 when recall is below `--min-recall` or any clean case produced a
false positive, so it can gate a change to the reviewer.
"""
import argparse
import json
import os
import sys
from collections import Counter

MINOR = {"nit", "pre-existing", "preexisting"}


def rank(severity):   # 0 for Nit and Pre-existing, 1 for anything else, so a renamed severity still counts
    return 0 if str(severity or "").strip().lower() in MINOR else 1


def load(path):
    with open(path) as f:
        return json.load(f)


def covers(f, expected, tolerance):
    if f.get("file") != expected["file"]:
        return False
    try:
        line = int(f.get("line"))
    except (TypeError, ValueError):
        return False
    if abs(line - int(expected["line"])) > tolerance:
        return False
    want = expected.get("severity")
    if want and (not f.get("severity") or rank(f.get("severity")) < rank(want)):
        return False   # an expected severity needs a reported one at least as high; none is not enough
    return True


def matching(expected, findings, tolerance):
    """Largest one-to-one pairing of expected findings to reported ones; returns {expected index: finding index}."""
    pair = {}

    def assign(e, seen):
        for i, f in enumerate(findings):
            if i in seen or not covers(f, expected[e], tolerance):
                continue
            seen.add(i)
            owner = next((k for k, v in pair.items() if v == i), None)
            if owner is None or assign(owner, seen):
                pair[e] = i
                return True
        return False

    for e in range(len(expected)):
        assign(e, set())
    return pair


def score(cases_dir, results_dir, tolerance):
    rows = []
    for name in sorted(os.listdir(cases_dir)):
        case_path = os.path.join(cases_dir, name, "case.json")
        if not os.path.isfile(case_path):
            continue
        case = load(case_path)
        result_path = os.path.join(results_dir, name + ".json")
        data = load(result_path) if os.path.isfile(result_path) else None
        findings = data.get("findings") if isinstance(data, dict) else None
        if not isinstance(findings, list):   # absent, malformed or missing "findings" counts as no result
            findings = None
        if findings is None:
            rows.append({"case": name, "clean": bool(case.get("clean")), "missing_result": True,
                         "recalled": False if not case.get("clean") else None, "false_positive": False,
                         "extra_findings": 0})
            continue
        if case.get("clean"):
            fp = any(rank(f.get("severity")) for f in findings)
            rows.append({"case": name, "clean": True, "recalled": None, "false_positive": fp,
                         "extra_findings": len(findings)})
        else:
            expected = case.get("expected", [])
            pair = matching(expected, findings, tolerance)
            hit = bool(expected) and len(pair) == len(expected)
            matched = len(pair)
            rows.append({"case": name, "clean": False, "recalled": hit, "false_positive": False,
                         "extra_findings": len(findings) - matched})
    bug_cases = [r for r in rows if not r["clean"]]
    clean_cases = [r for r in rows if r["clean"]]
    recall = (sum(1 for r in bug_cases if r["recalled"]) / len(bug_cases)) if bug_cases else None
    fp_rate = (sum(1 for r in clean_cases if r["false_positive"]) / len(clean_cases)) if clean_cases else None
    # Unrounded, so the --min-recall gate compares the real value (0.8996 must not pass 0.9).
    return {"cases": rows, "bug_cases": len(bug_cases), "clean_cases": len(clean_cases),
            "recall": recall, "false_positive_rate_on_clean": fp_rate}


def noise(outcomes):
    counts = Counter(o.get("outcome", "unknown") for o in outcomes)
    total = sum(counts.values())
    shares = {k: round(v / total, 3) for k, v in sorted(counts.items())} if total else {}
    return {"comments": total, "counts": dict(counts), "share": shares}


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)
    s = sub.add_parser("score")
    s.add_argument("--cases", required=True)
    s.add_argument("--results", required=True)
    s.add_argument("--tolerance", type=int, default=3)
    s.add_argument("--min-recall", type=float, default=0.0)
    s.add_argument("--json", action="store_true")
    n = sub.add_parser("noise")
    n.add_argument("--outcomes", required=True)
    n.add_argument("--json", action="store_true")
    args = ap.parse_args()

    if args.cmd == "noise":
        out = noise(load(args.outcomes))
        print(json.dumps(out, indent=2) if args.json else
              f"{out['comments']} comments: " + ", ".join(f"{k} {v:.0%}" for k, v in out["share"].items()))
        return

    out = score(args.cases, args.results, args.tolerance)
    shown = {k: (None if out[k] is None else round(out[k], 3)) for k in ("recall", "false_positive_rate_on_clean")}
    if args.json:
        print(json.dumps(dict(out, **shown), indent=2))
    else:
        for r in out["cases"]:
            if r.get("missing_result"):
                status = "MISSING RESULT"
            elif r["clean"]:
                status = "FALSE POSITIVE" if r["false_positive"] else "clean ok"
            else:
                status = "recalled" if r["recalled"] else "MISSED"
            print(f"{status:15} {r['case']} (extra findings: {r['extra_findings']})")
        print(f"recall: {shown['recall']}  false positives on clean: {shown['false_positive_rate_on_clean']}")
    if not out["cases"]:
        print("no cases found: nothing was tested")
    elif out["recall"] is None and args.min_recall > 0:
        print("no bug cases: recall cannot be measured")
    failed = not out["cases"] or (args.min_recall > 0 and (out["recall"] is None or out["recall"] < args.min_recall)) or \
             any(r["false_positive"] or r.get("missing_result") for r in out["cases"])
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
