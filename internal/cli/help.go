package cli

const helpText = `decide — a little judgment, a lot of evidence.
Experimental System One decisions for datasets and shell pipelines.

Usage: decide COMMAND [options]

Start here
  run SKILL SOURCES...       show each item's decisions and save the run
  runs list                 list saved runs
  runs view RUN_ID          read an old run's decisions; no model calls

  decide run code-risk . --include '**/*.go'
  decide runs list
  decide runs view RUN_ID

See more, or use a pipeline
  --details                 confidence and labeled probability distributions
  --jsonl                   full evidence for scripts/pipes
  --output FILE             full evidence in a new file (run)
  --color auto|always|never  terminal highlighting; auto respects NO_COLOR

Choose data and judgments
  sources list|preview SOURCES...  files, directories, JSONL, images, URLs, stdin
  plan SKILL SOURCES...            prepared inputs and questions; no model calls
  skills list|show|new|edit|validate|test  reusable judgments
  patterns list|show               compose judgments

Manage saved runs
  runs show|watch RUN_ID     status and counts
  runs resume RUN_ID         continue an interrupted run
  runs export RUN_ID         export full JSONL evidence

Small tools, big pipelines
  judge   ask named typed questions     grep   filter with a probability cutoff
  label   choose a taxonomy option      score  judge against ordered rubrics
  pick    choose a supplied candidate   join   judge supplied pairs
  rank    sort saved answers            pack   select strings under a byte budget
  gate    apply an explicit policy      check  judge and apply policy in CI
  eval dataset|fit|compare              calibrate and compare saved evidence

Use COMMAND --help for options. Commands never prompt.
Run output is readable by default; use --jsonl for full machine evidence.
Credentials: TYPESAFE_API_KEY, or CLOUDFLARE_AUTH_TOKEN + CLOUDFLARE_ACCOUNT_ID.
Default connection: DECIDE_PROVIDER, DECIDE_MODEL, DECIDE_PROFILE.
`
