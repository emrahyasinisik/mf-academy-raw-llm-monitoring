package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
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
		activeChange    *float64
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
		activeChange = &v
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
			ActiveAdapter: ActiveAdapterBox{
				Name:         activeAdapter,
				ValidRate:    windowValidRate,
				PreviousRate: priorValidRate,
				ChangePoints: activeChange,
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

func (s *Store) latestConsistency(ctx context.Context) (*ConsistencyCard, error) {
	var (
		card     ConsistencyCard
		minTotal sql.NullFloat64
		maxTotal sql.NullFloat64
	)
	err := s.db.QueryRow(ctx, `
		WITH latest AS (
			SELECT trial_group
			  FROM assessments
			 WHERE trial_group IS NOT NULL
			 GROUP BY trial_group
			 ORDER BY max(created_at) DESC
			 LIMIT 1
		),
		summary AS (
			SELECT a.trial_group::text AS group_id,
			       count(*)::int AS runs,
			       min(a.created_at) AS created_at,
			       min(a.overall_score) AS min_total,
			       max(a.overall_score) AS max_total
			  FROM assessments a
			  JOIN latest l ON l.trial_group = a.trial_group
			 GROUP BY a.trial_group
		),
		volatile AS (
			SELECT f.finding->>'key' AS criterion,
			       stddev_samp(
			           ((f.finding->>'score')::double precision) /
			           CASE
			             WHEN coalesce((c.criterion->>'scale_max')::double precision, 0) <= 0 THEN 5
			             ELSE (c.criterion->>'scale_max')::double precision
			           END
			       ) AS stddev
			  FROM assessments a
			  JOIN latest l ON l.trial_group = a.trial_group
			  CROSS JOIN LATERAL jsonb_array_elements(a.findings) AS f(finding)
			  JOIN LATERAL jsonb_array_elements(a.criteria_snapshot) AS c(criterion)
			    ON c.criterion->>'key' = f.finding->>'key'
			 WHERE coalesce((f.finding->>'evidence_found')::boolean, false)
			   AND f.finding->>'score' IS NOT NULL
			 GROUP BY criterion
			HAVING count(*) >= 2
			 ORDER BY stddev DESC, criterion ASC
			 LIMIT 1
		)
		SELECT s.group_id, s.runs, s.created_at, s.min_total, s.max_total,
		       coalesce(v.criterion, ''), coalesce(v.stddev, 0)
		  FROM summary s
		  LEFT JOIN volatile v ON true
	`).Scan(
		&card.Group, &card.Runs, &card.CreatedAt, &minTotal, &maxTotal,
		&card.VolatileCriterion, &card.VolatileStdDev)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if minTotal.Valid {
		card.MinTotal = round2(minTotal.Float64)
	}
	if maxTotal.Valid {
		card.MaxTotal = round2(maxTotal.Float64)
	}
	card.TotalSpread = round2(card.MaxTotal - card.MinTotal)
	card.VolatileStdDev = round4(card.VolatileStdDev)
	return &card, nil
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
func matureWeeks(weekStart int64, now time.Time) int {
	elapsed := now.Unix() - weekStart
	if elapsed <= 0 {
		return 0
	}
	return int(elapsed / (7 * 24 * 3600))
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

func utcDayUnix(t time.Time) int64 {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix()
}
