#!/bin/sh
# Run the check-packs spike. Everything it writes goes under work/, which
# git ignores: the code it reads, decide's answer cache, and the results in
# work/results/. Needs TYPESAFE_API_KEY, Go, git and golangci-lint, and
# about 30,000 model requests on the first run.
set -eu
cd "$(dirname "$0")"
R=work/results
mkdir -p $R/v1 $R/v2 $R/ctx $R/paths $R/dive
export DECIDE_HOME="$PWD/work/home"

(cd ../.. && go build -o spikes/check-packs/work/decide ./cmd/decide)
D=work/decide

[ -d work/BenchmarkJava ] || git clone -q https://github.com/OWASP-Benchmark/BenchmarkJava.git work/BenchmarkJava
git -C work/BenchmarkJava checkout -q 8b67a88 # Benchmark 1.2, as run
[ -d work/gogs ] || git clone -q https://github.com/gogs/gogs.git work/gogs
[ -d work/gogs-snap ] || git -C work/gogs worktree add -q ../gogs-snap 199cf4fd5^
[ -d work/dive ] || git clone -q https://github.com/deepnoodle-ai/dive.git work/dive
[ -d work/dive-snap ] || { mkdir -p work/dive-snap && git -C work/dive archive cae698f | tar -x -C work/dive-snap; }
python3 build_ctx.py

# OWASP Benchmark: each test case alone with both wordings, then with the
# code it calls.
for pair in sqli:sql-injection cmdi:command-injection pathtraver:path-traversal xss:xss \
	crypto:weak-crypto hash:weak-crypto weakrand:weak-crypto; do
	c=${pair%%:*} t=${pair#*:}
	$D run templates-v1/$t work/bench/$c --each file --json >$R/v1/bench-$c.jsonl
	$D run templates/$t work/bench/$c --each file --json >$R/v2/bench-$c.jsonl
	$D run templates/$t work/bench-ctx/$c --each file --json >$R/ctx/bench-$c.jsonl
done

# Gogs, by function, with both wordings.
for t in ssrf command-injection path-traversal sql-injection; do
	$D run templates-v1/$t work/gogs-snap --include '*.go' --exclude '*_test.go' --each function --json \
		>$R/v1/gogs-$t.jsonl
	$D run templates/$t work/gogs-snap --include '*.go' --exclude '*_test.go' --each function --json \
		>$R/v2/gogs-$t.jsonl
done

# Gogs, by function, with the four questions in one request. A fresh
# answer cache, so no question is answered from an earlier run.
mkdir -p $R/combined
DECIDE_HOME="$PWD/work/home-combined" $D run templates-combined/universal work/gogs-snap \
	--include '*.go' --exclude '*_test.go' --each function --json >$R/combined/gogs-universal.jsonl

# Gogs, by call path. The templates default to --each function, so the
# path run uses copies without that default. score.py reads each path's
# function names from work/paths-<sink>.jsonl.
(cd paths && go build -o ../work/paths .)
(cd work/gogs-snap && go mod download)
for pair in command:command-injection path:path-traversal ssrf:ssrf sql:sql-injection; do
	k=${pair%%:*} t=${pair#*:}
	work/paths -sink "$k" work/gogs-snap >work/paths-$k.jsonl
	mkdir -p work/templates-rec/$t
	python3 -c "import json; d=json.load(open('templates/$t/template.json')); d.pop('each'); json.dump(d, open('work/templates-rec/$t/template.json','w'), indent=2)"
	$D run work/templates-rec/$t work/paths-$k.jsonl --field text --json >$R/paths/gogs-paths-$k.jsonl
done

# dive, Go pack, beside golangci-lint.
for t in go-context go-goroutine-leak go-error-swallowed go-typed-nil; do
	$D run templates/$t work/dive-snap --include '*.go' --exclude '*_test.go' --each function --json \
		>$R/dive/dive-$t.jsonl
done
(cd work/dive-snap && golangci-lint run --default=none \
	-E errcheck,errorlint,ineffassign,staticcheck,bodyclose,sqlclosecheck,contextcheck,govet,gosec,nilnil,nilerr \
	--output.json.path=../results/dive/golangci-lint.json ./... >/dev/null) || true

echo "Done. Score the results with: python3 score.py bench|gogs|paths|dive"
