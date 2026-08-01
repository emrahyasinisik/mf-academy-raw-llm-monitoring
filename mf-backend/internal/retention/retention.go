// Package retention enforces the demo's storage limit: content older than the
// configured age is redacted, not deleted, so the measurement row survives its
// contents. See docs/superpowers/specs/2026-08-01-kvkk-ve-veri-silme-design.md.
package retention

import (
	"context"
	"errors"
	"time"
)

// AssessmentSweeper is the analysis store, declared here so this package does
// not import it — the dependency runs the other way at wiring time.
type AssessmentSweeper interface {
	SweepAssessments(ctx context.Context, olderThan time.Time) (int64, error)
}

// RunSweeper is the llm store, declared here so this package does not import
// it — the dependency runs the other way at wiring time.
type RunSweeper interface {
	SweepRuns(ctx context.Context, olderThan time.Time) (int64, error)
}

// ConversationSweeper is the decision store. Unlike the other two this one
// deletes rather than blanks: a conversation feeds no aggregate, so there is no
// measurement left behind worth keeping, and a row emptied of its messages
// would be a tombstone nobody reads.
type ConversationSweeper interface {
	SweepConversations(ctx context.Context, olderThan time.Time) (int64, error)
}

// Result tracks redactions per table so the caller knows which table succeeded
// and which failed: a skipped table is a data sovereignty problem that demands
// explicit visibility, not absorbed in a total.
type Result struct {
	Assessments   int64
	Runs          int64
	Conversations int64
}

// Sweep redacts everything older than age in both tables, and deletes the
// same-aged conversations outright.
//
// now is a parameter rather than a call to time.Now, because the cutoff is the
// only interesting thing this function computes and a test that cannot pin it
// is testing nothing.
//
// All three sweepers always run. Errors are joined rather than returned early:
// the tables fail independently, and abandoning the rest would leave personal
// data in a table that had nothing wrong with it.
func Sweep(
	ctx context.Context,
	a AssessmentSweeper,
	r RunSweeper,
	c ConversationSweeper,
	age time.Duration,
	now time.Time,
) (Result, error) {
	cutoff := now.Add(-age)

	var res Result
	var errs []error

	n, err := a.SweepAssessments(ctx, cutoff)
	if err != nil {
		errs = append(errs, err)
	}
	res.Assessments = n

	m, err := r.SweepRuns(ctx, cutoff)
	if err != nil {
		errs = append(errs, err)
	}
	res.Runs = m

	k, err := c.SweepConversations(ctx, cutoff)
	if err != nil {
		errs = append(errs, err)
	}
	res.Conversations = k

	return res, errors.Join(errs...)
}
