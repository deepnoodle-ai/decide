#!/usr/bin/env python3
"""Score the call-paths spike on Gogs against the function sweep.

    python3 score.py [--carry] [--no-deps] [FUNCTION_RESULTS_DIR]

--carry scores the run built with -carry (work/carry-<kind>.jsonl).
--no-deps leaves out the sink calls that only the dependency rule finds.

Reads work/paths-<kind>.jsonl (the builder's items) and
work/results/paths-<kind>.jsonl (decide's answers). The function sweep is
the check-packs spike's v2 run, ../check-packs/work/results/v2 by default.

Path items are grouped by the function that holds the sink call, with the
best score of its paths, so they compare with function items. A label with
no group is placed by its score in the function sweep: its rank is one more
than the number of groups that scored higher.
"""
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ARGS = [a for a in sys.argv[1:] if not a.startswith("--")]
SET = "carry" if "--carry" in sys.argv else "paths"
NO_DEPS = "--no-deps" in sys.argv
FN = ARGS[0] if ARGS else os.path.join(HERE, "../check-packs/work/results/v2")

KINDS = {
    "ssrf": ("ssrf", "ssrf", ["deliver", "getResponse", "MigrateRepository", "runSync"]),
    # Merge is #8301. #8390 and #8393 were fixed in git-module, which
    # CompareAndPullRequestPost (#8390, DiffBinary) and the API handlers
    # (#8393: LsTree, CreateArchive) call with a request's refs.
    "command": ("command-injection", "injection",
                ["Merge", "CompareAndPullRequestPost", "getRepoGitTree", "getArchive", "getContents"]),
    "path": ("path-traversal", "traversal", ["UploadRepoFiles", "UserPath", "RepoPath", "isRepositoryGitPath"]),
    "sql": ("sql-injection", "injection", ["GetByCollaboratorID", "searchUserByName"]),
}
# The SQL labels are false positives: every caller passes orderBy as a
# constant.


def rows(path):
    with open(path) as f:
        return [json.loads(line) for line in f if line.strip()]


def short(name):
    return name.split(".")[-1]


for kind, (tmpl, q, labels) in KINDS.items():
    items = rows(os.path.join(HERE, f"work/{SET}-{kind}.jsonl"))
    answers = rows(os.path.join(HERE, f"work/results/{SET}-{kind}.jsonl"))
    fn = [r for r in rows(os.path.join(FN, f"gogs-{tmpl}.jsonl")) if r["status"] == "complete"]
    fn_p = {}
    for r in fn:
        fn_p.setdefault(short(r["input"]), r["answers"][q]["noul"])
    fn_ranked = sorted(fn, key=lambda r: -r["answers"][q]["noul"])

    groups, paths_only = {}, {}
    for a in answers:
        if a["status"] != "complete":
            continue
        it = items[a["index"]]
        if NO_DEPS and it["rule"] == "dependency":
            continue
        p = a["answers"][q]["noul"]
        holder = it["path"][-1]
        if p > groups.get(holder, (-1,))[0]:
            groups[holder] = (p, it)
        if not it.get("orphan") and p > paths_only.get(holder, -1):
            paths_only[holder] = p
    ranked = sorted(groups.items(), key=lambda x: -x[1][0])
    kept = [it for it in items if not NO_DEPS or it["rule"] != "dependency"]
    n_paths = sum(1 for it in kept if not it.get("orphan"))
    print(f"\n=== {kind}: {len(fn)} functions; {n_paths} paths and {len(kept) - n_paths} orphans "
          f"in {len(groups)} sink-call functions")
    print(f"  flagged at 0.5: functions {sum(r['answers'][q]['noul'] >= .5 for r in fn)}, "
          f"path groups {sum(p >= .5 for p in paths_only.values())}, "
          f"paths plus orphans {sum(p >= .5 for p, _ in groups.values())}")
    for label in labels:
        frank = next((i + 1 for i, r in enumerate(fn_ranked) if short(r["input"]) == label), None)
        fp = fn_p.get(label)
        # A label matches any function on a path, so a handler that is the
        # entry point counts too. Its rank is one more than the groups that
        # scored higher.
        on = [(a["answers"][q]["noul"], items[a["index"]]) for a in answers if a["status"] == "complete"
              and (not NO_DEPS or items[a["index"]]["rule"] != "dependency")
              and label in map(short, items[a["index"]]["path"])]
        if on:
            p, it = max(on, key=lambda x: x[0])
            i = 1 + sum(1 for g, _ in groups.values() if g > p)
            how = "orphan" if it.get("orphan") else " -> ".join(it["path"])
            print(f"  {label:20} function rank {frank} (p={fp:.2f}); path rank {i}/{len(ranked)} p={p:.2f} via {it['rule']}: {how}")
        else:
            if kind == "sql":
                print(f"  {label:20} function rank {frank} (p={fp:.2f}); false positive, and no sink call, so not judged")
                continue
            merged = 1 + sum(1 for p, _ in groups.values() if fp is not None and p > fp)
            print(f"  {label:20} function rank {frank} (p={fp:.2f}); no sink call, placed at {merged}/{len(ranked) + 1} by function score")
    for i, (holder, (p, it)) in enumerate(ranked[:10]):
        how = "orphan" if it.get("orphan") else " -> ".join(it["path"])
        print(f"  {i + 1:3} {p:.2f} [{it['rule']}] {how}  ({it['sink']})")
