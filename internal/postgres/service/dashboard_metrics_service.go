package service

import (
	"backend/internal/postgres/infra"
	"context"

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
}

type DashboardMetricsService struct {
	instanceConn infra.InstanceConnectionService
}

func NewDashboardMetricsService(instanceConn infra.InstanceConnectionService) *DashboardMetricsService {
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

	metrics := &DashboardMetrics{
		Database:            dbName,
		DBSizeBytes:         dbSize,
		ActiveConns:         activeConns,
		IdleConns:           idleConns,
		CacheHitRatio:       ratio,
		Deadlocks:           deadlocks,
		BlockedSessions:     blocked,
		TablesNeedingVacuum: tablesNeedVacuum,
	}

	return metrics, nil
}
