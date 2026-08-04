package org

import (
	"context"
	"fmt"
	"time"
)

// Stats loads org-scoped usage for [from, to). Member ids come from
// users.org_id = orgID; assessments and llm_runs are filtered through that set.
// No Prometheus — the company panel must stay populated when the GPU box is off.
func (s *Store) Stats(ctx context.Context, orgID string, from, to time.Time) (OrgStats, error) {
	priorFrom := from.Add(-to.Sub(from))
	spine := daySpine(from, to)

	var (
		memberCount    int
		seatLimit      int
		reportsLast24h int64
		reportsPrev24h int64
		windowReports  int64
		priorReports   int64
		windowValidAvg float64
	)

	err := s.db.QueryRow(ctx, `
		SELECT
		    (SELECT count(*)::int FROM users WHERE org_id = $1),
		    (SELECT seat_limit FROM organizations WHERE id = $1),
		    (SELECT count(*) FROM assessments a
		       JOIN users u ON u.id = a.user_id
		      WHERE u.org_id = $1 AND a.created_at > now() - interval '24 hours'),
		    (SELECT count(*) FROM assessments a
		       JOIN users u ON u.id = a.user_id
		      WHERE u.org_id = $1
		        AND a.created_at > now() - interval '48 hours'
		        AND a.created_at <= now() - interval '24 hours'),
		    (SELECT count(*) FROM assessments a
		       JOIN users u ON u.id = a.user_id
		      WHERE u.org_id = $1 AND a.created_at >= $2 AND a.created_at < $3),
		    (SELECT count(*) FROM assessments a
		       JOIN users u ON u.id = a.user_id
		      WHERE u.org_id = $1 AND a.created_at >= $4 AND a.created_at < $2),
		    (SELECT coalesce(avg(a.schema_valid::int), 0) FROM assessments a
		       JOIN users u ON u.id = a.user_id
		      WHERE u.org_id = $1 AND a.created_at >= $2 AND a.created_at < $3)
	`, orgID, from, to, priorFrom).Scan(
		&memberCount, &seatLimit, &reportsLast24h, &reportsPrev24h,
		&windowReports, &priorReports, &windowValidAvg,
	)
	if err != nil {
		return OrgStats{}, fmt.Errorf("read org stat boxes: %w", err)
	}

	assessments := map[int64]int{}
	schemaValid := map[int64]int{}
	rows, err := s.db.Query(ctx, `
		SELECT date_trunc('day', a.created_at AT TIME ZONE 'UTC'),
		       count(*), count(*) FILTER (WHERE a.schema_valid)
		  FROM assessments a
		  JOIN users u ON u.id = a.user_id
		 WHERE u.org_id = $1 AND a.created_at >= $2 AND a.created_at < $3
		 GROUP BY 1
	`, orgID, from, to)
	if err != nil {
		return OrgStats{}, fmt.Errorf("read org daily assessments: %w", err)
	}
	for rows.Next() {
		var (
			day        time.Time
			count      int64
			validCount int64
		)
		if err := rows.Scan(&day, &count, &validCount); err != nil {
			rows.Close()
			return OrgStats{}, fmt.Errorf("scan org daily assessments: %w", err)
		}
		t := utcDayUnix(day)
		assessments[t] = int(count)
		schemaValid[t] = int(validCount)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return OrgStats{}, fmt.Errorf("read org daily assessments: %w", err)
	}
	rows.Close()

	runsByTarget := map[string]map[int64]int{}
	rows, err = s.db.Query(ctx, `
		SELECT r.target, date_trunc('day', r.created_at AT TIME ZONE 'UTC'), count(*)
		  FROM llm_runs r
		  JOIN users u ON u.id = r.user_id
		 WHERE u.org_id = $1 AND r.created_at >= $2 AND r.created_at < $3
		 GROUP BY 1, 2
	`, orgID, from, to)
	if err != nil {
		return OrgStats{}, fmt.Errorf("read org runs by target: %w", err)
	}
	for rows.Next() {
		var (
			target string
			day    time.Time
			count  int64
		)
		if err := rows.Scan(&target, &day, &count); err != nil {
			rows.Close()
			return OrgStats{}, fmt.Errorf("scan org runs by target: %w", err)
		}
		if runsByTarget[target] == nil {
			runsByTarget[target] = map[int64]int{}
		}
		runsByTarget[target][utcDayUnix(day)] = int(count)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return OrgStats{}, fmt.Errorf("read org runs by target: %w", err)
	}
	rows.Close()

	memberActivity := []MemberAct{}
	rows, err = s.db.Query(ctx, `
		SELECT u.id, u.name,
		       count(a.id)::int,
		       max(a.created_at)
		  FROM users u
		  LEFT JOIN assessments a
		    ON a.user_id = u.id
		   AND a.created_at >= $2 AND a.created_at < $3
		 WHERE u.org_id = $1
		 GROUP BY u.id
		 ORDER BY count(a.id) DESC, u.name
	`, orgID, from, to)
	if err != nil {
		return OrgStats{}, fmt.Errorf("read org member activity: %w", err)
	}
	for rows.Next() {
		var m MemberAct
		if err := rows.Scan(&m.UserID, &m.Name, &m.Count, &m.LastAt); err != nil {
			rows.Close()
			return OrgStats{}, fmt.Errorf("scan org member activity: %w", err)
		}
		memberActivity = append(memberActivity, m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return OrgStats{}, fmt.Errorf("read org member activity: %w", err)
	}
	rows.Close()

	return OrgStats{
		Boxes: OrgStatsBoxes{
			Members: MemberSeatBox{Count: memberCount, SeatLimit: seatLimit},
			ReportsLast24h: StatBox{
				Value:     float64(reportsLast24h),
				Previous:  float64(reportsPrev24h),
				ChangePct: changePct(float64(reportsLast24h), float64(reportsPrev24h)),
			},
			ReportsWindow: StatBox{
				Value:     float64(windowReports),
				Previous:  float64(priorReports),
				ChangePct: changePct(float64(windowReports), float64(priorReports)),
			},
			SchemaValidity: SchemaBox{Rate: windowValidAvg},
		},
		AssessmentsPerDay: assembleDaySeries(spine, assessments),
		SchemaValidPerDay: assembleDaySeries(spine, schemaValid),
		RunsByTarget:      assembleTargets(spine, runsByTarget),
		MemberActivity:    memberActivity,
	}, nil
}
