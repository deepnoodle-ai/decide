#!/usr/bin/env python3
"""Score the check-packs spike from the results run.sh writes to work/results/.

    python3 score.py bench [v1|v2|ctx|combined|security-v2]  OWASP Benchmark, by category
    python3 score.py gogs [v1|v2]         Gogs, labeled functions by rank
    python3 score.py paths                Gogs call paths, by sink function
    python3 score.py dive                 dive, top candidates per Go template
    python3 score.py n8n                  n8n, labeled functions by rank, per question
    python3 score.py combined [universal|security|security-v2]
                                          Gogs, a pack's questions in one request vs one each

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
    if run in ("combined", "security-v2"):
        # templates-combined/security asks every question of every case.
        questions = {"sqli": "sql_injection", "cmdi": "command_injection", "pathtraver": "path_traversal",
                     "xss": "xss", "crypto": "weak_cipher", "hash": "weak_hash", "weakrand": "weak_random"}
    print(f"{'category':10} {'n':>4} {'TPR@.5':>7} {'FPR@.5':>7} {'score@.5':>8} {'best thr':>8} {'best':>5} {'AUC':>5}")
    for cat, q in questions.items():
        try:
            rs = rows(f"results/combined/bench-v2-{cat}.jsonl" if run == "security-v2"
                      else f"results/{run}/bench-{cat}.jsonl")
        except FileNotFoundError:
            continue
        xs = [(r["answers"][q]["noul"], truth[os.path.basename(r["source"])[:-5]]) for r in rs
              if r["status"] == "complete"]
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


def avg_ranks(xs):
    """Ranks from 1, with tied values given the mean of their ranks."""
    order = sorted(range(len(xs)), key=lambda i: xs[i])
    ranks = [0.0] * len(xs)
    i = 0
    while i < len(order):
        j = i
        while j + 1 < len(order) and xs[order[j + 1]] == xs[order[i]]:
            j += 1
        for k in order[i:j + 1]:
            ranks[k] = (i + j) / 2 + 1
        i = j + 1
    return ranks


def spearman(a, b):
    """Spearman's rank correlation: the Pearson correlation of average ranks."""
    ra, rb = avg_ranks(a), avg_ranks(b)
    ma, mb = sum(ra) / len(ra), sum(rb) / len(rb)
    cov = sum((x - ma) * (y - mb) for x, y in zip(ra, rb))
    return cov / (sum((x - ma) ** 2 for x in ra) * sum((y - mb) ** 2 for y in rb)) ** 0.5


def combined(pack):
    """Compare a pack in templates-combined, which asks all its questions in
    one request, with the v2 templates, which ask one each."""
    rs = [r for r in rows(f"results/combined/gogs-{pack}.jsonl") if r["status"] == "complete"]
    print(f"{len(rs)} functions, {len({r['request_id'] for r in rs})} requests")
    for t, key in [("ssrf", "ssrf"), ("command-injection", "command_injection"),
                   ("path-traversal", "path_traversal"), ("sql-injection", "sql_injection")]:
        q, labels = GOGS[t]
        sep = {(r["source"], r["input"]): r["answers"][q]["noul"]
               for r in rows(f"results/v2/gogs-{t}.jsonl") if r["status"] == "complete"}
        com = {(r["source"], r["input"]): r["answers"][key]["noul"] for r in rs}
        both = sorted(set(sep) & set(com))
        diffs = [abs(sep[k] - com[k]) for k in both]
        rank = lambda d: {k: i + 1 for i, k in enumerate(sorted(d, key=lambda k: -d[k]))}
        rs_, rc = rank({k: sep[k] for k in both}), rank({k: com[k] for k in both})
        n = len(both)
        rho = spearman([sep[k] for k in both], [com[k] for k in both])
        top = lambda r: {k for k in both if r[k] <= 30}
        print(f"\n=== {t}: {n} functions; flagged at 0.5: one each {sum(sep[k] >= .5 for k in both)}, "
              f"combined {sum(com[k] >= .5 for k in both)}")
        print(f"  mean |difference| {sum(diffs) / n:.3f}; largest {max(diffs):.2f}; over 0.2: {sum(d > .2 for d in diffs)}; "
              f"rank correlation {rho:.3f}; top 30 shared {len(top(rs_) & top(rc))}")
        for label in labels + (["GetByCollaboratorID", "searchUserByName"] if t == "sql-injection" else []):
            for k in both:
                if k[1].split(".")[-1] == label:
                    print(f"  {label:22} one each rank {rs_[k]:4} p={sep[k]:.2f}   combined rank {rc[k]:4} p={com[k]:.2f}")
    # Questions with no one-each run on Gogs: flagged counts and top hits.
    for key in [k for k in rs[0]["answers"] if k not in
                ("ssrf", "command_injection", "path_traversal", "sql_injection")]:
        top = sorted(rs, key=lambda r: -r["answers"][key]["noul"])
        dirs = {}
        for r in rs:
            if r["answers"][key]["noul"] >= .5:
                d = "/".join(r["source"].split("/")[1:3])  # past gogs-snap/
                dirs[d] = dirs.get(d, 0) + 1
        print(f"\n=== {key}: flagged at 0.5 {sum(r['answers'][key]['noul'] >= .5 for r in rs)}, "
              f"at 0.7 {sum(r['answers'][key]['noul'] >= .7 for r in rs)}; by folder "
              + ", ".join(f"{d} {n}" for d, n in sorted(dirs.items(), key=lambda x: -x[1])[:4]))
        for r in top[:5]:
            print(f"  {r['answers'][key]['noul']:.2f} {r['input']:42} {r['source']}")


def n8n():
    """Rank n8n's labeled functions on each question of the security pack."""
    from n8n_labels import LABELS
    rs = [r for r in rows("results/combined/n8n-security.jsonl") if r["status"] == "complete"]
    print(f"{len(rs)} functions")
    labeled = {}
    for q, ghsa, path, name in LABELS:
        hits = [i for i, r in enumerate(rs) if r["source"].split("#")[0].endswith(path)
                and (r.get("input") == name or r.get("input", "").endswith("." + name))]
        if not hits:
            print(f"  not found: {ghsa} {path} {name}")
        labeled.setdefault(q, set()).update(hits)
    for q in rs[0]["answers"]:
        ps = [r["answers"][q]["noul"] for r in rs]
        order = sorted(range(len(rs)), key=lambda i: -ps[i])
        rank = {i: n + 1 for n, i in enumerate(order)}
        pos = [ps[i] for i in labeled.get(q, ())]
        line = f"\n=== {q}: flagged " + ", ".join(f"{t}: {sum(p >= t for p in ps)}" for t in (.5, .6, .7, .8, .9))
        if pos:
            neg = [ps[i] for i in range(len(rs)) if i not in labeled[q]]
            auc = sum(1 if p > n else .5 if p == n else 0 for p in pos for n in neg) / (len(pos) * len(neg))
            line += f"\n  {len(pos)} labels, AUC {auc:.2f}; found " + ", ".join(
                f"{t}: {sum(p >= t for p in pos)}" for t in (.5, .6, .7, .8, .9))
        print(line)
        for i in sorted(labeled.get(q, ()), key=lambda i: rank[i]):
            print(f"  rank {rank[i]:5} p={ps[i]:.2f} {rs[i]['input']}  {rs[i]['source'].split('packages/')[-1]}")
        for i in order[:5]:
            print(f"  top {ps[i]:.2f} {rs[i].get('input', '')}  {rs[i]['source'].split('packages/')[-1]}")


if __name__ == "__main__":
    cmd = sys.argv[1] if len(sys.argv) > 1 else "bench"
    arg = sys.argv[2] if len(sys.argv) > 2 else None
    {"bench": lambda: bench(arg or "ctx"), "gogs": lambda: gogs(arg or "v2"),
     "paths": paths, "dive": dive, "n8n": n8n, "combined": lambda: combined(arg or "universal")}[cmd]()
