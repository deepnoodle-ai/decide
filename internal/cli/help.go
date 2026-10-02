package cli

const helpText = `Evaluate input data using a skill:
  decide run code-risk . --include '**/*.go'

List skills:     decide skills list
List saved runs: decide runs list
View results:    decide runs view RUN_ID

Skills define the questions. Run applies them to files, directories, JSONL,
images, URLs, or stdin and saves the answers. Use plan to check prepared inputs
before model calls. Patterns configure how judgments are combined or processed,
using run --pattern.

Answers are readable by default. Add --details for probabilities or --jsonl for
full machine evidence. Commands never prompt. Use COMMAND --help for options.
`
