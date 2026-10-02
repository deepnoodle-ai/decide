package cli

const helpText = `decide — a little judgment, a lot of evidence.
Experimental System One decisions for datasets and shell pipelines.

Usage: decide COMMAND [options]

Take a look around
  explore SOURCES...             open the interactive judgment workbench
  inspect RUN_OR_FILE            browse recorded answers and their evidence
  skills list|show|new|edit|validate|test  find a judgment or make one your own
  patterns list|show             learn ways to compose judgments

Bring your data
  sources list|preview SOURCES...  files, directories, JSONL, images, URLs, stdin
  plan SKILL SOURCES...            preview prepared inputs; no model calls
  run SKILL SOURCES...             run a dataset; show each item’s decisions
    --details                     confidence and probability distributions
    --jsonl                       full evidence for scripts/pipes
    --output FILE                 full evidence in a new file
  runs list|show|watch|resume|export  reopen your judgment notebook

Small tools, big pipelines
  judge   ask named typed questions     grep   filter with a probability cutoff
  label   choose a taxonomy option      score  judge against ordered rubrics
  pick    choose a supplied candidate   join   judge supplied pairs
  rank    sort saved answers            pack   select strings under a byte budget
  gate    apply an explicit policy      check  judge and apply policy in CI
  eval dataset|fit|compare              calibrate and compare saved evidence

Try a small experiment
  decide explore . --skill builtin/code-risk --include '**/*.go'
  decide plan builtin/code-risk . --include '**/*.go' --sample 5 --seed 42
  decide run builtin/ticket-routing tickets.jsonl --workers 8

Use COMMAND --help for options. Batch commands never prompt.
Stdout carries structured results or requested help; stderr carries diagnostics.
Credentials: TYPESAFE_API_KEY, or CLOUDFLARE_AUTH_TOKEN + CLOUDFLARE_ACCOUNT_ID.
Default connection: DECIDE_PROVIDER, DECIDE_MODEL, DECIDE_PROFILE.
`
