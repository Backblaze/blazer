"""Control-band detector.

Reads hourly readings (oldest first) from stdin as a JSON list and prints the worst tier the
newest reading falls in, as JSON. Only the high side matters (error rates, failure rates).
The baseline is every reading before the newest one, so feed it ~30 days of readings.
"""
import json
import statistics
import sys

MIN_SIGMA = 0.01   # in the metric's own units; stops a flat baseline turning every blip into a breach
MIN_BASELINE = 24  # readings needed before the newest one


def count_above(readings, line, last):
    return sum(r > line for r in readings[-last:])


def tier(readings):
    baseline = readings[:-1]
    if len(baseline) < MIN_BASELINE:
        raise ValueError(f"need at least {MIN_BASELINE} readings before the newest one")
    mean = statistics.mean(baseline)
    sigma = max(statistics.stdev(baseline), MIN_SIGMA)
    rules = [  # worst first
        ("3sigma", "reading above 3 sigma", readings[-1] > mean + 3 * sigma),
        ("2sigma", "reading above 2 sigma", readings[-1] > mean + 2 * sigma),
        ("2sigma", "drift: 4 of 5 readings above 1 sigma", count_above(readings, mean + sigma, 5) >= 4),
        ("2sigma", "drift: 8 readings in a row above the mean", count_above(readings, mean, 8) >= 8),
        ("1sigma", "reading above 1 sigma", readings[-1] > mean + sigma),
    ]
    stats = {"reading": readings[-1], "mean": round(mean, 4), "sigma": round(sigma, 4)}
    for name, rule, tripped in rules:
        if tripped:
            return {"tier": name, "rule": rule, **stats}
    return {"tier": "none", **stats}


if __name__ == "__main__":
    try:
        print(json.dumps(tier(json.load(sys.stdin))))
    except ValueError as err:
        sys.exit(str(err))
