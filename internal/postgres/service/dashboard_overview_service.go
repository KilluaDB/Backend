package service

import (
	"backend/internal/postgres/infra"
	"backend/internal/repositories"
	"context"
	"time"

	"github.com/google/uuid"
)

type DashboardOverview struct {
	Instance      *DashboardInstanceInfo  `json:"instance,omitempty"`
	DB            *DashboardDBInfo        `json:"db,omitempty"`
	SchemaSummary *DashboardSchemaSummary `json:"schema_summary,omitempty"`
}

type DashboardInstanceInfo struct {
	ID        uuid.UUID  `json:"id"`
	Status    string     `json:"status"`
	Host      *string    `json:"host,omitempty"`
	Port      int        `json:"port,omitempty"`
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
}

type DashboardDBInfo struct {
	Database       string `json:"database"`
	ServerVersion  string `json:"server_version"`
	PostmasterUpS  int64  `json:"postmaster_uptime_s"`
	PingTimeMs     int64  `json:"ping_time_ms"`
	NowRFC3339     string `json:"now_rfc3339"`
	IsInRecovery   bool   `json:"is_in_recovery"`
	HasStatStmtExt bool   `json:"has_pg_stat_statements"`
}

type DashboardSchemaSummary struct {
	TotalTables      int64 `json:"total_tables"`
	TotalColumns     int64 `json:"total_columns"`
	TotalPrimaryKeys int64 `json:"total_primary_keys"`
}

type DashboardOverviewService struct {
	instanceConn infra.InstanceConnectionService
	projectRepo  repositories.ProjectRepository
}

func NewDashboardOverviewService(instanceConn infra.InstanceConnectionService, instanceRepo repositories.ProjectRepository) *DashboardOverviewService {
	return &DashboardOverviewService{instanceConn: instanceConn, projectRepo: instanceRepo}
}

func (s *DashboardOverviewService) GetOverview(ctx context.Context, userID, projectID uuid.UUID) (*DashboardOverview, error) {
	project, _ := s.projectRepo.GetByID(ctx, projectID)
	var instInfo *DashboardInstanceInfo
	if project != nil {
		instInfo = &DashboardInstanceInfo{
			ID:        project.ID,
			Status:    project.Status,
			Host:      nil,
			CreatedAt: project.RuntimeCreatedAt,
			UpdatedAt: project.RuntimeUpdatedAt,
		}
	}

	start := time.Now()
	pool, err := s.instanceConn.GetPool(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}

	var (
		database      string
		version       string
		nowStr        string
		uptimeSeconds int64
		inRecovery    bool
		hasExt        bool
		totalTables   int64
		totalColumns  int64
		totalPKs      int64
	)

	// Keep overview queries small and fast.
	if err := pool.QueryRow(ctx, `SELECT current_database(), version(), to_char(now(), 'YYYY-MM-DD"T"HH24:MI:SSOF')`).Scan(&database, &version, &nowStr); err != nil {
		return nil, err
	}
	if err := pool.QueryRow(ctx, `SELECT EXTRACT(EPOCH FROM (now() - pg_postmaster_start_time()))::bigint, pg_is_in_recovery()`).Scan(&uptimeSeconds, &inRecovery); err != nil {
		return nil, err
	}
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements')`).Scan(&hasExt); err != nil {
		// Extension catalog should exist; if not, treat as absent.
		hasExt = false
	}
	if err := pool.QueryRow(ctx, `
		WITH user_tables AS (
			SELECT t.table_schema, t.table_name
			FROM information_schema.tables t
			WHERE t.table_type = 'BASE TABLE'
			  AND t.table_schema = 'public'
		)
		SELECT
			(SELECT COUNT(*)::bigint
			 FROM user_tables) AS total_tables,
			(SELECT COUNT(*)::bigint
			 FROM information_schema.columns c
			 JOIN user_tables ut
			   ON ut.table_schema = c.table_schema
			  AND ut.table_name = c.table_name) AS total_columns,
			(SELECT COUNT(*)::bigint
			 FROM information_schema.table_constraints tc
			 JOIN user_tables ut
			   ON ut.table_schema = tc.table_schema
			  AND ut.table_name = tc.table_name
			 WHERE tc.constraint_type = 'PRIMARY KEY'
			) AS total_primary_keys
	`).Scan(&totalTables, &totalColumns, &totalPKs); err != nil {
		return nil, err
	}

	pingMs := time.Since(start).Milliseconds()

	return &DashboardOverview{
		Instance: instInfo,
		DB: &DashboardDBInfo{
			Database:       database,
			ServerVersion:  version,
			PostmasterUpS:  uptimeSeconds,
			PingTimeMs:     pingMs,
			NowRFC3339:     nowStr,
			IsInRecovery:   inRecovery,
			HasStatStmtExt: hasExt,
		},
		SchemaSummary: &DashboardSchemaSummary{
			TotalTables:      totalTables,
			TotalColumns:     totalColumns,
			TotalPrimaryKeys: totalPKs,
		},
	}, nil
}
