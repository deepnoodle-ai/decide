#!/usr/bin/env python3
"""Score the check-packs spike from the results run.sh writes to work/results/.

    python3 score.py bench [v1|v2|ctx]    OWASP Benchmark, by category
    python3 score.py gogs [v1|v2]         Gogs, labeled functions by rank
    python3 score.py paths                Gogs call paths, by sink function
    python3 score.py dive                 dive, top candidates per Go template

The Benchmark's labels come from work/BenchmarkJava/expectedresults-1.2.csv,
which run.sh clones.
"""
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))


def rows(path):
    with open(os.path.join(HERE, "work", path)) as f:
        return [json.loads(line) for line in f]


def bench(run):
    truth = {}
    with open(os.path.join(HERE, "work/BenchmarkJava/expectedresults-1.2.csv")) as f:
        for line in f:
            if not line.startswith("#"):
                name, _, real, _ = line.strip().split(",")
                truth[name] = real == "true"
    questions = {"sqli": "injection", "cmdi": "injection", "pathtraver": "traversal",
                 "xss": "xss", "crypto": "cipher", "hash": "hash", "weakrand": "random"}
    print(f"{'category':10} {'n':>4} {'TPR@.5':>7} {'FPR@.5':>7} {'score@.5':>8} {'best thr':>8} {'best':>5} {'AUC':>5}")
    for cat, q in questions.items():
        try:
            rs = rows(f"results/{run}/bench-{cat}.jsonl")
        except FileNotFoundError:
            continue
        xs = [(r["answers"][q]["noul"], truth[os.path.basename(r["source"])[:-5]]) for r in rs]
        pos = [p for p, y in xs if y]
        neg = [p for p, y in xs if not y]

        def at(t):
            return sum(p >= t for p in pos) / len(pos), sum(p >= t for p in neg) / len(neg)

        tpr, fpr = at(0.5)
        best = max((at(t / 100)[0] - at(t / 100)[1], t / 100) for t in range(5, 100, 5))
        auc = sum(1 if p > n else 0.5 if p == n else 0 for p in pos for n in neg) / (len(pos) * len(neg))
        print(f"{cat:10} {len(xs):4} {tpr:7.2f} {fpr:7.2f} {tpr - fpr:8.2f} {best[1]:8.2f} {best[0]:5.2f} {auc:5.2f}")


GOGS = {
    "ssrf": ("ssrf", ["deliver", "runSync", "MigrateRepository", "getResponse"]),
    "command-injection": ("injection", ["Merge"]),
    "path-traversal": ("traversal", ["UploadRepoFiles", "UserPath", "RepoPath", "isRepositoryGitPath"]),
    "sql-injection": ("injection", []),
}


def gogs(run):
    for t, (q, labels) in GOGS.items():
        rs = sorted(((r["answers"][q]["noul"], r["input"], r["source"]) for r in rows(f"results/{run}/gogs-{t}.jsonl")
                     if r["status"] == "complete"), key=lambda x: -x[0])
        print(f"\n=== {t}: {len(rs)} functions, {sum(p >= .5 for p, _, _ in rs)} at 0.5 or more")
        for label in labels:
            for i, (p, name, src) in enumerate(rs):
                if name.split(".")[-1] == label:
                    print(f"  label {label:20} rank {i + 1:4}  p={p:.2f}  {src}")
        for i, (p, name, src) in enumerate(rs[:15]):
            print(f"  {i + 1:4} {p:.2f} {name:40} {src}")


def paths():
    kinds = {"ssrf": ("ssrf", GOGS["ssrf"][1]), "command": ("injection", ["Merge"]),
             "path": ("traversal", ["UploadRepoFiles"]), "sql": ("injection", [])}
    for k, (q, labels) in kinds.items():
        items = rows(f"paths-{k}.jsonl")
        rs = sorted(((r["answers"][q]["noul"], items[r["index"]]["path"])
                     for r in rows(f"results/paths/gogs-paths-{k}.jsonl")), key=lambda x: -x[0])
        best = {}
        for p, path in rs:
            best.setdefault(path[-1], (p, path))
        ranked = sorted(best.values(), key=lambda x: -x[0])
        print(f"\n=== {k}: {len(rs)} paths, {len(ranked)} sink functions, {sum(p >= .5 for p, _ in ranked)} at 0.5 or more")
        for label in labels:
            for i, (p, path) in enumerate(ranked):
                if path[-1].split(".")[-1] == label:
                    print(f"  label {label:20} rank {i + 1:3}/{len(ranked)}  p={p:.2f}  {' -> '.join(path)}")
        for i, (p, path) in enumerate(ranked[:10]):
            print(f"  {i + 1:3} {p:.2f} {' -> '.join(path)}")


def dive():
    for t, q in [("go-goroutine-leak", "leak"), ("go-context", "dropped"),
                 ("go-error-swallowed", "swallowed"), ("go-typed-nil", "typed_nil")]:
        rs = sorted(((r["answers"][q]["noul"], r["input"], r["source"]) for r in rows(f"results/dive/dive-{t}.jsonl")
                     if r["status"] == "complete"), key=lambda x: -x[0])
        print(f"\n=== {t}: {len(rs)} functions, {sum(p >= .5 for p, _, _ in rs)} at 0.5 or more")
        for p, name, src in rs[:10]:
            print(f"  {p:.2f} {name:42} {src}")


if __name__ == "__main__":
    cmd = sys.argv[1] if len(sys.argv) > 1 else "bench"
    arg = sys.argv[2] if len(sys.argv) > 2 else None
    {"bench": lambda: bench(arg or "ctx"), "gogs": lambda: gogs(arg or "v2"),
     "paths": paths, "dive": dive}[cmd]()
