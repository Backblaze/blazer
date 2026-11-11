---
name: sdlc-review-tuning
description: Measure and tune an AI code reviewer. Build an incident-derived eval set and score every reviewer change, classify noise from authors' responses, test on known-clean PRs, and stop agent author-reviewer loops with round caps and diff-only re-review. Use when the reviewer is noisy, misses bugs, loops over many rounds, or when changing its prompts, tools, model or REVIEW.md.
---

# Tuning the AI reviewer

Treat the reviewer as a system with a score, not a prompt you edit by feel. The priority order: catch real bugs first, reduce review rounds second. Noise is more expensive with agent authors, because every nit triggers another commit and another round. This follows the Claude Code team's account (see `references/code-review-at-anthropic.md` in the playbook folder).

## 1. An eval set from incidents

1. Collect past PRs that caused incidents, with their fixes and reverts.
2. Have engineers who know each area confirm every bug and where it lives. A case with an unverified label poisons the score.
3. Store each case so the reviewer can be run on the original diff (`evals/review/<name>/` with the diff or the `from`/`to` commits and the expected finding location).
4. Run the reviewer against the full set daily, and before merging any change to its prompts, tools, model or `REVIEW.md`.
5. Score recall (the known bug was reported, at the right place and with the right severity). A change that does not improve the score is dropped.
6. Every new incident adds a case. The harness for running evals is in `sdlc-evals`.

## 2. Measure noise

- Classify every reviewer comment by the author's response: fixed, marked wrong, marked intentional, or marked as not mattering. Pull this from PR thread resolutions, with the resolving reason where present.
- Track the share in each class per reviewer version and per repo or area.
- Run the reviewer on a set of known-clean PRs and count how often it reports something that would lead to a harmful change. That is the false-positive rate that matters.
- Use the numbers to edit `REVIEW.md`: cap nits, add skip paths, tighten what Important means.

## 3. Verify findings before they post

Run several hunting agents in parallel, then have a separate group of verifier agents vote on each finding. Post a finding only if enough verifiers agree it is real. Drop style-only, diagnostic and cleanup findings unless the owners asked for them. A/B test each filter on real PRs before adopting it. Tag survivors Important, Nit or Pre-existing so a bug the PR did not introduce is not blamed on its author.

## 4. Stop loops

Agent authors and agent reviewers can iterate indefinitely: one PR in the source article went through 50+ rounds overnight, and about 59% of what the reviewer kept flagging was code present since its first pass. Apply:

- **Round cap.** After three rounds, the reviewer asks the author to reconsider the approach instead of posting more line fixes.
- **Diff-only re-review** after the first few passes.
- **Flag the bug class**, so the author fixes every instance in one commit.
- **Never repost** a finding already raised or resolved.
- **Nightly review of existing code** to find old bugs early, instead of letting them surface as late findings on unrelated PRs.
- **Depth by risk.** Multiple rounds are worth it for risky changes and wasteful for routine ones (`sdlc-risk-tiers`).

Track mean reviews to pass, the share passing after one round, and the share needing five or more. Those moved from 2.08 to 1.32, from 65% to 78%, and from 8.9% to 1.5% for the Claude Code team after these changes. Treat those as an example of the scale of effect, not a target.

## Assets

- `assets/score_reviewer.py`: scores the reviewer against the eval set and summarizes noise.
  ```bash
  python3 score_reviewer.py score --cases evals/review --results out/review-results --min-recall 0.8
  python3 score_reviewer.py noise --outcomes outcomes.json
  ```
  Case layout: `evals/review/<case>/case.json` with `expected` findings (`file`, `line`, optional `severity`) or `"clean": true`; reviewer output in `<case>.json` with `findings`. A case is recalled when every expected finding has its own reported finding within `--tolerance` lines; one finding cannot cover two bugs. A clean case fails if the reviewer reports anything other than a Nit or Pre-existing finding, whatever severity name it uses. A missing result file counts as a miss, so a crashed run cannot pass. `score` exits 1 below `--min-recall` or on any false positive on a clean case, so it can gate a change to the reviewer's prompts, tools or model.
- `assets/nightly-review.yml`: a scheduled workflow that reviews one area of the repo each night with a read-only agent (Claude Code's `Read`, `Grep`, `Glob`, `dontAsk` as the worked example; for another assistant swap in its headless command from `references/ai-assistants.md` with the same restrictions) and files at most one issue from the output in a separate step, so the agent never has write access. It reads the run through `sdlc-evals/assets/normalize_result.py` (copy it to `.github/scripts/`), so a failed or unfinished run is never filed as a clean area, and it works for Codex output too. Rotates through top-level folders by day of year, or takes an `area` input. Adjust the prompt to your `REVIEW.md` and instruction file, and check the issue label exists. This is also the scheduled drift scan that harness-style repos use for garbage collection (`sdlc-harness-translate`).

## 5. Check the reviewer follows its instructions

Do not assume `REVIEW.md` is obeyed. Add eval cases that depend on a rule (a nit cap, a skip path, the count-first summary, file-and-line evidence) and check the output against it.

## Measure

- Leading: recall on the incident set; share of comments fixed vs marked wrong or not mattering; false-positive rate on clean PRs.
- Lagging: mean reviews to pass; reverts and incidents on changes the reviewer cleared; escapes that match an eval case.
