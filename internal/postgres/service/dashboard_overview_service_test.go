package service

import (
	"backend/internal/mocks"
	"backend/internal/repository"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testProjectRepoAdapter struct {
	*mocks.ProjectStore
}

func testProjectRepoFromMock(store *mocks.ProjectStore) repository.ProjectStore {
	return testProjectRepoAdapter{ProjectStore: store}
}

func TestDashboardOverviewService_GetOverview(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	projectID := uuid.New()

	newServiceWithMock := func(t *testing.T, mock pgxmock.PgxPoolIface, projectRepo repository.ProjectStore) *DashboardOverviewService {
		t.Helper()
		svc := NewDashboardOverviewService(stubInstanceConn{}, projectRepo)
		svc.runnerSource = mockRunnerSource{runner: mock}
		return svc
	}

	expectDBInfoQuery := func(mock pgxmock.PgxPoolIface) {
		mock.ExpectQuery(`current_database\(\).*version\(\)`).
			WillReturnRows(pgxmock.NewRows([]string{"current_database", "version", "to_char"}).
				AddRow("app_db", "PostgreSQL 16.0", "2026-06-12T15:30:00+03"))
	}

	expectUptimeQuery := func(mock pgxmock.PgxPoolIface) {
		mock.ExpectQuery(`EXTRACT.*pg_postmaster_start_time`).
			WillReturnRows(pgxmock.NewRows([]string{"uptime", "pg_is_in_recovery"}).
				AddRow(int64(3600), false))
	}

	expectExtensionQuery := func(mock pgxmock.PgxPoolIface, hasExt bool) {
		mock.ExpectQuery(`pg_extension.*pg_stat_statements`).
			WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(hasExt))
	}

	expectSchemaSummaryQuery := func(mock pgxmock.PgxPoolIface) {
		mock.ExpectQuery(`information_schema.tables`).
			WillReturnRows(pgxmock.NewRows([]string{"total_tables", "total_columns", "total_primary_keys"}).
				AddRow(int64(3), int64(12), int64(2)))
	}

	t.Run("pool error", func(t *testing.T) {
		svc := NewDashboardOverviewService(stubInstanceConn{}, nil)

		got, err := svc.GetOverview(ctx, userID, projectID)

		require.Error(t, err)
		assert.Nil(t, got)
	})

	t.Run("first query error", func(t *testing.T) {
		mock := newMockPool(t)
		mock.ExpectQuery(`current_database\(\).*version\(\)`).
			WillReturnError(errors.New("db info failed"))
		svc := newServiceWithMock(t, mock, nil)

		got, err := svc.GetOverview(ctx, userID, projectID)

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "db info failed")
	})

	t.Run("second query error", func(t *testing.T) {
		mock := newMockPool(t)
		expectDBInfoQuery(mock)
		mock.ExpectQuery(`EXTRACT.*pg_postmaster_start_time`).
			WillReturnError(errors.New("uptime failed"))
		svc := newServiceWithMock(t, mock, nil)

		got, err := svc.GetOverview(ctx, userID, projectID)

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "uptime failed")
	})

	t.Run("third query error", func(t *testing.T) {
		mock := newMockPool(t)
		expectDBInfoQuery(mock)
		expectUptimeQuery(mock)
		mock.ExpectQuery(`pg_extension.*pg_stat_statements`).
			WillReturnError(errors.New("extension lookup failed"))
		expectSchemaSummaryQuery(mock)
		svc := newServiceWithMock(t, mock, nil)

		got, err := svc.GetOverview(ctx, userID, projectID)

		require.NoError(t, err)
		require.NotNil(t, got)
		require.NotNil(t, got.DB)
		assert.False(t, got.DB.HasStatStmtExt)
	})

	t.Run("fourth query error", func(t *testing.T) {
		mock := newMockPool(t)
		expectDBInfoQuery(mock)
		expectUptimeQuery(mock)
		expectExtensionQuery(mock, true)
		mock.ExpectQuery(`information_schema.tables`).
			WillReturnError(errors.New("schema summary failed"))
		svc := newServiceWithMock(t, mock, nil)

		got, err := svc.GetOverview(ctx, userID, projectID)

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "schema summary failed")
	})

	t.Run("success with nil project", func(t *testing.T) {
		mock := newMockPool(t)
		expectDBInfoQuery(mock)
		expectUptimeQuery(mock)
		expectExtensionQuery(mock, true)
		expectSchemaSummaryQuery(mock)
		svc := newServiceWithMock(t, mock, nil)

		got, err := svc.GetOverview(ctx, userID, projectID)

		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Nil(t, got.Instance)
		require.NotNil(t, got.DB)
		assert.Equal(t, "app_db", got.DB.Database)
		assert.Equal(t, "PostgreSQL 16.0", got.DB.ServerVersion)
		assert.Equal(t, int64(3600), got.DB.PostmasterUpS)
		assert.Equal(t, "2026-06-12T15:30:00+03", got.DB.NowRFC3339)
		assert.False(t, got.DB.IsInRecovery)
		assert.True(t, got.DB.HasStatStmtExt)
		require.NotNil(t, got.SchemaSummary)
		assert.Equal(t, int64(3), got.SchemaSummary.TotalTables)
		assert.Equal(t, int64(12), got.SchemaSummary.TotalColumns)
		assert.Equal(t, int64(2), got.SchemaSummary.TotalPrimaryKeys)
	})

	t.Run("success with project", func(t *testing.T) {
		mock := newMockPool(t)
		expectDBInfoQuery(mock)
		expectUptimeQuery(mock)
		expectExtensionQuery(mock, false)
		expectSchemaSummaryQuery(mock)

		store := mocks.NewProjectStore()
		project := store.SeedProject(userID, "postgresql")
		svc := newServiceWithMock(t, mock, testProjectRepoFromMock(store))

		got, err := svc.GetOverview(ctx, userID, project.ID)

		require.NoError(t, err)
		require.NotNil(t, got)
		require.NotNil(t, got.Instance)
		assert.Equal(t, project.ID, got.Instance.ID)
		assert.Equal(t, "running", got.Instance.Status)
		assert.Nil(t, got.Instance.Host)
		require.NotNil(t, got.DB)
		assert.Equal(t, "app_db", got.DB.Database)
		assert.False(t, got.DB.HasStatStmtExt)
		require.NotNil(t, got.SchemaSummary)
		assert.Equal(t, int64(3), got.SchemaSummary.TotalTables)
		assert.Equal(t, int64(12), got.SchemaSummary.TotalColumns)
		assert.Equal(t, int64(2), got.SchemaSummary.TotalPrimaryKeys)
	})
}
