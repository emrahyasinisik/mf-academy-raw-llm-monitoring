package llm

import (
	"strings"
	"testing"
)

// benchRun is a representative payload: a ~3KB answer, typical for Gemma 2B.
func benchRun() Run {
	return Run{
		ID:               "11111111-1111-1111-1111-111111111111",
		Response:         strings.Repeat("The quick brown fox jumps over the lazy dog. ", 68),
		LatencyMs:        2400,
		CompletionTokens: 780,
		ExpectedKeywords: []string{"fox", "dog", "jumps", "lazy", "brown"},
	}
}

// BenchmarkScoreRun guards the allocation budget of the scoring hot path, which
// runs on every auto-scored POST /llm/runs. It regressed badly when the
// response was lower-cased once per component instead of once per run.
func BenchmarkScoreRun(b *testing.B) {
	run := benchRun()
	w := DefaultWeights()
	b.ReportAllocs()
	for b.Loop() {
		_ = ScoreRun(run, w)
	}
}

func TestScoreRunGradesAStrongAnswer(t *testing.T) {
	run := benchRun()
	got := ScoreRun(run, DefaultWeights())

	if got.RunID != run.ID {
		t.Errorf("RunID = %q, want %q", got.RunID, run.ID)
	}
	if got.Score <= 0 || got.Score > 100 {
		t.Errorf("Score = %v, want within (0,100]", got.Score)
	}
	// Every keyword is present in the repeated sentence.
	if got.Breakdown.Keywords != 100 {
		t.Errorf("Breakdown.Keywords = %v, want 100", got.Breakdown.Keywords)
	}
	if got.Breakdown.Completion != 100 {
		t.Errorf("Breakdown.Completion = %v, want 100", got.Breakdown.Completion)
	}
}

func TestScoreRunEmptyResponse(t *testing.T) {
	got := ScoreRun(Run{Response: "   "}, DefaultWeights())
	if got.Breakdown.Completion != 0 {
		t.Errorf("Breakdown.Completion = %v, want 0", got.Breakdown.Completion)
	}
	if got.Grade != "F" {
		t.Errorf("Grade = %q, want F", got.Grade)
	}
}

// Sharing one lower-cased copy across components must not change behaviour:
// refusal detection and keyword matching both have to stay case-insensitive.
func TestScoringIsCaseInsensitive(t *testing.T) {
	refusal := ScoreRun(Run{Response: "I CANNOT help with that request, sorry."}, DefaultWeights())
	if refusal.Breakdown.Completion != 30 {
		t.Errorf("Breakdown.Completion = %v, want 30 for an upper-case refusal", refusal.Breakdown.Completion)
	}

	kw := ScoreRun(Run{
		Response:         "The Fox and the DOG.",
		ExpectedKeywords: []string{"fox", "dog"},
	}, DefaultWeights())
	if kw.Breakdown.Keywords != 100 {
		t.Errorf("Breakdown.Keywords = %v, want 100 for mixed-case matches", kw.Breakdown.Keywords)
	}
}
