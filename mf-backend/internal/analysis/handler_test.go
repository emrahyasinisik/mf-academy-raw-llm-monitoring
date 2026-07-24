package analysis

import (
	"testing"
	"time"
)

// The default chat budget must not be allowed to truncate a rubric answer.
// A cut-off JSON scores schema_valid=false for reasons that have nothing to do
// with the model, and that bogus figure is what a fine-tuning decision would
// then be made against.
func TestTokenBudgetFloorsAboveTheChatDefault(t *testing.T) {
	// The shipped startup rubric has 9 criteria; the settings default is 512.
	got := tokenBudget(9, 512)
	if got <= 512 {
		t.Errorf("tokenBudget(9, 512) = %d; the default would truncate the answer", got)
	}
	if got < 9*100 {
		t.Errorf("tokenBudget(9, 512) = %d; too small for nine findings with evidence", got)
	}
}

// It is a floor, not an override — a generous operator setting must survive.
func TestTokenBudgetKeepsLargerConfiguredValue(t *testing.T) {
	if got := tokenBudget(3, 2048); got != 2048 {
		t.Errorf("tokenBudget(3, 2048) = %d, want 2048", got)
	}
}

// The card is 6 GB in --mode local; an unbounded budget buys a timeout, not a
// longer answer.
func TestTokenBudgetIsCapped(t *testing.T) {
	if got := tokenBudget(500, 0); got > 4096 {
		t.Errorf("tokenBudget(500, 0) = %d, want it capped at 4096", got)
	}
}

// trialTimeout must cover every sequential run, or a trial is cut off after
// roughly the first generation and reports a spread built from one sample.
func TestTrialTimeoutCoversEveryRun(t *testing.T) {
	gen := time.Second * 25
	got := TrialTimeout(gen)
	if got < gen*maxTrials {
		t.Errorf("TrialTimeout(%v) = %v; too short for %d sequential runs", gen, got, maxTrials)
	}
}

func TestTrialTimeoutIsCapped(t *testing.T) {
	if got := TrialTimeout(time.Second * 600); got > time.Minute*30 {
		t.Errorf("trialTimeout = %v, want capping at 30m", got)
	}
}
