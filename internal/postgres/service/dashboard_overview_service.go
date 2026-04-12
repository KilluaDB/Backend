package service

import (
	"backend/internal/repositories"
	"context"
	"time"

	"github.com/google/uuid"
)

type DashboardOverview struct {
	Instance *DashboardInstanceInfo `json:"instance,omitempty"`
	DB       *DashboardDBInfo       `json:"db,omitempty"`
}

type DashboardInstanceInfo struct {
	ID        uuid.UUID `json:"id"`
	Status    string    `json:"status"`
	Host      *string   `json:"host,omitempty"`
	Port      *int      `json:"port,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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

type DashboardOverviewService struct {
	instanceConn InstanceConnectionService
	instanceRepo *repositories.DatabaseInstanceRepository
}

func NewDashboardOverviewService(instanceConn InstanceConnectionService, instanceRepo *repositories.DatabaseInstanceRepository) *DashboardOverviewService {
	return &DashboardOverviewService{instanceConn: instanceConn, instanceRepo: instanceRepo}
}

func (s *DashboardOverviewService) GetOverview(ctx context.Context, userID, projectID uuid.UUID) (*DashboardOverview, error) {
	// Resolve meta instance info (best-effort; dashboard still works if missing).
	inst, _ := s.instanceRepo.GetByProjectID(projectID)
	var instInfo *DashboardInstanceInfo
	if inst != nil {
		instInfo = &DashboardInstanceInfo{
			ID:        inst.ID,
			Status:    inst.Status,
			Host:      inst.Host,
			Port:      inst.Port,
			CreatedAt: inst.CreatedAt,
			UpdatedAt: inst.UpdatedAt,
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
	}, nil
}
