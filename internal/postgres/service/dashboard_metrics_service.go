package service

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type DashboardMetrics struct {
	Database            string  `json:"database"`
	DBSizeBytes         int64   `json:"db_size_bytes"`
	ActiveConns         int64   `json:"active_connections"`
	IdleConns           int64   `json:"idle_connections"`
	CacheHitRatio       float64 `json:"cache_hit_ratio"`
	Deadlocks           int64   `json:"deadlocks"`
	BlockedSessions     int64   `json:"blocked_sessions"`
	TablesNeedingVacuum int64   `json:"tables_needing_vacuum"`

	StatStatementsEnabled bool           `json:"pg_stat_statements_enabled"`
	TopQueries            []TopQueryStat `json:"top_queries,omitempty"`
}

type TopQueryStat struct {
	Query           string  `json:"query"`
	Calls           int64   `json:"calls"`
	TotalTimeMs     float64 `json:"total_time_ms"`
	MeanTimeMs      float64 `json:"mean_time_ms"`
	Rows            int64   `json:"rows"`
	SharedBlksHit   int64   `json:"shared_blks_hit"`
	SharedBlksRead  int64   `json:"shared_blks_read"`
	TempBlksWritten int64   `json:"temp_blks_written"`
}

type DashboardMetricsService struct {
	instanceConn InstanceConnectionService
}

func NewDashboardMetricsService(instanceConn InstanceConnectionService) *DashboardMetricsService {
	return &DashboardMetricsService{instanceConn: instanceConn}
}

func (s *DashboardMetricsService) GetMetrics(ctx context.Context, userID, projectID uuid.UUID) (*DashboardMetrics, error) {
	pool, err := s.instanceConn.GetPool(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}

	// Basic DB stats.
	var (
		dbName                 string
		dbSize                 int64
		activeConns, idleConns int64
		blksHit, blksRead      int64
		deadlocks              int64
		blocked                int64
		tablesNeedVacuum       int64
		hasStatStatements      bool
	)

	if err := pool.QueryRow(ctx, `SELECT current_database(), pg_database_size(current_database())`).Scan(&dbName, &dbSize); err != nil {
		return nil, err
	}

	// Connection breakdown (active vs idle).
	if err := pool.QueryRow(ctx, `
		SELECT
		  COUNT(*) FILTER (WHERE state = 'active')::bigint AS active,
		  COUNT(*) FILTER (WHERE state = 'idle')::bigint AS idle
		FROM pg_stat_activity
		WHERE datname = current_database()
	`).Scan(&activeConns, &idleConns); err != nil {
		return nil, err
	}

	// Cache hit ratio (approx).
	if err := pool.QueryRow(ctx, `
		SELECT blks_hit::bigint, blks_read::bigint, deadlocks::bigint
		FROM pg_stat_database
		WHERE datname = current_database()
	`).Scan(&blksHit, &blksRead, &deadlocks); err != nil {
		return nil, err
	}

	// Blocked sessions snapshot (best-effort).
	_ = pool.QueryRow(ctx, `
		SELECT COUNT(*)::bigint
		FROM pg_stat_activity a
		WHERE a.datname = current_database()
		  AND a.wait_event_type = 'Lock'
	`).Scan(&blocked)

	// Vacuum heuristic: tables with a notable number of dead tuples.
	_ = pool.QueryRow(ctx, `
		SELECT COUNT(*)::bigint
		FROM pg_stat_user_tables
		WHERE n_dead_tup > 1000
	`).Scan(&tablesNeedVacuum)

	ratio := 0.0
	den := float64(blksHit + blksRead)
	if den > 0 {
		ratio = float64(blksHit) / den
	}

	// pg_stat_statements: detect + query top statements.
	_ = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements')`).Scan(&hasStatStatements)

	metrics := &DashboardMetrics{
		Database:              dbName,
		DBSizeBytes:           dbSize,
		ActiveConns:           activeConns,
		IdleConns:             idleConns,
		CacheHitRatio:         ratio,
		Deadlocks:             deadlocks,
		BlockedSessions:       blocked,
		TablesNeedingVacuum:   tablesNeedVacuum,
		StatStatementsEnabled: hasStatStatements,
	}

	if hasStatStatements {
		// Small timeout so dashboards don't hang.
		qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		rows, qerr := pool.Query(qctx, `
			SELECT query, calls::bigint, total_exec_time, mean_exec_time, rows::bigint,
			       shared_blks_hit::bigint, shared_blks_read::bigint, temp_blks_written::bigint
			FROM pg_stat_statements
			WHERE query <> '<insufficient privilege>'
			  AND dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
			  AND query NOT ILIKE '%pg_stat_%'
			  AND query NOT ILIKE '%current_database()%'
			  AND query NOT ILIKE '%information_schema%'
			  AND query NOT ILIKE '%pg_class%'
			  AND query NOT ILIKE '%pg_attribute%'
			ORDER BY total_exec_time DESC
			LIMIT 10
		`)
		if qerr == nil {
			defer rows.Close()
			var out []TopQueryStat
			for rows.Next() {
				var t TopQueryStat
				if err := rows.Scan(&t.Query, &t.Calls, &t.TotalTimeMs, &t.MeanTimeMs, &t.Rows, &t.SharedBlksHit, &t.SharedBlksRead, &t.TempBlksWritten); err != nil {
					// If anything goes wrong, just stop returning top queries.
					out = nil
					break
				}
				out = append(out, t)
			}
			if rows.Err() == nil {
				metrics.TopQueries = out
			}
		}
	}

	return metrics, nil
}
