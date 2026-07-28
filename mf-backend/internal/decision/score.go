package decision

import "math"

// Dimension weights mirror build_persona_dataset.py — the fine-tune's ground
// truth. Keeping them here means the number shown in the UI is computed the
// same way the training set labels were, not whatever a small model feels like
// printing after "SKOR:".
var dimensionWeights = map[string]float64{
	"pazar":           0.25,
	"rekabet":         0.20,
	"moat":            0.20,
	"ekip_traction":   0.25,
	"risk":            0.10,
}

// scoreFromDimensions turns 0-5 per-dimension ratings into a 0-100 score and
// label. Dimensions with no evidence (score < 0) are excluded and do not
// penalise the result — same rule as the analysis engine's coverage logic.
func scoreFromDimensions(scores map[string]int) (int, string, float64) {
	var weighted, totalWeight float64
	scored := 0

	for key, w := range dimensionWeights {
		s, ok := scores[key]
		if !ok || s < 0 {
			continue
		}
		if s > 5 {
			s = 5
		}
		weighted += w * (float64(s) / 5.0)
		totalWeight += w
		scored++
	}

	if scored == 0 || totalWeight <= 0 {
		return 0, "Temkinli", 0
	}

	value := 100 * weighted / totalWeight
	score := int(math.Round(value))
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	coverage := totalWeight / sumWeights()
	return score, labelForScore(score), coverage
}

func sumWeights() float64 {
	var s float64
	for _, w := range dimensionWeights {
		s += w
	}
	return s
}

func labelForScore(score int) string {
	switch {
	case score >= 65:
		return "Yatırılabilir"
	case score >= 40:
		return "Temkinli"
	default:
		return "Yatırılamaz"
	}
}
