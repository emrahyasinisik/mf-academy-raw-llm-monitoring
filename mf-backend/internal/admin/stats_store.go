package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/emrah/mf-backend/internal/analysis"
)

func (s *Store) Stats(ctx context.Context, from, to time.Time) (StatsResponse, error) {
	priorFrom := from.Add(-to.Sub(from))
	spine := daySpine(from, to)

	var (
		totalUsers      int64
		usersAtFrom     int64
		totalReports    int64
		reportsAtFrom   int64
		reportsLast24h  int64
		reportsPrev24h  int64
		activeAdapter   string
		windowReports   int64
		windowValidRate float64
		priorReports    int64
		priorValidRate  float64
		validityChange  *float64
		newUsers        = map[int64]int{}
		assessments     = map[int64]int{}
		schemaValid     = map[int64]int{}
		orgTypes        = []CategoryCount{}
		runsByTarget    = map[string]map[int64]int{}
		funnel          Funnel
		cohorts         = []CohortRow{}
		consistency     *ConsistencyCard
	)

	err := s.db.QueryRow(ctx, `
		SELECT
		    (SELECT count(*) FROM users),
		    (SELECT count(*) FROM users WHERE created_at < $1),
		    (SELECT count(*) FROM assessments),
		    (SELECT count(*) FROM assessments WHERE created_at < $1),
		    (SELECT count(*) FROM assessments WHERE created_at > now() - interval '24 hours'),
		    (SELECT count(*) FROM assessments
		       WHERE created_at > now() - interval '48 hours'
		         AND created_at <= now() - interval '24 hours'),
		    coalesce((SELECT a.name FROM llm_settings s
		                LEFT JOIN llm_adapters a ON a.id = s.active_adapter_id
		               WHERE s.id = 1), ''),
		    (SELECT count(*) FROM assessments WHERE created_at >= $1 AND created_at < $2),
		    (SELECT coalesce(avg(schema_valid::int), 0) FROM assessments
		       WHERE created_at >= $1 AND created_at < $2),
		    (SELECT count(*) FROM assessments WHERE created_at >= $3 AND created_at < $1),
		    (SELECT coalesce(avg(schema_valid::int), 0) FROM assessments
		       WHERE created_at >= $3 AND created_at < $1)
	`, from, to, priorFrom).Scan(
		&totalUsers, &usersAtFrom, &totalReports, &reportsAtFrom, &reportsLast24h,
		&reportsPrev24h, &activeAdapter, &windowReports, &windowValidRate,
		&priorReports, &priorValidRate)
	if err != nil {
		return StatsResponse{}, fmt.Errorf("read stat boxes: %w", err)
	}
	if windowReports > 0 && priorReports > 0 {
		v := round2((windowValidRate - priorValidRate) * 100)
		validityChange = &v
	}

	rows, err := s.db.Query(ctx, `
		SELECT date_trunc('day', created_at AT TIME ZONE 'UTC'), count(*)
		  FROM users WHERE created_at >= $1 AND created_at < $2 GROUP BY 1
	`, from, to)
	if err != nil {
		return StatsResponse{}, fmt.Errorf("read daily users: %w", err)
	}
	for rows.Next() {
		var (
			day   time.Time
			count int64
		)
		if err := rows.Scan(&day, &count); err != nil {
			rows.Close()
			return StatsResponse{}, fmt.Errorf("scan daily users: %w", err)
		}
		newUsers[utcDayUnix(day)] = int(count)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return StatsResponse{}, fmt.Errorf("read daily users: %w", err)
	}
	rows.Close()

	rows, err = s.db.Query(ctx, `
		SELECT date_trunc('day', created_at AT TIME ZONE 'UTC'),
		       count(*), count(*) FILTER (WHERE schema_valid)
		  FROM assessments WHERE created_at >= $1 AND created_at < $2 GROUP BY 1
	`, from, to)
	if err != nil {
		return StatsResponse{}, fmt.Errorf("read daily assessments: %w", err)
	}
	for rows.Next() {
		var (
			day        time.Time
			count      int64
			validCount int64
		)
		if err := rows.Scan(&day, &count, &validCount); err != nil {
			rows.Close()
			return StatsResponse{}, fmt.Errorf("scan daily assessments: %w", err)
		}
		t := utcDayUnix(day)
		assessments[t] = int(count)
		schemaValid[t] = int(validCount)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return StatsResponse{}, fmt.Errorf("read daily assessments: %w", err)
	}
	rows.Close()

	rows, err = s.db.Query(ctx, `SELECT type, count(*) FROM organizations GROUP BY 1 ORDER BY 1`)
	if err != nil {
		return StatsResponse{}, fmt.Errorf("read organization types: %w", err)
	}
	for rows.Next() {
		var (
			c     CategoryCount
			count int64
		)
		if err := rows.Scan(&c.Key, &count); err != nil {
			rows.Close()
			return StatsResponse{}, fmt.Errorf("scan organization types: %w", err)
		}
		c.Count = int(count)
		orgTypes = append(orgTypes, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return StatsResponse{}, fmt.Errorf("read organization types: %w", err)
	}
	rows.Close()

	rows, err = s.db.Query(ctx, `
		SELECT target, date_trunc('day', created_at AT TIME ZONE 'UTC'), count(*)
		  FROM llm_runs WHERE created_at >= $1 AND created_at < $2 GROUP BY 1, 2
	`, from, to)
	if err != nil {
		return StatsResponse{}, fmt.Errorf("read runs by target: %w", err)
	}
	for rows.Next() {
		var (
			target string
			day    time.Time
			count  int64
		)
		if err := rows.Scan(&target, &day, &count); err != nil {
			rows.Close()
			return StatsResponse{}, fmt.Errorf("scan runs by target: %w", err)
		}
		if runsByTarget[target] == nil {
			runsByTarget[target] = map[int64]int{}
		}
		runsByTarget[target][utcDayUnix(day)] = int(count)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return StatsResponse{}, fmt.Errorf("read runs by target: %w", err)
	}
	rows.Close()

	var registered, consented, analyzed int64
	err = s.db.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE u.terms_accepted_at IS NOT NULL),
		       count(*) FILTER (WHERE EXISTS (
		           SELECT 1 FROM assessments a WHERE a.user_id = u.id))
		  FROM users u WHERE u.created_at >= $1 AND u.created_at < $2
	`, from, to).Scan(&registered, &consented, &analyzed)
	if err != nil {
		return StatsResponse{}, fmt.Errorf("read activation funnel: %w", err)
	}
	funnel = Funnel{
		Registered: int(registered),
		Consented:  int(consented),
		Analyzed:   int(analyzed),
	}

	rows, err = s.db.Query(ctx, `
		SELECT date_trunc('week', u.created_at AT TIME ZONE 'UTC') AS w,
		       count(*),
		       count(*) FILTER (WHERE EXISTS (
		           SELECT 1 FROM assessments a WHERE a.user_id = u.id
		             AND a.created_at >= u.created_at + interval '7 days'
		             AND a.created_at <  u.created_at + interval '14 days')),
		       count(*) FILTER (WHERE EXISTS (
		           SELECT 1 FROM assessments a WHERE a.user_id = u.id
		             AND a.created_at >= u.created_at + interval '21 days'
		             AND a.created_at <  u.created_at + interval '28 days'))
		  FROM users u WHERE u.created_at >= $1 AND u.created_at < $2
		 GROUP BY 1 ORDER BY 1
	`, from, to)
	if err != nil {
		return StatsResponse{}, fmt.Errorf("read retention cohorts: %w", err)
	}
	now := time.Now().UTC()
	for rows.Next() {
		var (
			weekStart time.Time
			size      int64
			week2     int64
			week4     int64
		)
		if err := rows.Scan(&weekStart, &size, &week2, &week4); err != nil {
			rows.Close()
			return StatsResponse{}, fmt.Errorf("scan retention cohorts: %w", err)
		}
		t := utcDayUnix(weekStart)
		cohorts = append(cohorts, CohortRow{
			WeekStart:   t,
			Size:        int(size),
			Week2:       int(week2),
			Week4:       int(week4),
			MatureWeeks: matureWeeks(t, now),
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return StatsResponse{}, fmt.Errorf("read retention cohorts: %w", err)
	}
	rows.Close()

	consistency, err = s.latestConsistency(ctx)
	if err != nil {
		return StatsResponse{}, fmt.Errorf("read consistency card: %w", err)
	}

	return StatsResponse{
		Boxes: StatsBoxes{
			TotalUsers: StatBox{
				Value:     float64(totalUsers),
				Previous:  float64(usersAtFrom),
				ChangePct: changePct(float64(totalUsers), float64(usersAtFrom)),
			},
			TotalReports: StatBox{
				Value:     float64(totalReports),
				Previous:  float64(reportsAtFrom),
				ChangePct: changePct(float64(totalReports), float64(reportsAtFrom)),
			},
			ReportsLast24h: StatBox{
				Value:     float64(reportsLast24h),
				Previous:  float64(reportsPrev24h),
				ChangePct: changePct(float64(reportsLast24h), float64(reportsPrev24h)),
			},
			ActiveAdapter: ActiveAdapterBox{Name: activeAdapter},
			SchemaValidity: SchemaValidityBox{
				Rate:         windowValidRate,
				PreviousRate: priorValidRate,
				ChangePoints: validityChange,
			},
		},
		Days:         assembleDays(spine, int(usersAtFrom), newUsers, assessments, schemaValid),
		OrgTypes:     orgTypes,
		RunsByTarget: assembleTargets(spine, runsByTarget),
		Funnel:       funnel,
		Cohorts:      cohorts,
		Consistency:  consistency,
	}, nil
}

// latestConsistency reduces the most recent trial group to the card the sales
// material publishes, or returns nil when there is nothing honest to publish.
//
// The legs are fetched and reduced in Go rather than aggregated in SQL, and
// that is a reversal of the first implementation worth recording. The SQL used
// stddev_samp, which divides by n-1; analysis.PerCriterionStdDev divides by n,
// because a trial is every run there was, not a sample drawn from a larger
// population. On the default five-run trial the two differ by about 11.8%, so
// GET /analysis/trials/{group} and this panel published materially different
// numbers for the same measurement. Calling the one function is the only fix
// that cannot drift apart again — and it puts the tie-break somewhere a unit
// test can reach.
//
// One query, all legs. A group is capped at analysis.maxTrials (10) rows, so
// materialising them costs nothing, and the endpoint runs under a 5s
// REQUEST_TIMEOUT shared with its siblings — a query per leg would spend that
// budget on round trips.
func (s *Store) latestConsistency(ctx context.Context) (*ConsistencyCard, error) {
	rows, err := s.db.Query(ctx, `
		WITH latest AS (
			SELECT trial_group
			  FROM assessments
			 WHERE trial_group IS NOT NULL
			 GROUP BY trial_group
			 ORDER BY max(created_at) DESC
			 LIMIT 1
		)
		SELECT a.trial_group::text, a.created_at, a.overall_score, a.redacted_at,
		       a.criteria_snapshot, a.findings
		  FROM assessments a
		  JOIN latest l ON l.trial_group = a.trial_group
		 ORDER BY a.created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	legs := []trialLeg{}
	for rows.Next() {
		var (
			leg                      trialLeg
			redactedAt               *time.Time
			rawCriteria, rawFindings []byte
		)
		if err := rows.Scan(&leg.Group, &leg.CreatedAt, &leg.Score, &redactedAt,
			&rawCriteria, &rawFindings); err != nil {
			return nil, err
		}
		leg.Redacted = redactedAt != nil
		if err := json.Unmarshal(rawCriteria, &leg.Criteria); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(rawFindings, &leg.Findings); err != nil {
			return nil, err
		}
		legs = append(legs, leg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return consistencyCard(legs), nil
}

// trialLeg is one run of a consistency group, carrying only what the card is
// computed from.
type trialLeg struct {
	Group     string
	CreatedAt time.Time
	// Score is the leg's weighted total, nil when no criterion could be scored
	// at all. Nullable in the column for that reason, so it is nullable here.
	Score    *float64
	Redacted bool
	Criteria []analysis.Criterion
	Findings []analysis.Finding
}

// consistencyCard reduces a trial group to the published figure, or to nothing.
//
// Separated from the query so the two ways this can have no answer are covered
// by a test rather than by inspection — both used to produce a card reading
// "0 puan", and zero is the *best* possible consistency result.
func consistencyCard(legs []trialLeg) *ConsistencyCard {
	// A single run has no spread. Neither has a group whose every
	// overall_score is NULL, which is what a group looks like when nothing in
	// the case could be scored — min and max are then both absent and the old
	// max-min arithmetic quietly returned 0. Publishing a flawless figure for a
	// measurement that was never taken is the one failure this card cannot
	// afford, and the panel already renders the card's absence.
	if len(legs) < 2 {
		return nil
	}

	card := ConsistencyCard{
		// Oldest leg first, so this is the group's own timestamp. The rubric is
		// read from the same leg for the reason summarise() does: every run in
		// a group shares one snapshot.
		Group:     legs[0].Group,
		CreatedAt: legs[0].CreatedAt,
		Runs:      len(legs),
	}

	var lowTotal, topTotal float64
	scored := false
	runs := make([][]analysis.Finding, 0, len(legs))
	for _, leg := range legs {
		if leg.Redacted {
			card.RedactedRuns++
		}
		if leg.Score != nil {
			switch {
			case !scored:
				lowTotal, topTotal, scored = *leg.Score, *leg.Score, true
			case *leg.Score < lowTotal:
				lowTotal = *leg.Score
			case *leg.Score > topTotal:
				topTotal = *leg.Score
			}
		}
		runs = append(runs, leg.Findings)
	}
	if !scored {
		return nil
	}

	card.MinTotal, card.MaxTotal = round2(lowTotal), round2(topTotal)
	card.TotalSpread = round2(topTotal - lowTotal)
	card.VolatileCriterion, card.VolatileStdDev = mostVolatile(
		analysis.PerCriterionStdDev(legs[0].Criteria, runs))
	return &card
}

// mostVolatile picks the criterion that moved most, or nothing at all.
//
// Keys are sorted before the scan so a tie resolves the same way on every
// request: two criteria that moved identically would otherwise alternate in the
// card between one page load and the next, which reads as instability in the
// measurement rather than in the rubric. This is the ORDER BY stddev DESC,
// criterion ASC the SQL used to carry.
//
// An empty map gives ("", nil), never ("", 0) — see ConsistencyCard.
func mostVolatile(spread map[string]float64) (string, *float64) {
	if len(spread) == 0 {
		return "", nil
	}
	keys := make([]string, 0, len(spread))
	for key := range spread {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	best, highest := keys[0], spread[keys[0]]
	for _, key := range keys[1:] {
		if spread[key] > highest {
			best, highest = key, spread[key]
		}
	}
	return best, &highest
}

func daySpine(from, to time.Time) []int64 {
	spine := []int64{}
	for day := from.UTC(); day.Before(to.UTC()); day = day.Add(24 * time.Hour) {
		spine = append(spine, day.Unix())
	}
	return spine
}

func assembleDays(spine []int64, baseUsers int, newUsers, assessments, schemaValid map[int64]int) []DayPoint {
	days := []DayPoint{}
	cumulative := baseUsers
	for _, t := range spine {
		cumulative += newUsers[t]
		days = append(days, DayPoint{
			T:               t,
			NewUsers:        newUsers[t],
			CumulativeUsers: cumulative,
			Assessments:     assessments[t],
			SchemaValid:     schemaValid[t],
		})
	}
	return days
}

func changePct(current, previous float64) *float64 {
	if previous == 0 {
		return nil
	}
	v := round2((current - previous) / previous * 100)
	return &v
}

func assembleTargets(spine []int64, byTarget map[string]map[int64]int) []TargetSeries {
	targets := make([]string, 0, len(byTarget))
	for target := range byTarget {
		targets = append(targets, target)
	}
	sort.Strings(targets)

	series := []TargetSeries{}
	for _, target := range targets {
		points := make([]SeriesPoint, 0, len(spine))
		for _, t := range spine {
			points = append(points, SeriesPoint{T: t, V: float64(byTarget[target][t])})
		}
		series = append(series, TargetSeries{Target: target, Points: points})
	}
	return series
}

// Fully elapsed weeks decide which cohort retention cells can be read. A
// three-day-old cohort has no fourth-week retention yet, not 0% retention.
//
// Counted from the END of the cohort week, not its start, and the difference is
// up to six days of published churn that has not happened. The row is grouped by
// date_trunc('week', created_at) but retention is measured from each member's
// own created_at — the fourth-week window is created_at + 21..28 days. Somebody
// who signed up on the Sunday of a Monday-starting week only closes that window
// on weekStart + 34 days, so unlocking the cell at weekStart + 28 counted them
// as a non-returner while they still had six days to return. Adding the week's
// own width makes the cell wait for the last possible member.
func matureWeeks(weekStart int64, now time.Time) int {
	const week = int64(7 * 24 * 3600)
	elapsed := now.Unix() - (weekStart + week)
	if elapsed <= 0 {
		return 0
	}
	return int(elapsed / week)
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func utcDayUnix(t time.Time) int64 {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix()
}
