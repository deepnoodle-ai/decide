// Package funnel runs staged screening: cheap questions about many items,
// then narrower questions about the survivors, carrying each item's answers
// from stage to stage.
//
// EXPERIMENTAL. This package may change or be deleted in any release.
//
// A dropped item is gone for later stages. Thresholds are the caller's and
// must be measured; an error drops only the items it touches, so check
// Report.Failed before trusting the survivors.
//
// Design: https://github.com/deepnoodle-ai/decide/blob/main/docs/prds/decision-tools.md
package funnel
