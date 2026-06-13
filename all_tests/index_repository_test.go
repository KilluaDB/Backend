package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTableRepository_ListIndexes_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT i\.relname.*pg_class t`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{
			"relname", "indisunique", "indisprimary", "amname", "indexdef", "indisvalid",
		}).AddRow("users_pkey", true, true, "btree", "CREATE UNIQUE INDEX ...", true))

	repo := NewTableRepository()
	list, err := repo.ListIndexes(context.Background(), mock, "public", "users")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "users_pkey", list[0].Name)
	assert.True(t, list[0].Primary)
}

func TestTableRepository_ListIndexes_empty(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT i\.relname.*pg_class t`).
		WithArgs("public", "empty").
		WillReturnRows(pgxmock.NewRows([]string{
			"relname", "indisunique", "indisprimary", "amname", "indexdef", "indisvalid",
		}))

	repo := NewTableRepository()
	list, err := repo.ListIndexes(context.Background(), mock, "public", "empty")
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestTableRepository_CreateIndex_btree(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`CREATE INDEX "idx_email" ON "public"\."users" \("email"\)`).
		WillReturnResult(pgxmock.NewResult("CREATE INDEX", 0))

	repo := NewTableRepository()
	err := repo.CreateIndex(context.Background(), mock, "public", "users", "idx_email", []string{"email"}, false, "")
	require.NoError(t, err)
}

func TestTableRepository_CreateIndex_uniqueGin(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`CREATE UNIQUE INDEX "idx_data" ON "public"\."docs" USING gin \("data"\)`).
		WillReturnResult(pgxmock.NewResult("CREATE INDEX", 0))

	repo := NewTableRepository()
	err := repo.CreateIndex(context.Background(), mock, "public", "docs", "idx_data", []string{"data"}, true, "gin")
	require.NoError(t, err)
}

func TestTableRepository_CreateIndex_execError(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`CREATE INDEX`).
		WillReturnError(errors.New("create failed"))

	repo := NewTableRepository()
	err := repo.CreateIndex(context.Background(), mock, "public", "users", "idx", []string{"email"}, false, "btree")
	require.Error(t, err)
}

func TestTableRepository_DropIndex_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT ix\.indisprimary.*pg_class i`).
		WithArgs("public", "users", "idx_email").
		WillReturnRows(pgxmock.NewRows([]string{"indisprimary"}).AddRow(false))
	mock.ExpectExec(`DROP INDEX "public"\."idx_email"`).
		WillReturnResult(pgxmock.NewResult("DROP INDEX", 0))

	repo := NewTableRepository()
	err := repo.DropIndex(context.Background(), mock, "public", "users", "idx_email")
	require.NoError(t, err)
}

func TestTableRepository_DropIndex_notFound(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT ix\.indisprimary`).
		WithArgs("public", "users", "missing").
		WillReturnError(pgx.ErrNoRows)

	repo := NewTableRepository()
	err := repo.DropIndex(context.Background(), mock, "public", "users", "missing")
	require.ErrorIs(t, err, ErrIndexNotFound)
}

func TestTableRepository_DropIndex_primary(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT ix\.indisprimary`).
		WithArgs("public", "users", "users_pkey").
		WillReturnRows(pgxmock.NewRows([]string{"indisprimary"}).AddRow(true))

	repo := NewTableRepository()
	err := repo.DropIndex(context.Background(), mock, "public", "users", "users_pkey")
	require.ErrorIs(t, err, ErrCannotDropPrimaryIndex)
}
