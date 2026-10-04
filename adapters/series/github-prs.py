#!/usr/bin/env python3
"""Weekly merged and currently open PRs grouped into initiative labels."""
import argparse
import datetime as dt
import json
import os
import re
import subprocess
from common import push, timestamp

parser = argparse.ArgumentParser()
parser.add_argument("repo", help="owner/repo")
parser.add_argument("--series", default="github-prs")
parser.add_argument("--weeks", type=int, default=12)
parser.add_argument("--rules", help='JSON array of {initiative, labels?: [...], title_regex?: "..."}; first match wins')
args = parser.parse_args()
if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", args.repo) or not 1 <= args.weeks <= 12:
    raise SystemExit("repo must be owner/repo; weeks must be 1..12")
rules = []
if args.rules:
    with open(args.rules, encoding="utf-8") as source:
        rules = json.load(source)
now = dt.datetime.now(dt.timezone.utc)
current_week = (now - dt.timedelta(days=now.weekday())).replace(hour=0, minute=0, second=0, microsecond=0)
start = current_week-dt.timedelta(weeks=args.weeks-1)
counts = {}
initiatives = {rule["initiative"] for rule in rules} | {"other"}

def initiative(pr):
    labels = {label["name"] for label in pr["labels"]}
    for rule in rules:
        if labels.intersection(rule.get("labels", [])) or (rule.get("title_regex") and re.search(rule["title_regex"], pr["title"])):
            return rule["initiative"]
    for label in sorted(labels):
        if label.startswith("initiative:"):
            return label.removeprefix("initiative:")
    return "other"

# Updated order allows a safe cutoff: created_at and merged_at cannot be newer
# than updated_at. Fail at the bound rather than report a silently partial count.
for page in range(1, 101):
    raw = subprocess.check_output(["gh", "api", f"repos/{args.repo}/pulls?state=all&sort=updated&direction=desc&per_page=100&page={page}"])
    rows = json.loads(raw)
    for pr in rows:
        status, date = ("merged", pr["merged_at"]) if pr["merged_at"] else ("open", pr["created_at"])
        if status == "open" and pr["state"] != "open":
            continue
        when = dt.datetime.fromisoformat(date.replace("Z", "+00:00"))
        if when < start:
            continue
        group = initiative(pr)
        initiatives.add(group)
        week = (when - dt.timedelta(days=when.weekday())).replace(hour=0, minute=0, second=0, microsecond=0)
        key = (group, status, week)
        counts[key] = counts.get(key, 0) + 1
    if not rows or dt.datetime.fromisoformat(rows[-1]["updated_at"].replace("Z", "+00:00")) < start:
        break
else:
    raise SystemExit("GitHub result exceeds 100 pages; narrow the repository/window")
if len(initiatives)*2 > 100:
    raise SystemExit("initiative rules exceed the 100 label-set cap; consolidate groups")
for group in sorted(initiatives):
    for status in ("open", "merged"):
        for index in range(args.weeks):
            week = start+dt.timedelta(weeks=index)
            # Closed weeks have a fixed observation date for idempotent retries.
            # The current week's observation is now, keeping the live series fresh.
            observed = now if week == current_week else week+dt.timedelta(days=7, microseconds=-1000)
            push(args.series, counts.get((group, status, week), 0), {"initiative": group, "status": status}, observed.isoformat().replace("+00:00", "Z"))
