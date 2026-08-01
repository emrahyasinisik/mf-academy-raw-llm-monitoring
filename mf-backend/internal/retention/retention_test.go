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

func (f *fakeSweeper) SweepConversations(_ context.Context, olderThan time.Time) (int64, error) {
	f.calls++
	f.cutoff = olderThan
	return f.n, f.err
}

var now = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

func TestSweepPassesTheSameCutoffToAllThree(t *testing.T) {
	a, r, c := &fakeSweeper{n: 3}, &fakeSweeper{n: 5}, &fakeSweeper{n: 7}
	got, err := Sweep(context.Background(), a, r, c, 30*24*time.Hour, now)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	want := now.Add(-30 * 24 * time.Hour)
	if !a.cutoff.Equal(want) || !r.cutoff.Equal(want) || !c.cutoff.Equal(want) {
		t.Errorf("cutoffs %v / %v / %v, want all %v", a.cutoff, r.cutoff, c.cutoff, want)
	}
	if got.Assessments != 3 || got.Runs != 5 || got.Conversations != 7 {
		t.Errorf("Result = %+v, want {3 5 7}", got)
	}
}

// Bir tablonun düşmesi diğer ikisini bağışlamamalı: tablolar bağımsız
// başarısız oluyor, ve sağlam olanı atlamak kişisel veriyi yerinde bırakır.
func TestSweepRunsAllThreeEvenWhenTheFirstFails(t *testing.T) {
	boom := errors.New("connection reset")
	a, r, c := &fakeSweeper{err: boom}, &fakeSweeper{n: 5}, &fakeSweeper{n: 7}
	got, err := Sweep(context.Background(), a, r, c, time.Hour, now)
	if r.calls != 1 || c.calls != 1 {
		t.Errorf("runs called %d, conversations called %d; want 1 and 1", r.calls, c.calls)
	}
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want it to wrap %v", err, boom)
	}
	if got.Runs != 5 || got.Conversations != 7 {
		t.Errorf("Result = %+v, want the successful halves reported anyway", got)
	}
}

func TestSweepJoinsAllFailures(t *testing.T) {
	first, second, third := errors.New("a down"), errors.New("r down"), errors.New("c down")
	a, r, c := &fakeSweeper{err: first}, &fakeSweeper{err: second}, &fakeSweeper{err: third}
	_, err := Sweep(context.Background(), a, r, c, time.Hour, now)
	for _, want := range []error{first, second, third} {
		if !errors.Is(err, want) {
			t.Errorf("err = %v, want it to wrap %v", err, want)
		}
	}
}
