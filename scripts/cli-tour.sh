#!/usr/bin/env bash
# Tiny live examples for every Decide command. Run from anywhere.
set -euo pipefail

cli=${DECIDE_CLI:-decide}
command -v "$cli" >/dev/null || { echo "Install cmd/decide first." >&2; exit 2; }
command -v jq >/dev/null || { echo "This tour needs jq." >&2; exit 2; }
: "${TYPESAFE_API_KEY:?Set TYPESAFE_API_KEY to run the live tour.}"

tour_dir=$(mktemp -d "${TMPDIR:-/tmp}/decide-tour.XXXXXX")
cd "$tour_dir"
printf 'Tour files: %s\n' "$tour_dir"

# 1. Rescue notes. The banana cake is a distraction.
cat > questions.json <<'JSON'
{"relevant":{"type":"noul","instructions":"Does this passage explain restoring an interrupted software session or resuming work from a saved checkpoint?"}}
JSON
cat > passages.jsonl <<'JSONL'
"To restore an interrupted coding session, load its saved session ID and replay its durable history before accepting new input."
"Bananas are yellow fruit, and this recipe uses flour and sugar."
"Resume the interrupted session by loading its persisted checkpoint and reconnecting the supervisor."
JSONL
cat > policy.json <<'JSON'
{"type":"bands","input":"relevant","measure":"noul","polarity":"high_is_safe","allow":0.8,"review":0.5,"on_missing":"escalate"}
JSON

"$cli" judge --questions questions.json --as rescue \
  < passages.jsonl > observed.jsonl
"$cli" rank --run rescue --answer relevant \
  < observed.jsonl > ranked.jsonl
"$cli" pack --run rescue --answer relevant --budget-bytes 240 \
  < ranked.jsonl > packed.jsonl
echo 'Packed rescue notes:'
jq -r 'select(.runs[-1].result.selected) | .data' packed.jsonl
"$cli" gate --run rescue --policy policy.json \
  < observed.jsonl > gated.jsonl
jq '{text: .data, outcome: .runs[-1].result.outcome}' gated.jsonl

# Keep every grep observation, even when the cake misses the cutoff.
jq -r '.data' observed.jsonl > passages.txt
grep_status=0
"$cli" grep --input text --keep-all --threshold 0.8 \
  --question 'Does this passage explain restoring an interrupted software session?' \
  < passages.txt > grepped.jsonl || grep_status=$?
if (( grep_status > 1 )); then exit "$grep_status"; fi
jq '{text: .data, matched: .runs[-1].result.matched}' grepped.jsonl

# Same policy, fresh judgments, CI-style status. Rejection is a valid result.
check_status=0
"$cli" check --questions questions.json --policy policy.json \
  < passages.jsonl > checked.jsonl || check_status=$?
if (( check_status > 1 )); then exit "$check_status"; fi
printf 'check exit: %s (0 = all allowed; 1 = review or escalate)\n' "$check_status"

# 2. Inbox triage and a proposal with actual edges.
cat > teams.json <<'JSON'
{"instructions":"Which team should handle this support ticket?","criteria":{"billing":"Payments, invoices, refunds, and subscriptions.","engineering":"Software crashes, defects, and broken application behavior."}}
JSON
cat > tickets.jsonl <<'JSONL'
{"id":"double-charge","text":"I was charged twice. Please refund the duplicate invoice payment."}
{"id":"crash","text":"The desktop app crashes with a stack trace when I open a project."}
JSONL
"$cli" label --taxonomy teams.json < tickets.jsonl > labeled.jsonl
echo 'Inbox triage:'
jq '{id, team: .runs[-1].result.label}' labeled.jsonl

cat > rubric.json <<'JSON'
{"specificity":{"instructions":"How concrete and bounded is the proposed software change?","criteria":["Vague wish with no defined behavior.","Some concrete details, but acceptance behavior is incomplete.","Specific behavior and acceptance checks are supplied."]}}
JSON
cat > proposals.jsonl <<'JSONL'
"Make the app better, somehow."
"Add a --max-record-bytes flag defaulting to 1048576. Reject longer records with exit 2. Test the exact boundary."
JSONL
"$cli" score --rubric rubric.json < proposals.jsonl > scored.jsonl
jq '{proposal: .data, score: .runs[-1].response.answers.specificity}' scored.jsonl

# 3. Send the receipt. Then compare two suppliers, one pair at a time.
cat > candidates.jsonl <<'JSONL'
{"state":"Send the receipt to my personal address, dana.personal@example.com, not the billing alias.","candidates":["billing@example.com","dana.personal@example.com"]}
JSONL
"$cli" pick --question 'Which address should receive the receipt?' \
  < candidates.jsonl > picked.jsonl
jq '.runs[-1].result' picked.jsonl

cat > pairs.jsonl <<'JSONL'
{"id":"same","left":{"name":"Acme Tools","country":"US","registration":"ABC-123"},"right":{"name":"ACME Tools LLC","country":"US","registration":"ABC-123"}}
{"id":"different","left":{"name":"Acme Tools","country":"US","registration":"ABC-123"},"right":{"name":"Summit Bakery","country":"CA","registration":"XYZ-987"}}
JSONL
"$cli" join --question 'Do left and right refer to the same supplier, considering name, country, and registration?' \
  < pairs.jsonl > joined.jsonl
jq '{id, relation: .runs[-1].response.answers.relation.noul}' joined.jsonl

# 4. Labeled evidence. Labels never enter model state.
cat > cases.jsonl <<'JSONL'
{"id":"fit-positive","state":"Restore the interrupted session by loading its saved session ID and replaying durable history.","label":"true"}
{"id":"fit-negative","state":"Mash two bananas into the cake batter.","label":"false"}
{"id":"heldout-positive","state":"Resume the interrupted session from its persisted checkpoint and reconnect the supervisor.","label":"true"}
{"id":"heldout-negative","state":"The cake recipe requires two eggs and a cup of flour.","label":"false"}
JSONL
cat > fit-config.json <<'JSON'
{"measure":"noul","polarity":"high_is_safe","noul_target":"true","max_error":0,"min_allowed":1,"ece_bins":10}
JSON
cat > tolerance.json <<'JSON'
{"metric":"raw_brier","direction":"higher_is_worse","max_delta":0.01}
JSON

"$cli" judge --questions questions.json --state-field state \
  < cases.jsonl > cases-observed.jsonl
jq -c 'select(.id | startswith("fit-"))' cases-observed.jsonl \
  | "$cli" eval dataset --answer relevant > fit.json
jq -c 'select(.id | startswith("heldout-"))' cases-observed.jsonl \
  | "$cli" eval dataset --answer relevant > heldout.json
"$cli" eval fit --fit fit.json --heldout heldout.json \
  --config fit-config.json > artifact.json
# Self-comparison checks the plumbing. See the recipe for a new model.
"$cli" eval compare --artifact artifact.json --candidate heldout.json \
  --tolerance tolerance.json > comparison.json
echo 'Offline comparison:'
jq '{baseline_model, candidate_model, delta, exceeded}' comparison.json
printf 'Done. Inputs and real observations are in %s\n' "$tour_dir"
