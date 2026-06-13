package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	pgservice "backend/internal/postgres/service"
	"backend/internal/service"
	"backend/internal/testutil"
	"backend/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockDashboardOverviewService struct {
	overview *pgservice.DashboardOverview
	err      error
}

func (m mockDashboardOverviewService) GetOverview(ctx context.Context, userID, projectID uuid.UUID) (*pgservice.DashboardOverview, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.overview, nil
}

type mockDashboardMetricsService struct {
	metrics *pgservice.DashboardMetrics
	err     error
}

func (m mockDashboardMetricsService) GetMetrics(ctx context.Context, userID, projectID uuid.UUID) (*pgservice.DashboardMetrics, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.metrics, nil
}

func TestDashboardHandler(t *testing.T) {
	type endpointCase struct {
		name       string
		userID     uuid.UUID
		projectID  string
		serviceErr error
		wantStatus int
	}

	commonCases := []endpointCase{
		{
			name:       "unauthorized",
			projectID:  uuid.New().String(),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid projectId",
			userID:     uuid.New(),
			projectID:  "not-a-uuid",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "project not accessible",
			userID:     uuid.New(),
			projectID:  uuid.New().String(),
			serviceErr: service.ErrProjectNotAccessible,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "no running instance",
			userID:     uuid.New(),
			projectID:  uuid.New().String(),
			serviceErr: service.ErrNoRunningInstance,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "internal error",
			userID:     uuid.New(),
			projectID:  uuid.New().String(),
			serviceErr: errors.New("db down"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	t.Run("GetOverview", func(t *testing.T) {
		for _, tt := range commonCases {
			t.Run(tt.name, func(t *testing.T) {
				h := NewDashboardHandler(mockDashboardOverviewService{err: tt.serviceErr}, nil)
				c, w := newDashboardHandlerContext(http.MethodGet, "/projects/"+tt.projectID+"/dashboard/overview", tt.userID, tt.projectID)

				h.GetOverview(c)

				assert.Equal(t, tt.wantStatus, w.Code)
			})
		}

		t.Run("success", func(t *testing.T) {
			projectID := uuid.New()
			overview := &pgservice.DashboardOverview{
				DB: &pgservice.DashboardDBInfo{
					Database:       "app_db",
					ServerVersion:  "PostgreSQL 16.0",
					PostmasterUpS:  3600,
					PingTimeMs:     12,
					NowRFC3339:     "2026-06-12T15:30:00+03",
					IsInRecovery:   false,
					HasStatStmtExt: true,
				},
				SchemaSummary: &pgservice.DashboardSchemaSummary{
					TotalTables:      3,
					TotalColumns:     12,
					TotalPrimaryKeys: 2,
				},
			}
			h := NewDashboardHandler(mockDashboardOverviewService{overview: overview}, nil)
			c, w := newDashboardHandlerContext(http.MethodGet, "/projects/"+projectID.String()+"/dashboard/overview", uuid.New(), projectID.String())

			h.GetOverview(c)

			assert.Equal(t, http.StatusOK, w.Code)
			var body map[string]any
			require.NoError(t, testutil.ParseJSONResponse(w, &body))
			data := body["data"].(map[string]any)
			db := data["db"].(map[string]any)
			summary := data["schema_summary"].(map[string]any)
			assert.Equal(t, "app_db", db["database"])
			assert.Equal(t, "PostgreSQL 16.0", db["server_version"])
			assert.Equal(t, float64(3), summary["total_tables"])
			assert.Equal(t, float64(12), summary["total_columns"])
			assert.Equal(t, float64(2), summary["total_primary_keys"])
		})
	})

	t.Run("GetMetrics", func(t *testing.T) {
		for _, tt := range commonCases {
			t.Run(tt.name, func(t *testing.T) {
				h := NewDashboardHandler(nil, mockDashboardMetricsService{err: tt.serviceErr})
				c, w := newDashboardHandlerContext(http.MethodGet, "/projects/"+tt.projectID+"/dashboard/metrics", tt.userID, tt.projectID)

				h.GetMetrics(c)

				assert.Equal(t, tt.wantStatus, w.Code)
			})
		}

		t.Run("success", func(t *testing.T) {
			projectID := uuid.New()
			metrics := &pgservice.DashboardMetrics{
				Database:            "app_db",
				DBSizeBytes:         2048,
				ActiveConns:         4,
				IdleConns:           2,
				CacheHitRatio:       0.9,
				Deadlocks:           1,
				BlockedSessions:     5,
				TablesNeedingVacuum: 7,
			}
			h := NewDashboardHandler(nil, mockDashboardMetricsService{metrics: metrics})
			c, w := newDashboardHandlerContext(http.MethodGet, "/projects/"+projectID.String()+"/dashboard/metrics", uuid.New(), projectID.String())

			h.GetMetrics(c)

			assert.Equal(t, http.StatusOK, w.Code)
			var body map[string]any
			require.NoError(t, testutil.ParseJSONResponse(w, &body))
			data := body["data"].(map[string]any)
			assert.Equal(t, "app_db", data["database"])
			assert.Equal(t, float64(2048), data["db_size_bytes"])
			assert.Equal(t, float64(4), data["active_connections"])
			assert.Equal(t, float64(2), data["idle_connections"])
			assert.InEpsilon(t, 0.9, data["cache_hit_ratio"], 0.001)
			assert.Equal(t, float64(1), data["deadlocks"])
			assert.Equal(t, float64(5), data["blocked_sessions"])
			assert.Equal(t, float64(7), data["tables_needing_vacuum"])
		})
	})
}

func newDashboardHandlerContext(method, path string, userID uuid.UUID, projectID string) (*gin.Context, *httptest.ResponseRecorder) {
	c, w := testutil.NewGinContext(method, path, nil, nil)
	if userID != uuid.Nil {
		c.Set(utils.UserIDContextKey, userID)
	}
	c.Params = gin.Params{{Key: "id", Value: projectID}}
	return c, w
}
