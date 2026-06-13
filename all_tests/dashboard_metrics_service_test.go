package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDashboardMetricsService_GetMetrics(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	projectID := uuid.New()

	newServiceWithMock := func(t *testing.T, mock pgxmock.PgxPoolIface) *DashboardMetricsService {
		t.Helper()
		svc := NewDashboardMetricsService(stubInstanceConn{})
		svc.runnerSource = mockRunnerSource{runner: mock}
		return svc
	}

	expectFirstQuery := func(mock pgxmock.PgxPoolIface) {
		mock.ExpectQuery(`current_database\(\)`).
			WillReturnRows(pgxmock.NewRows([]string{"current_database", "pg_database_size"}).
				AddRow("app_db", int64(2048)))
	}

	expectSecondQuery := func(mock pgxmock.PgxPoolIface) {
		mock.ExpectQuery(`pg_stat_activity`).
			WillReturnRows(pgxmock.NewRows([]string{"active", "idle"}).
				AddRow(int64(4), int64(2)))
	}

	expectThirdQuery := func(mock pgxmock.PgxPoolIface, blksHit, blksRead, deadlocks int64) {
		mock.ExpectQuery(`pg_stat_database`).
			WillReturnRows(pgxmock.NewRows([]string{"blks_hit", "blks_read", "deadlocks"}).
				AddRow(blksHit, blksRead, deadlocks))
	}

	expectBestEffortQueries := func(mock pgxmock.PgxPoolIface, blockedSessions, tablesNeedingVacuum int64) {
		mock.ExpectQuery(`pg_stat_activity.*Lock`).
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(blockedSessions))
		mock.ExpectQuery(`pg_stat_user_tables`).
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(tablesNeedingVacuum))
	}

	t.Run("pool error", func(t *testing.T) {
		svc := NewDashboardMetricsService(stubInstanceConn{})

		got, err := svc.GetMetrics(ctx, userID, projectID)

		require.Error(t, err)
		assert.Nil(t, got)
	})

	t.Run("first query error", func(t *testing.T) {
		mock := newMockPool(t)
		mock.ExpectQuery(`current_database\(\)`).
			WillReturnError(errors.New("db size failed"))
		svc := newServiceWithMock(t, mock)

		got, err := svc.GetMetrics(ctx, userID, projectID)

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "db size failed")
	})

	t.Run("second query error", func(t *testing.T) {
		mock := newMockPool(t)
		expectFirstQuery(mock)
		mock.ExpectQuery(`pg_stat_activity`).
			WillReturnError(errors.New("connections failed"))
		svc := newServiceWithMock(t, mock)

		got, err := svc.GetMetrics(ctx, userID, projectID)

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "connections failed")
	})

	t.Run("third query error", func(t *testing.T) {
		mock := newMockPool(t)
		expectFirstQuery(mock)
		expectSecondQuery(mock)
		mock.ExpectQuery(`pg_stat_database`).
			WillReturnError(errors.New("database stats failed"))
		svc := newServiceWithMock(t, mock)

		got, err := svc.GetMetrics(ctx, userID, projectID)

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "database stats failed")
	})

	t.Run("success with non-zero cache ratio", func(t *testing.T) {
		mock := newMockPool(t)
		expectFirstQuery(mock)
		expectSecondQuery(mock)
		expectThirdQuery(mock, 900, 100, 3)
		expectBestEffortQueries(mock, 5, 7)
		svc := newServiceWithMock(t, mock)

		got, err := svc.GetMetrics(ctx, userID, projectID)

		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "app_db", got.Database)
		assert.Equal(t, int64(2048), got.DBSizeBytes)
		assert.Equal(t, int64(4), got.ActiveConns)
		assert.Equal(t, int64(2), got.IdleConns)
		assert.Equal(t, 0.9, got.CacheHitRatio)
		assert.Equal(t, int64(3), got.Deadlocks)
		assert.Equal(t, int64(5), got.BlockedSessions)
		assert.Equal(t, int64(7), got.TablesNeedingVacuum)
	})

	t.Run("success with zero denominator", func(t *testing.T) {
		mock := newMockPool(t)
		expectFirstQuery(mock)
		expectSecondQuery(mock)
		expectThirdQuery(mock, 0, 0, 1)
		expectBestEffortQueries(mock, 0, 2)
		svc := newServiceWithMock(t, mock)

		got, err := svc.GetMetrics(ctx, userID, projectID)

		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, 0.0, got.CacheHitRatio)
		assert.Equal(t, int64(1), got.Deadlocks)
		assert.Equal(t, int64(0), got.BlockedSessions)
		assert.Equal(t, int64(2), got.TablesNeedingVacuum)
	})

	t.Run("best-effort queries", func(t *testing.T) {
		mock := newMockPool(t)
		mock.MatchExpectationsInOrder(false)
		expectFirstQuery(mock)
		expectSecondQuery(mock)
		expectThirdQuery(mock, 9, 1, 0)
		mock.ExpectQuery(`pg_stat_activity.*Lock`).
			WillReturnError(errors.New("blocked sessions failed"))
		mock.ExpectQuery(`pg_stat_user_tables`).
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(6)))
		svc := newServiceWithMock(t, mock)

		got, err := svc.GetMetrics(ctx, userID, projectID)

		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "app_db", got.Database)
		assert.Equal(t, 0.9, got.CacheHitRatio)
		assert.Equal(t, int64(0), got.BlockedSessions)
		assert.Equal(t, int64(6), got.TablesNeedingVacuum)
	})
}
