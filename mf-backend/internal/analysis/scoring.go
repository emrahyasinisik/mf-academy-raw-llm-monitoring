package analysis

import "math"

// This file is the arithmetic the model is not allowed to do.
//
// Given a rubric and the model's per-criterion findings, it produces the two
// numbers a report leads with. Both are pure functions of their inputs: same
// rubric plus same findings gives the same result on any machine, forever,
// which is what "reproducible" has to mean if a rejection is going to be
// defended with it.

// Score computes the overall rating and the coverage behind it.
//
// Two numbers rather than one, and this is the substantive decision in the
// package. A criterion with no evidence is *excluded* from the score and
// subtracted from coverage — it is not scored zero. Scoring silence as failure
// would mean a thin deck and a bad deck are indistinguishable, and would let a
// missing page decide a funding outcome.
//
// The consequence is that a score must always be read next to its coverage.
// 75 at 0.9 coverage is a strong case. 75 at 0.3 coverage is a case that is
// strong in the third of the rubric it bothered to address, and the honest
// reading of it is "come back when the rest is written".
//
// Returns nil when no criterion had evidence at all. Not zero: zero is a claim
// about the subject, nil is a statement about what could be determined.
func Score(criteria []Criterion, findings []Finding) (overall *float64, coverage float64) {
	if len(criteria) == 0 {
		return nil, 0
	}

	byKey := make(map[string]Finding, len(findings))
	for _, f := range findings {
		byKey[f.Key] = f
	}

	var (
		weightedSum  float64 // sum of weight * normalised score, over scored criteria
		scoredWeight float64 // total weight of criteria that had evidence
		totalWeight  float64 // total weight of the rubric
	)

	for _, c := range criteria {
		// A non-positive weight would either contribute nothing or, if
		// negative, let one criterion cancel another out. Skipped entirely so
		// a typo in a rubric cannot invert a result.
		if c.Weight <= 0 {
			continue
		}
		totalWeight += c.Weight

		f, ok := byKey[c.Key]
		if !ok || !f.EvidenceFound || f.Score == nil {
			continue
		}

		// Clamp rather than reject. The model occasionally returns 6 on a
		// 0-5 scale; treating that as "the maximum" loses less information
		// than discarding the finding, and the raw response is stored anyway
		// for anyone auditing how often it happens.
		max := float64(c.EffectiveScaleMax())
		s := math.Max(0, math.Min(*f.Score, max))

		weightedSum += c.Weight * (s / max)
		scoredWeight += c.Weight
	}

	if totalWeight <= 0 {
		return nil, 0
	}
	coverage = scoredWeight / totalWeight

	if scoredWeight <= 0 {
		return nil, coverage
	}

	// Renormalised by scoredWeight, not totalWeight. Dividing by the full
	// rubric weight would fold coverage into the score — an unaddressed
	// criterion would drag the number down exactly as a badly-addressed one
	// does, which is the conflation this whole design exists to avoid.
	value := 100 * weightedSum / scoredWeight

	// Guard against a rubric of NaN weights reaching the database, where the
	// CHECK constraint would reject the row and lose the work.
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, coverage
	}
	rounded := math.Round(value*100) / 100
	return &rounded, coverage
}

// Stats summarises a set of trial scores.
//
// Population standard deviation, not sample: these are every trial that was
// run, not a sample drawn from a larger population, so there is no degree of
// freedom to correct for. Using the sample form would inflate the figure for
// small trial counts, which is precisely the range this is used in.
func Stats(values []float64) (mean, stddev, min, max float64, ok bool) {
	if len(values) == 0 {
		return 0, 0, 0, 0, false
	}
	min, max = values[0], values[0]
	var sum float64
	for _, v := range values {
		sum += v
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	mean = sum / float64(len(values))

	var sq float64
	for _, v := range values {
		d := v - mean
		sq += d * d
	}
	stddev = math.Sqrt(sq / float64(len(values)))
	return mean, stddev, min, max, true
}

// PerCriterionStdDev reports how much each criterion's rating moved across
// trials, normalised to its own 0..1 scale so criteria with different scale
// maxima are comparable.
//
// This is the diagnostic that makes an unstable rubric fixable. An overall
// stddev of 8 points says "something is unstable"; this says which criterion,
// and therefore whose guidance text needs rewriting. Criteria that were never
// scored are absent rather than reported as 0 — a criterion nobody could
// assess is not a stable one.
func PerCriterionStdDev(criteria []Criterion, runs [][]Finding) map[string]float64 {
	scaleByKey := make(map[string]float64, len(criteria))
	for _, c := range criteria {
		scaleByKey[c.Key] = float64(c.EffectiveScaleMax())
	}

	collected := make(map[string][]float64, len(criteria))
	for _, findings := range runs {
		for _, f := range findings {
			if !f.EvidenceFound || f.Score == nil {
				continue
			}
			scale, known := scaleByKey[f.Key]
			if !known || scale <= 0 {
				continue
			}
			collected[f.Key] = append(collected[f.Key], *f.Score/scale)
		}
	}

	out := make(map[string]float64, len(collected))
	for key, values := range collected {
		// A single observation has no spread to report. Emitting 0 would read
		// as "perfectly stable" for something measured once.
		if len(values) < 2 {
			continue
		}
		_, sd, _, _, _ := Stats(values)
		out[key] = math.Round(sd*10000) / 10000
	}
	return out
}
