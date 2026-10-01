// Package heads builds one request with a Choice selector and questions for
// every possible branch, then reads only the selected branch's answers.
//
// EXPERIMENTAL. This package may change or be deleted in any release.
//
// Every branch question must be answerable from the original state without
// seeing the selector answer. Put its branch premise in its instructions;
// question IDs are not sent to the model.
//
// Design: https://github.com/deepnoodle-ai/decide/blob/main/docs/prds/decision-tools.md
package heads
