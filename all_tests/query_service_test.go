package service

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const extCheckSQL = `SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements')`

type stubInstanceConn struct {
	instanceID uuid.UUID
}

func (s stubInstanceConn) GetPool(ctx context.Context, userID, projectID uuid.UUID) (*pgxpool.Pool, error) {
	return nil, errors.New("unexpected GetPool")
}

func (s stubInstanceConn) GetPoolWithMeta(ctx context.Context, userID, projectID uuid.UUID) (*pgxpool.Pool, uuid.UUID, error) {
	return nil, uuid.Nil, errors.New("unexpected GetPoolWithMeta")
}

func (s stubInstanceConn) GetInstanceID(ctx context.Context, userID, projectID uuid.UUID) (uuid.UUID, error) {
	if s.instanceID == uuid.Nil {
		return uuid.New(), nil
	}
	return s.instanceID, nil
}

type mockRunnerSource struct {
	runner pgQueryRunner
	id     uuid.UUID
	err    error
}

func (m mockRunnerSource) QueryRunner(ctx context.Context, userID, projectID uuid.UUID) (pgQueryRunner, uuid.UUID, error) {
	if m.err != nil {
		return nil, uuid.Nil, m.err
	}
	id := m.id
	if id == uuid.Nil {
		id = uuid.New()
	}
	return m.runner, id, nil
}

func newMockPool(t *testing.T) pgxmock.PgxPoolIface {
	t.Helper()
	mock, err := pgxmock.NewPool(pgxmock.QueryMatcherOption(pgxmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, mock.ExpectationsWereMet())
		mock.Close()
	})
	return mock
}

func svcWithMock(t *testing.T, mock pgxmock.PgxPoolIface, instID uuid.UUID) *QueryService {
	t.Helper()
	svc := NewQueryService(nil, 50)
	svc.runnerSource = mockRunnerSource{runner: mock, id: instID}
	return svc
}

func TestNewQueryService_defaultMaxLimit(t *testing.T) {
	svc := NewQueryService(nil, 0)
	assert.Equal(t, 50, svc.maxLimit)
}

func TestQueryService_ValidateSQLQuery(t *testing.T) {
	svc := NewQueryService(nil, 50)

	tests := []struct {
		name    string
		query   string
		wantErr string
	}{
		{"valid select", "SELECT 1", ""},
		{"empty", "", "query cannot be empty"},
		{"comments only", "-- comment\n", "query cannot be empty"},
		{"drop database", "DROP DATABASE x", "not allowed"},
		{"delete without where", "DELETE FROM users", "WHERE clause"},
		{"delete with where", "DELETE FROM users WHERE id = 1", ""},
		{"multiple statements", "SELECT 1; SELECT 2; SELECT 3", "multiple statements"},
		{"truncate", "TRUNCATE users", "not allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.ValidateSQLQuery(tt.query)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestIsSystemQueryText(t *testing.T) {
	tests := []struct {
		query string
		want  bool
	}{
		{"BEGIN", true},
		{"SELECT * FROM users", false},
		{"SELECT * FROM pg_catalog.pg_tables", true},
		{"AUTOVACUUM ANALYZE", true},
		{"", true},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, isSystemQueryText(tt.query), "query=%q", tt.query)
	}
}

func TestQueryService_ExecuteSQLQuery_invalidQuery(t *testing.T) {
	svc := NewQueryService(stubInstanceConn{instanceID: uuid.New()}, 50)
	userID := uuid.New()
	projectID := uuid.New()

	result, hist, err := svc.ExecuteSQLQuery(context.Background(), userID, &ExecuteQueryRequest{
		Query: "DROP DATABASE x",
	}, projectID)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidQuery)
	require.NotNil(t, result)
	assert.Contains(t, result.Error, "not allowed")
	require.NotNil(t, hist)
	require.NotNil(t, hist.Success)
	assert.False(t, *hist.Success)
}

func TestQueryService_ExecuteSQLQuery_poolError(t *testing.T) {
	svc := NewQueryService(nil, 50)
	svc.runnerSource = mockRunnerSource{err: errors.New("pool unavailable")}
	userID := uuid.New()
	projectID := uuid.New()

	_, _, err := svc.ExecuteSQLQuery(context.Background(), userID, &ExecuteQueryRequest{
		Query: "SELECT 1",
	}, projectID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pool unavailable")
}

func TestQueryService_ExecuteSQLQuery_selectSuccess(t *testing.T) {
	mock := newMockPool(t)
	instID := uuid.New()
	svc := svcWithMock(t, mock, instID)
	userID := uuid.New()
	projectID := uuid.New()
	query := "SELECT id, name FROM users"

	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WillReturnRows(pgxmock.NewRows([]string{"id", "name"}).
			AddRow(int64(1), "alice").
			AddRow(int64(2), "bob"))

	result, hist, err := svc.ExecuteSQLQuery(context.Background(), userID, &ExecuteQueryRequest{Query: query}, projectID)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, []string{"id", "name"}, result.Columns)
	assert.Len(t, result.Rows, 2)
	assert.Equal(t, 2, result.RowCount)
	assert.Empty(t, result.Error)
	require.NotNil(t, hist)
	require.NotNil(t, hist.Success)
	assert.True(t, *hist.Success)
	assert.Equal(t, instID, hist.DBInstanceID)
}

func TestQueryService_ExecuteSQLQuery_selectTypedValues(t *testing.T) {
	mock := newMockPool(t)
	svc := svcWithMock(t, mock, uuid.New())
	query := "SELECT uid, created_at FROM users"

	uid := uuid.New()
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WillReturnRows(pgxmock.NewRows([]string{"uid", "created_at"}).
			AddRow(uid[:], ts))

	result, _, err := svc.ExecuteSQLQuery(context.Background(), uuid.New(), &ExecuteQueryRequest{Query: query}, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, uid.String(), result.Rows[0]["uid"])
	assert.Equal(t, ts.Format(time.RFC3339), result.Rows[0]["created_at"])
}

func TestQueryService_ExecuteSQLQuery_insertSuccess(t *testing.T) {
	mock := newMockPool(t)
	svc := svcWithMock(t, mock, uuid.New())
	query := "INSERT INTO users (name) VALUES ('test')"

	mock.ExpectExec(regexp.QuoteMeta(query)).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	result, hist, err := svc.ExecuteSQLQuery(context.Background(), uuid.New(), &ExecuteQueryRequest{Query: query}, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, int64(1), result.RowsAffected)
	assert.Equal(t, 0, result.RowCount)
	require.NotNil(t, hist.Success)
	assert.True(t, *hist.Success)
}

func TestQueryService_ExecuteSQLQuery_updateSuccess(t *testing.T) {
	mock := newMockPool(t)
	svc := svcWithMock(t, mock, uuid.New())
	query := "UPDATE users SET name = 'new' WHERE id = 1"

	mock.ExpectExec(regexp.QuoteMeta(query)).WillReturnResult(pgxmock.NewResult("UPDATE", 2))

	result, _, err := svc.ExecuteSQLQuery(context.Background(), uuid.New(), &ExecuteQueryRequest{Query: query}, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, int64(2), result.RowsAffected)
}

func TestQueryService_ExecuteSQLQuery_deleteSuccess(t *testing.T) {
	mock := newMockPool(t)
	svc := svcWithMock(t, mock, uuid.New())
	query := "DELETE FROM users WHERE id = 1"

	mock.ExpectExec(regexp.QuoteMeta(query)).WillReturnResult(pgxmock.NewResult("DELETE", 1))

	result, _, err := svc.ExecuteSQLQuery(context.Background(), uuid.New(), &ExecuteQueryRequest{Query: query}, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, int64(1), result.RowsAffected)
}

func TestQueryService_ExecuteSQLQuery_selectExecutionError(t *testing.T) {
	mock := newMockPool(t)
	svc := svcWithMock(t, mock, uuid.New())
	query := "SELECT broken"

	mock.ExpectQuery(regexp.QuoteMeta(query)).WillReturnError(errors.New("syntax error at line 1"))

	result, hist, err := svc.ExecuteSQLQuery(context.Background(), uuid.New(), &ExecuteQueryRequest{Query: query}, uuid.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "syntax error")
	require.NotNil(t, result)
	assert.Contains(t, result.Error, "syntax error")
	require.NotNil(t, hist.Success)
	assert.False(t, *hist.Success)
}

func TestQueryService_ExecuteSQLQuery_execExecutionError(t *testing.T) {
	mock := newMockPool(t)
	svc := svcWithMock(t, mock, uuid.New())
	query := "UPDATE users SET name = 'x' WHERE id = 1"

	mock.ExpectExec(regexp.QuoteMeta(query)).WillReturnError(errors.New("permission denied"))

	result, _, err := svc.ExecuteSQLQuery(context.Background(), uuid.New(), &ExecuteQueryRequest{Query: query}, uuid.New())
	require.Error(t, err)
	assert.Contains(t, result.Error, "permission denied")
}

func TestQueryService_GetQueryHistory_poolError(t *testing.T) {
	svc := NewQueryService(nil, 50)
	svc.runnerSource = mockRunnerSource{err: errors.New("no pool")}

	_, err := svc.GetQueryHistory(context.Background(), uuid.New(), uuid.New(), 10)
	require.Error(t, err)
}

func TestQueryService_GetQueryHistory_extensionCheckError(t *testing.T) {
	mock := newMockPool(t)
	svc := svcWithMock(t, mock, uuid.New())

	mock.ExpectQuery(regexp.QuoteMeta(extCheckSQL)).WillReturnError(errors.New("extension lookup failed"))

	_, err := svc.GetQueryHistory(context.Background(), uuid.New(), uuid.New(), 10)
	require.Error(t, err)
}

func TestQueryService_GetQueryHistory_extensionDisabled(t *testing.T) {
	mock := newMockPool(t)
	svc := svcWithMock(t, mock, uuid.New())

	mock.ExpectQuery(regexp.QuoteMeta(extCheckSQL)).
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))

	items, err := svc.GetQueryHistory(context.Background(), uuid.New(), uuid.New(), 10)
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestQueryService_GetQueryHistory_successFiltersSystemQueries(t *testing.T) {
	mock := newMockPool(t)
	svc := svcWithMock(t, mock, uuid.New())

	mock.ExpectQuery(regexp.QuoteMeta(extCheckSQL)).
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))

	historySQL := `
		SELECT query, calls::bigint, total_exec_time, mean_exec_time, rows::bigint,
		       shared_blks_hit::bigint, shared_blks_read::bigint, temp_blks_written::bigint
		FROM pg_stat_statements
		WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
		  AND userid = (SELECT oid FROM pg_roles WHERE rolname = current_user)
		ORDER BY total_exec_time DESC, calls DESC
		LIMIT 50
	`
	mock.ExpectQuery(regexp.QuoteMeta(historySQL)).
		WillReturnRows(pgxmock.NewRows([]string{
			"query", "calls", "total_exec_time", "mean_exec_time", "rows",
			"shared_blks_hit", "shared_blks_read", "temp_blks_written",
		}).
			AddRow("VACUUM users", int64(1), 10.0, 10.0, int64(0), int64(0), int64(0), int64(0)).
			AddRow("SELECT * FROM orders", int64(5), 50.0, 10.0, int64(100), int64(1), int64(2), int64(0)))

	items, err := svc.GetQueryHistory(context.Background(), uuid.New(), uuid.New(), 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "SELECT * FROM orders", items[0].Query)
	assert.Equal(t, int64(5), items[0].Calls)
}

func TestQueryService_GetQueryHistory_limitDefaultsAndCap(t *testing.T) {
	mock := newMockPool(t)
	svc := svcWithMock(t, mock, uuid.New())

	expectHistory := func(limit int) {
		mock.ExpectQuery(regexp.QuoteMeta(extCheckSQL)).
			WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
		historySQL := `
		SELECT query, calls::bigint, total_exec_time, mean_exec_time, rows::bigint,
		       shared_blks_hit::bigint, shared_blks_read::bigint, temp_blks_written::bigint
		FROM pg_stat_statements
		WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
		  AND userid = (SELECT oid FROM pg_roles WHERE rolname = current_user)
		ORDER BY total_exec_time DESC, calls DESC
		LIMIT 50
	`
		rows := pgxmock.NewRows([]string{
			"query", "calls", "total_exec_time", "mean_exec_time", "rows",
			"shared_blks_hit", "shared_blks_read", "temp_blks_written",
		})
		for i := 0; i < 35; i++ {
			rows.AddRow("SELECT "+string(rune('a'+i%26)), int64(1), 1.0, 1.0, int64(1), int64(0), int64(0), int64(0))
		}
		mock.ExpectQuery(regexp.QuoteMeta(historySQL)).WillReturnRows(rows)
		items, err := svc.GetQueryHistory(context.Background(), uuid.New(), uuid.New(), limit)
		require.NoError(t, err)
		if limit <= 0 {
			limit = 10
		}
		if limit > 30 {
			limit = 30
		}
		assert.Len(t, items, limit)
	}

	t.Run("default limit", func(t *testing.T) { expectHistory(0) })
	t.Run("capped limit", func(t *testing.T) { expectHistory(100) })
}

var _ pgQueryRunner = (pgxmock.PgxPoolIface)(nil)
