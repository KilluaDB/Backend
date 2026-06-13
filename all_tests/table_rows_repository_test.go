package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTableRepository_SelectRows_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`SELECT \* FROM "public"\."users" LIMIT \$1 OFFSET \$2`).
		WithArgs(3, 0).
		WillReturnRows(pgxmock.NewRows([]string{"id", "email"}).
			AddRow(int32(1), "a@test.com").
			AddRow(int32(2), "b@test.com"))

	repo := NewTableRepository()
	rows, hasMore, err := repo.SelectRows(context.Background(), mock, "public", "users", nil, 2, 0)
	require.NoError(t, err)
	assert.False(t, hasMore)
	assert.Len(t, rows, 2)
	assert.Equal(t, int32(1), rows[0]["id"])
}

func TestTableRepository_SelectRows_hasMore(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`SELECT \* FROM "public"\."users" LIMIT \$1 OFFSET \$2`).
		WithArgs(3, 0).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).
			AddRow(1).AddRow(2).AddRow(3))

	repo := NewTableRepository()
	rows, hasMore, err := repo.SelectRows(context.Background(), mock, "public", "users", nil, 2, 0)
	require.NoError(t, err)
	assert.True(t, hasMore)
	assert.Len(t, rows, 2)
}

func TestTableRepository_SelectRows_withFilter(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`SELECT \* FROM "public"\."users" WHERE "id" = \$1 LIMIT \$2 OFFSET \$3`).
		WithArgs(1, 2, 0).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(1))

	repo := NewTableRepository()
	rows, _, err := repo.SelectRows(context.Background(), mock, "public", "users", map[string]interface{}{"id": 1}, 1, 0)
	require.NoError(t, err)
	assert.Len(t, rows, 1)
}

func TestTableRepository_SelectRows_invalidLimit(t *testing.T) {
	repo := NewTableRepository()
	_, _, err := repo.SelectRows(context.Background(), newRepoMock(t), "public", "users", nil, 0, 0)
	require.Error(t, err)
}

func TestTableRepository_SelectRows_queryError(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`SELECT \* FROM "public"\."users"`).
		WithArgs(2, 0).
		WillReturnError(errors.New("select failed"))

	repo := NewTableRepository()
	_, _, err := repo.SelectRows(context.Background(), mock, "public", "users", nil, 1, 0)
	require.Error(t, err)
}

func TestTableRepository_CountRows_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM "public"\."users"`).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(5)))

	repo := NewTableRepository()
	n, err := repo.CountRows(context.Background(), mock, "public", "users", nil)
	require.NoError(t, err)
	assert.Equal(t, int64(5), n)
}

func TestTableRepository_CountRows_withFilter(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM "public"\."users" WHERE "status" = \$1`).
		WithArgs("active").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(2)))

	repo := NewTableRepository()
	n, err := repo.CountRows(context.Background(), mock, "public", "users", map[string]interface{}{"status": "active"})
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)
}

func TestTableRepository_UpdateRows_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`UPDATE "public"\."users" SET "email" = \$1 WHERE "id" = \$2`).
		WithArgs("new@test.com", 1).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	repo := NewTableRepository()
	err := repo.UpdateRows(context.Background(), mock, "public", "users",
		map[string]interface{}{"id": 1},
		map[string]interface{}{"email": "new@test.com"},
	)
	require.NoError(t, err)
}

func TestTableRepository_UpdateRows_allRows(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`UPDATE "public"\."users" SET "active" = \$1`).
		WithArgs(true).
		WillReturnResult(pgxmock.NewResult("UPDATE", 10))

	repo := NewTableRepository()
	err := repo.UpdateRows(context.Background(), mock, "public", "users", nil, map[string]interface{}{"active": true})
	require.NoError(t, err)
}

func TestTableRepository_UpdateRows_execError(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`UPDATE "public"\."users"`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(errors.New("update failed"))

	repo := NewTableRepository()
	err := repo.UpdateRows(context.Background(), mock, "public", "users", nil, map[string]interface{}{"x": 1})
	require.Error(t, err)
}

func TestTableRepository_DeleteRowsByFilter_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`DELETE FROM "public"\."users" WHERE "id" = \$1`).
		WithArgs(1).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	repo := NewTableRepository()
	err := repo.DeleteRowsByFilter(context.Background(), mock, "public", "users", map[string]interface{}{"id": 1})
	require.NoError(t, err)
}

func TestTableRepository_DeleteRowsByFilter_all(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`DELETE FROM "public"\."users"`).
		WillReturnResult(pgxmock.NewResult("DELETE", 100))

	repo := NewTableRepository()
	err := repo.DeleteRowsByFilter(context.Background(), mock, "public", "users", nil)
	require.NoError(t, err)
}

func TestTableRepository_InsertRow_noIDColumn(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT EXISTS.*information_schema\.columns`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"exists", "data_type"}).AddRow(false, ""))
	mock.ExpectExec(`INSERT INTO "public"\."users" \("email"\) VALUES \(\$1\)`).
		WithArgs("a@test.com").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	repo := NewTableRepository()
	id, err := repo.InsertRow(context.Background(), mock, "public", "users", map[string]interface{}{"email": "a@test.com"})
	require.NoError(t, err)
	assert.Equal(t, int64(0), id)
}

func TestTableRepository_InsertRow_withIntID(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT EXISTS.*information_schema\.columns`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"exists", "data_type"}).AddRow(true, "integer"))
	mock.ExpectQuery(`INSERT INTO "public"\."users" \("email"\) VALUES \(\$1\) RETURNING id`).
		WithArgs("a@test.com").
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(int64(42)))

	repo := NewTableRepository()
	id, err := repo.InsertRow(context.Background(), mock, "public", "users", map[string]interface{}{"email": "a@test.com"})
	require.NoError(t, err)
	assert.Equal(t, int64(42), id)
}

func TestTableRepository_InsertRow_execError(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT EXISTS.*information_schema\.columns`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"exists", "data_type"}).AddRow(false, ""))
	mock.ExpectExec(`INSERT INTO "public"\."users"`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(errors.New("insert failed"))

	repo := NewTableRepository()
	_, err := repo.InsertRow(context.Background(), mock, "public", "users", map[string]interface{}{"email": "x"})
	require.Error(t, err)
}

func TestScanRowsToMaps_types(t *testing.T) {
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	mock := newRepoMock(t)
	mock.ExpectQuery(`SELECT \* FROM "public"\."t"`).
		WithArgs(2, 0).
		WillReturnRows(pgxmock.NewRows([]string{"ts", "data"}).
			AddRow(now, []byte("hello")))

	repo := NewTableRepository()
	rows, _, err := repo.SelectRows(context.Background(), mock, "public", "t", nil, 1, 0)
	require.NoError(t, err)
	assert.Equal(t, now.Format(time.RFC3339), rows[0]["ts"])
	assert.Equal(t, "hello", rows[0]["data"])
}
