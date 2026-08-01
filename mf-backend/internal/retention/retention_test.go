package retention

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeSweeper struct {
	n      int64
	err    error
	cutoff time.Time
	calls  int
}

func (f *fakeSweeper) SweepAssessments(_ context.Context, olderThan time.Time) (int64, error) {
	f.calls++
	f.cutoff = olderThan
	return f.n, f.err
}

func (f *fakeSweeper) SweepRuns(_ context.Context, olderThan time.Time) (int64, error) {
	f.calls++
	f.cutoff = olderThan
	return f.n, f.err
}

var now = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

func TestSweepPassesTheSameCutoffToBoth(t *testing.T) {
	a, r := &fakeSweeper{n: 3}, &fakeSweeper{n: 5}
	got, err := Sweep(context.Background(), a, r, 30*24*time.Hour, now)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	want := now.Add(-30 * 24 * time.Hour)
	if !a.cutoff.Equal(want) || !r.cutoff.Equal(want) {
		t.Errorf("cutoffs %v / %v, want both %v", a.cutoff, r.cutoff, want)
	}
	if got.Assessments != 3 || got.Runs != 5 {
		t.Errorf("Result = %+v, want {3 5}", got)
	}
}

// One table failing must not spare the other. A sweep that gives up on the
// first error leaves personal data behind in a table that was perfectly
// healthy, and the next run an hour later hits the same broken one first.
func TestSweepRunsBothEvenWhenTheFirstFails(t *testing.T) {
	boom := errors.New("connection reset")
	a, r := &fakeSweeper{err: boom}, &fakeSweeper{n: 5}
	got, err := Sweep(context.Background(), a, r, time.Hour, now)
	if r.calls != 1 {
		t.Errorf("runs sweeper called %d times, want 1", r.calls)
	}
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want it to wrap %v", err, boom)
	}
	if got.Runs != 5 {
		t.Errorf("Result.Runs = %d, want the successful half reported anyway", got.Runs)
	}
}
