package llm

import (
	"fmt"
	"strings"
)

// Weights control how much each component contributes to the final decision
// score. They sum to 1.0. Making them configurable (per request) turns the
// scorer into a transparent, tunable rule engine rather than a black box.
type Weights struct {
	Completion float64 `json:"completion"` // did it actually answer?
	Latency    float64 `json:"latency"`    // was it fast enough?
	Efficiency float64 `json:"efficiency"` // tokens spent vs. output produced
	Keywords   float64 `json:"keywords"`   // did it cover expected content?
	Length     float64 `json:"length"`     // is the answer an adequate length?
}

// DefaultWeights is a balanced profile emphasising that the model actually
// answered and covered the expected content.
func DefaultWeights() Weights {
	return Weights{
		Completion: 0.30,
		Latency:    0.15,
		Efficiency: 0.15,
		Keywords:   0.25,
		Length:     0.15,
	}
}

// Thresholds used by the rule engine. Extracted as constants so the scoring
// policy is readable in one place instead of buried as magic numbers.
const (
	goodLatencyMs = 1500  // at/under this, latency scores 100
	badLatencyMs  = 20000 // at/over this, latency scores 0
	minGoodChars  = 40    // shorter answers are penalised
	maxGoodChars  = 4000  // longer answers get diminishing returns
)

// refusalMarkers are phrases that suggest the model declined to answer.
var refusalMarkers = []string{
	"i cannot", "i can't", "i'm sorry", "i am sorry",
	"as an ai", "unable to", "i won't", "i will not",
}

// ScoreRun applies the rule-based decision scoring to a run and returns a
// Score with a transparent per-component breakdown and a human rationale.
func ScoreRun(run Run, w Weights) Score {
	completion := scoreCompletion(run.Response)
	latency := scoreLatency(run.LatencyMs)
	efficiency := scoreEfficiency(run.CompletionTokens, run.Response)
	keywords := scoreKeywords(run.Response, run.ExpectedKeywords)
	length := scoreLength(run.Response)

	breakdown := map[string]float64{
		"completion": round1(completion),
		"latency":    round1(latency),
		"efficiency": round1(efficiency),
		"keywords":   round1(keywords),
		"length":     round1(length),
	}

	total := completion*w.Completion +
		latency*w.Latency +
		efficiency*w.Efficiency +
		keywords*w.Keywords +
		length*w.Length

	total = clamp(total, 0, 100)

	return Score{
		RunID:     run.ID,
		Score:     round1(total),
		Grade:     grade(total),
		Breakdown: breakdown,
		Rationale: rationale(run, breakdown),
	}
}

// scoreCompletion: 100 if a substantive answer, low if empty or a refusal.
func scoreCompletion(response string) float64 {
	trimmed := strings.TrimSpace(response)
	if trimmed == "" {
		return 0
	}
	lower := strings.ToLower(trimmed)
	for _, m := range refusalMarkers {
		if strings.Contains(lower, m) {
			return 30 // answered, but looks like a refusal/hedge
		}
	}
	return 100
}

// scoreLatency: linear from 100 (fast) down to 0 (slow) between thresholds.
func scoreLatency(latencyMs int) float64 {
	if latencyMs <= goodLatencyMs {
		return 100
	}
	if latencyMs >= badLatencyMs {
		return 0
	}
	span := float64(badLatencyMs - goodLatencyMs)
	over := float64(latencyMs - goodLatencyMs)
	return 100 * (1 - over/span)
}

// scoreEfficiency: reward producing meaningful text per completion token.
// ~4 characters per token is typical; far below that means wasted tokens.
func scoreEfficiency(completionTokens int, response string) float64 {
	chars := len(strings.TrimSpace(response))
	if completionTokens <= 0 {
		// No token accounting available — neutral score.
		return 70
	}
	if chars == 0 {
		return 0
	}
	charsPerToken := float64(chars) / float64(completionTokens)
	// Map [1.5 .. 4.0] chars/token onto [40 .. 100].
	switch {
	case charsPerToken >= 4.0:
		return 100
	case charsPerToken <= 1.5:
		return 40
	default:
		return 40 + (charsPerToken-1.5)/(4.0-1.5)*60
	}
}

// scoreKeywords: fraction of expected keywords present in the response.
// With no expected keywords, this dimension is neutral (does not punish).
func scoreKeywords(response string, expected []string) float64 {
	if len(expected) == 0 {
		return 100
	}
	lower := strings.ToLower(response)
	found := 0
	for _, kw := range expected {
		if kw = strings.ToLower(strings.TrimSpace(kw)); kw != "" && strings.Contains(lower, kw) {
			found++
		}
	}
	return 100 * float64(found) / float64(len(expected))
}

// scoreLength: penalise answers that are too short or excessively long.
func scoreLength(response string) float64 {
	n := len(strings.TrimSpace(response))
	switch {
	case n == 0:
		return 0
	case n < minGoodChars:
		return 100 * float64(n) / float64(minGoodChars)
	case n <= maxGoodChars:
		return 100
	default:
		// Gentle decay beyond the ideal ceiling, floored at 60.
		over := float64(n - maxGoodChars)
		return clamp(100-over/200, 60, 100)
	}
}

// grade converts a 0..100 score into a letter grade.
func grade(score float64) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 80:
		return "B"
	case score >= 70:
		return "C"
	case score >= 60:
		return "D"
	default:
		return "F"
	}
}

// rationale produces a short human-readable explanation of the score.
func rationale(run Run, b map[string]float64) string {
	parts := []string{}
	if b["completion"] < 50 {
		parts = append(parts, "response was empty or looked like a refusal")
	}
	if b["keywords"] < 100 && len(run.ExpectedKeywords) > 0 {
		parts = append(parts, fmt.Sprintf("covered %.0f%% of expected keywords", b["keywords"]))
	}
	if b["latency"] < 60 {
		parts = append(parts, fmt.Sprintf("slow response (%dms)", run.LatencyMs))
	}
	if b["length"] < 60 {
		parts = append(parts, "answer length was suboptimal")
	}
	if len(parts) == 0 {
		return "Strong answer across completion, latency, efficiency, keyword coverage and length."
	}
	return "Deductions: " + strings.Join(parts, "; ") + "."
}

// ---- tiny numeric helpers ----

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func round1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}
