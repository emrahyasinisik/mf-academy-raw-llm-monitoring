package analysis

import (
	"math"
	"testing"
)

// The behaviours asserted here are the product's claims, not implementation
// details. If any of these change, what the reports mean changes with them.

func ptr(f float64) *float64 { return &f }

func rubric() []Criterion {
	return []Criterion{
		{Key: "a", Weight: 0.5, ScaleMax: 5},
		{Key: "b", Weight: 0.3, ScaleMax: 5},
		{Key: "c", Weight: 0.2, ScaleMax: 5},
	}
}

func TestScoreWeightsEachCriterion(t *testing.T) {
	// 5/5 on the heaviest, 0/5 on the rest: 0.5*1 + 0.3*0 + 0.2*0 = 0.5 of a
	// full rubric weight of 1.0.
	overall, coverage := Score(rubric(), []Finding{
		{Key: "a", EvidenceFound: true, Score: ptr(5)},
		{Key: "b", EvidenceFound: true, Score: ptr(0)},
		{Key: "c", EvidenceFound: true, Score: ptr(0)},
	})
	if overall == nil {
		t.Fatal("expected a score")
	}
	if math.Abs(*overall-50) > 0.001 {
		t.Errorf("overall = %v, want 50", *overall)
	}
	if math.Abs(coverage-1.0) > 0.001 {
		t.Errorf("coverage = %v, want 1.0", coverage)
	}
}

// The central design decision: absence of evidence is not evidence of absence.
// A criterion nobody addressed must not be scored as a failure.
func TestMissingEvidenceIsExcludedNotZeroed(t *testing.T) {
	// Only `a` has evidence, and it is perfect. If missing criteria were
	// scored zero the result would be 50; excluded, it is 100 at 0.5 coverage.
	overall, coverage := Score(rubric(), []Finding{
		{Key: "a", EvidenceFound: true, Score: ptr(5)},
		{Key: "b", EvidenceFound: false},
		{Key: "c", EvidenceFound: false},
	})
	if overall == nil {
		t.Fatal("expected a score")
	}
	if math.Abs(*overall-100) > 0.001 {
		t.Errorf("overall = %v, want 100 (silence must not be scored as failure)", *overall)
	}
	if math.Abs(coverage-0.5) > 0.001 {
		t.Errorf("coverage = %v, want 0.5", coverage)
	}
}

// A thin-but-strong case and a thorough-and-strong case must be
// distinguishable. They score the same and are told apart by coverage — which
// is why the report must never show one number without the other.
func TestCoverageSeparatesThinFromThorough(t *testing.T) {
	thin, thinCov := Score(rubric(), []Finding{
		{Key: "a", EvidenceFound: true, Score: ptr(4)},
	})
	thorough, thoroughCov := Score(rubric(), []Finding{
		{Key: "a", EvidenceFound: true, Score: ptr(4)},
		{Key: "b", EvidenceFound: true, Score: ptr(4)},
		{Key: "c", EvidenceFound: true, Score: ptr(4)},
	})
	if thin == nil || thorough == nil {
		t.Fatal("expected both to score")
	}
	if math.Abs(*thin-*thorough) > 0.001 {
		t.Errorf("scores differ (%v vs %v); they should be equal and separated by coverage", *thin, *thorough)
	}
	if thinCov >= thoroughCov {
		t.Errorf("coverage failed to separate them: thin=%v thorough=%v", thinCov, thoroughCov)
	}
}

// Nothing assessable must return nil, not 0. Zero is a claim about the subject;
// nil says the subject could not be assessed.
func TestNoEvidenceAtAllReturnsNil(t *testing.T) {
	overall, coverage := Score(rubric(), []Finding{
		{Key: "a", EvidenceFound: false},
		{Key: "b", EvidenceFound: false},
	})
	if overall != nil {
		t.Errorf("overall = %v, want nil", *overall)
	}
	if coverage != 0 {
		t.Errorf("coverage = %v, want 0", coverage)
	}
}

// A finding claiming evidence but carrying no rating cannot be scored, and must
// be treated as unassessed rather than as zero.
func TestEvidenceFoundWithoutScoreIsUnassessed(t *testing.T) {
	overall, coverage := Score(rubric(), []Finding{
		{Key: "a", EvidenceFound: true, Score: ptr(5)},
		{Key: "b", EvidenceFound: true, Score: nil},
	})
	if overall == nil || math.Abs(*overall-100) > 0.001 {
		t.Fatalf("overall = %v, want 100", overall)
	}
	if math.Abs(coverage-0.5) > 0.001 {
		t.Errorf("coverage = %v, want 0.5", coverage)
	}
}

// Models occasionally return out-of-range ratings. Clamping loses less than
// discarding the finding would.
func TestOutOfRangeScoresAreClamped(t *testing.T) {
	over, _ := Score([]Criterion{{Key: "a", Weight: 1, ScaleMax: 5}},
		[]Finding{{Key: "a", EvidenceFound: true, Score: ptr(9)}})
	if over == nil || math.Abs(*over-100) > 0.001 {
		t.Errorf("above-range score = %v, want clamped to 100", over)
	}
	under, _ := Score([]Criterion{{Key: "a", Weight: 1, ScaleMax: 5}},
		[]Finding{{Key: "a", EvidenceFound: true, Score: ptr(-3)}})
	if under == nil || math.Abs(*under-0) > 0.001 {
		t.Errorf("below-range score = %v, want clamped to 0", under)
	}
}

// A rubric that omits scale_max must not divide by zero and produce NaN.
func TestMissingScaleMaxDefaults(t *testing.T) {
	overall, _ := Score([]Criterion{{Key: "a", Weight: 1}},
		[]Finding{{Key: "a", EvidenceFound: true, Score: ptr(5)}})
	if overall == nil {
		t.Fatal("expected a score")
	}
	if math.IsNaN(*overall) {
		t.Fatal("missing scale_max produced NaN")
	}
	if math.Abs(*overall-100) > 0.001 {
		t.Errorf("overall = %v, want 100 on the default 0-5 scale", *overall)
	}
}

// A negative weight must not let one criterion cancel another out.
func TestNonPositiveWeightsAreIgnored(t *testing.T) {
	overall, coverage := Score([]Criterion{
		{Key: "a", Weight: 1, ScaleMax: 5},
		{Key: "bad", Weight: -5, ScaleMax: 5},
		{Key: "zero", Weight: 0, ScaleMax: 5},
	}, []Finding{
		{Key: "a", EvidenceFound: true, Score: ptr(5)},
		{Key: "bad", EvidenceFound: true, Score: ptr(5)},
		{Key: "zero", EvidenceFound: true, Score: ptr(0)},
	})
	if overall == nil || math.Abs(*overall-100) > 0.001 {
		t.Errorf("overall = %v, want 100", overall)
	}
	if math.Abs(coverage-1.0) > 0.001 {
		t.Errorf("coverage = %v, want 1.0 (only the valid criterion counts)", coverage)
	}
}

// Findings for criteria that are not in the rubric must be ignored, not
// silently folded in — the snapshot is the authority on what is being scored.
func TestUnknownFindingKeysAreIgnored(t *testing.T) {
	overall, coverage := Score([]Criterion{{Key: "a", Weight: 1, ScaleMax: 5}},
		[]Finding{
			{Key: "a", EvidenceFound: true, Score: ptr(3)},
			{Key: "invented", EvidenceFound: true, Score: ptr(5)},
		})
	if overall == nil || math.Abs(*overall-60) > 0.001 {
		t.Errorf("overall = %v, want 60", overall)
	}
	if math.Abs(coverage-1.0) > 0.001 {
		t.Errorf("coverage = %v, want 1.0", coverage)
	}
}

// Same inputs, same outputs — the reproducibility the product is sold on.
func TestScoringIsDeterministic(t *testing.T) {
	findings := []Finding{
		{Key: "a", EvidenceFound: true, Score: ptr(4)},
		{Key: "b", EvidenceFound: true, Score: ptr(2)},
		{Key: "c", EvidenceFound: false},
	}
	first, firstCov := Score(rubric(), findings)
	for i := 0; i < 100; i++ {
		got, cov := Score(rubric(), findings)
		if *got != *first || cov != firstCov {
			t.Fatalf("run %d differed: %v/%v vs %v/%v", i, *got, cov, *first, firstCov)
		}
	}
}

func TestStatsPopulationStdDev(t *testing.T) {
	mean, sd, min, max, ok := Stats([]float64{2, 4, 4, 4, 5, 5, 7, 9})
	if !ok {
		t.Fatal("expected ok")
	}
	if math.Abs(mean-5) > 0.001 {
		t.Errorf("mean = %v, want 5", mean)
	}
	// Population stddev of that classic set is exactly 2; the sample form
	// would give ~2.138.
	if math.Abs(sd-2) > 0.001 {
		t.Errorf("stddev = %v, want 2 (population, not sample)", sd)
	}
	if min != 2 || max != 9 {
		t.Errorf("min/max = %v/%v, want 2/9", min, max)
	}
}

func TestStatsEmpty(t *testing.T) {
	if _, _, _, _, ok := Stats(nil); ok {
		t.Error("expected ok=false for an empty set")
	}
}

// A criterion measured once has no spread; reporting 0 would read as
// "perfectly stable".
func TestPerCriterionStdDevSkipsSingleObservations(t *testing.T) {
	out := PerCriterionStdDev(rubric(), [][]Finding{
		{{Key: "a", EvidenceFound: true, Score: ptr(4)}, {Key: "b", EvidenceFound: true, Score: ptr(1)}},
		{{Key: "a", EvidenceFound: true, Score: ptr(2)}},
	})
	if _, present := out["b"]; present {
		t.Error("b was scored once and should be absent, not 0")
	}
	if _, present := out["a"]; !present {
		t.Fatal("a was scored twice and should be present")
	}
	// 4/5 and 2/5 -> 0.8 and 0.4, mean 0.6, population stddev 0.2.
	if math.Abs(out["a"]-0.2) > 0.0001 {
		t.Errorf("a stddev = %v, want 0.2", out["a"])
	}
}

// A perfectly stable criterion should report 0, distinguishing it from one that
// was never measured (which is absent).
func TestPerCriterionStdDevZeroForStable(t *testing.T) {
	out := PerCriterionStdDev(rubric(), [][]Finding{
		{{Key: "a", EvidenceFound: true, Score: ptr(3)}},
		{{Key: "a", EvidenceFound: true, Score: ptr(3)}},
	})
	sd, present := out["a"]
	if !present {
		t.Fatal("a should be present")
	}
	if sd != 0 {
		t.Errorf("stddev = %v, want 0", sd)
	}
}
