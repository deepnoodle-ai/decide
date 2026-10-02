package cli

const helpText = `Run a reusable judgment on your data:
  decide run code-risk . --include '**/*.go'

Find a judgment:  decide skills list
Read past work:  decide runs list
                 decide runs view RUN_ID

Skills define the questions. Run applies them to files, directories, JSONL,
images, URLs, or stdin and saves the answers. Use plan to check prepared inputs
before model calls. Patterns are advanced compositions, used with run --pattern.

Answers are readable by default. Add --details for probabilities or --jsonl for
full machine evidence. Commands never prompt. Use COMMAND --help for options.
`
