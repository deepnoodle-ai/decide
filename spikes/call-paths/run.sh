#!/bin/sh
# Run the call-paths spike on Gogs. Everything it writes goes under work/,
# which git ignores. Needs TYPESAFE_API_KEY, Go and git. The function sweep
# it compares against is the check-packs spike's: run ../check-packs/run.sh
# first, or pass its results folder to score.py.
set -eu
cd "$(dirname "$0")"
mkdir -p work/results
export DECIDE_HOME="$PWD/work/home"

(cd ../.. && go build -o spikes/call-paths/work/decide ./cmd/decide)
(cd builder && go build -o ../work/builder .)
[ -d work/gogs ] || git clone -q https://github.com/gogs/gogs.git work/gogs
[ -d work/gogs-snap ] || git -C work/gogs worktree add -q ../gogs-snap 199cf4fd5^
(cd work/gogs-snap && go mod download)

for pair in ssrf:ssrf command:command-injection path:path-traversal sql:sql-injection; do
	k=${pair%%:*} t=${pair#*:}
	work/builder -sink "$k" -carry -orphans work/gogs-snap >work/carry-$k.jsonl 2>work/carry-$k.err
	cat work/carry-$k.err
	# The templates default to --each function; decide reads JSONL only
	# without it, so the run uses copies without that default.
	mkdir -p work/templates-rec/$t
	python3 -c "import json; d=json.load(open('../check-packs/templates/$t/template.json')); d.pop('each'); json.dump(d, open('work/templates-rec/$t/template.json','w'), indent=2)"
	work/decide run work/templates-rec/$t work/carry-$k.jsonl --field text --json >work/results/carry-$k.jsonl
done

echo "Done. Score the results with: python3 score.py --carry [--no-deps]"
