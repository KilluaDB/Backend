package service

import (
	"context"
	"errors"
	"testing"

	"backend/internal/postgres/model"
	"backend/internal/postgres/repository"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTableRowsMockPool(t *testing.T) pgxmock.PgxPoolIface {
	t.Helper()
	mock, err := pgxmock.NewPool(pgxmock.QueryMatcherOption(pgxmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, mock.ExpectationsWereMet())
		mock.Close()
	})
	return mock
}

func tableSvcWithRowsMock(t *testing.T, mock pgxmock.PgxPoolIface) *TableService {
	t.Helper()
	svc := NewTableService(stubInstanceConn{}, repository.NewTableRepository())
	svc.poolSource = mockTablePoolSource{pool: mock}
	return svc
}

func TestTableService_GetTables_success(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectQuery(`(?s)SELECT table_name.*information_schema\.tables`).
		WithArgs("public").
		WillReturnRows(pgxmock.NewRows([]string{"table_name"}).AddRow("users"))

	svc := tableSvcWithRowsMock(t, mock)
	tables, err := svc.GetTables(context.Background(), uuid.New(), uuid.New(), "public")
	require.NoError(t, err)
	assert.Equal(t, []string{"users"}, tables)
}

func TestTableService_GetTableMetadata_success(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectQuery(`(?s)SELECT 1.*information_schema\.tables`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"?column?"}).AddRow(1))
	mock.ExpectQuery(`(?s)SELECT column_name, data_type.*information_schema\.columns`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{
			"column_name", "data_type", "udt_name", "character_maximum_length",
			"is_nullable", "column_default", "is_identity",
		}).AddRow("id", "integer", "int4", nil, "NO", nil, false))
	mock.ExpectQuery(`(?s)SELECT kcu\.column_name.*PRIMARY KEY`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"column_name"}).AddRow("id"))
	mock.ExpectQuery(`(?s)SELECT.*FOREIGN KEY`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{
			"constraint_name", "column_name", "foreign_table_schema", "foreign_table_name",
			"foreign_column_name", "update_rule", "delete_rule",
		}))

	svc := tableSvcWithRowsMock(t, mock)
	meta, err := svc.GetTableMetadata(context.Background(), uuid.New(), uuid.New(), "public", "users")
	require.NoError(t, err)
	assert.Equal(t, "users", meta.Table)
	assert.Len(t, meta.Columns, 1)
	assert.Equal(t, []string{"id"}, meta.PrimaryKeys)
}

func TestTableService_GetTableMetadata_notFound(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectQuery(`(?s)SELECT 1.*information_schema\.tables`).
		WithArgs("public", "missing").
		WillReturnError(errors.New("no rows")) // not pgx.ErrNoRows from mock - use WillReturnError

	svc := tableSvcWithRowsMock(t, mock)
	_, err := svc.GetTableMetadata(context.Background(), uuid.New(), uuid.New(), "public", "missing")
	require.Error(t, err)
}

func TestTableService_GetTableMetadata_tableNotExists(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectQuery(`(?s)SELECT 1.*information_schema\.tables`).
		WithArgs("public", "ghost").
		WillReturnRows(pgxmock.NewRows([]string{"?column?"})) // empty -> ErrNoRows on scan

	svc := tableSvcWithRowsMock(t, mock)
	_, err := svc.GetTableMetadata(context.Background(), uuid.New(), uuid.New(), "public", "ghost")
	require.Error(t, err)
}

func TestTableService_GetRows_success(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectQuery(`SELECT \* FROM "public"\."users" LIMIT \$1 OFFSET \$2`).
		WithArgs(2, 0).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(1))

	svc := tableSvcWithRowsMock(t, mock)
	result, err := svc.GetRows(context.Background(), uuid.New(), uuid.New(), "public", "users", nil, 1, 0, false)
	require.NoError(t, err)
	assert.Len(t, result.Rows, 1)
}

func TestTableService_GetRows_withTotal(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectQuery(`SELECT \* FROM "public"\."users" LIMIT \$1 OFFSET \$2`).
		WithArgs(2, 0).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM "public"\."users"`).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(10)))

	svc := tableSvcWithRowsMock(t, mock)
	result, err := svc.GetRows(context.Background(), uuid.New(), uuid.New(), "public", "users", nil, 1, 0, true)
	require.NoError(t, err)
	require.NotNil(t, result.Total)
	assert.Equal(t, int64(10), *result.Total)
}

func TestTableService_GetRows_invalidFilterColumn(t *testing.T) {
	svc := tableSvcWithRowsMock(t, newTableRowsMockPool(t))
	_, err := svc.GetRows(context.Background(), uuid.New(), uuid.New(), "public", "users",
		map[string]interface{}{"bad col": 1}, 1, 0, false)
	require.Error(t, err)
}

func TestTableService_UpdateRows_success(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectExec(`UPDATE "public"\."users" SET "email" = \$1`).
		WithArgs("x@test.com").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	svc := tableSvcWithRowsMock(t, mock)
	err := svc.UpdateRows(context.Background(), uuid.New(), uuid.New(), "public", "users", nil,
		map[string]interface{}{"email": "x@test.com"})
	require.NoError(t, err)
}

func TestTableService_UpdateRows_emptyUpdate(t *testing.T) {
	svc := tableSvcWithRowsMock(t, newTableRowsMockPool(t))
	err := svc.UpdateRows(context.Background(), uuid.New(), uuid.New(), "public", "users", nil, map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update cannot be empty")
}

func TestTableService_DeleteRowsByFilter_success(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectExec(`DELETE FROM "public"\."users"`).
		WillReturnResult(pgxmock.NewResult("DELETE", 3))

	svc := tableSvcWithRowsMock(t, mock)
	err := svc.DeleteRowsByFilter(context.Background(), uuid.New(), uuid.New(), "public", "users", nil)
	require.NoError(t, err)
}

func TestTableService_InsertRow_success(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectQuery(`(?s)SELECT EXISTS.*information_schema\.columns`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"exists", "data_type"}).AddRow(false, ""))
	mock.ExpectExec(`INSERT INTO "public"\."users" \("email"\) VALUES \(\$1\)`).
		WithArgs("a@test.com").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	svc := tableSvcWithRowsMock(t, mock)
	resp, err := svc.InsertRow(context.Background(), uuid.New(), uuid.New(), InsertRowRequest{
		Schema: "public", Table: "users", Values: map[string]interface{}{"email": "a@test.com"},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(0), resp.RowID)
}

func TestTableService_InsertRow_emptyValues(t *testing.T) {
	svc := tableSvcWithRowsMock(t, newTableRowsMockPool(t))
	_, err := svc.InsertRow(context.Background(), uuid.New(), uuid.New(), InsertRowRequest{
		Schema: "public", Table: "users", Values: map[string]interface{}{},
	})
	require.Error(t, err)
}

func TestTableService_AddColumn_success(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectQuery(`(?s)SELECT 1.*information_schema\.tables`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"?column?"}).AddRow(1))
	mock.ExpectQuery(`(?s)SELECT kcu\.column_name.*PRIMARY KEY`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"column_name"}))
	mock.ExpectBegin()
	mock.ExpectExec(`ALTER TABLE "public"\."users" ADD COLUMN "nickname" TEXT NOT NULL`).
		WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))
	mock.ExpectQuery(`(?s)SELECT ordinal_position.*information_schema\.columns`).
		WithArgs("public", "users", "nickname").
		WillReturnRows(pgxmock.NewRows([]string{"ordinal_position"}).AddRow(int64(2)))
	mock.ExpectCommit()

	svc := tableSvcWithRowsMock(t, mock)
	resp, err := svc.AddColumn(context.Background(), uuid.New(), uuid.New(), AddColumnRequest{
		Schema: "public", TableName: "users", Name: "nickname", Type: "TEXT", Nullable: false,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), resp.ColumnID)
}

func TestTableService_AddColumn_tableNotFound(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectQuery(`(?s)SELECT 1.*information_schema\.tables`).
		WithArgs("public", "missing").
		WillReturnRows(pgxmock.NewRows([]string{"?column?"}))

	svc := tableSvcWithRowsMock(t, mock)
	_, err := svc.AddColumn(context.Background(), uuid.New(), uuid.New(), AddColumnRequest{
		Schema: "public", TableName: "missing", Name: "x", Type: "TEXT",
	})
	require.ErrorIs(t, err, ErrTableNotFound)
}

func TestTableService_AddColumn_invalidColumn(t *testing.T) {
	svc := tableSvcWithRowsMock(t, newTableRowsMockPool(t))
	_, err := svc.AddColumn(context.Background(), uuid.New(), uuid.New(), AddColumnRequest{
		Schema: "public", TableName: "users", Name: "bad col", Type: "TEXT",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidTableRequest)
}

func TestTableService_DeleteColumn_success(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" DROP COLUMN "nickname"`).
		WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))

	svc := tableSvcWithRowsMock(t, mock)
	err := svc.DeleteColumn(context.Background(), uuid.New(), uuid.New(),
		DeleteColumnRequest{Schema: "public", TableName: "users"}, "nickname")
	require.NoError(t, err)
}

func TestTableService_DeleteColumn_invalidName(t *testing.T) {
	svc := tableSvcWithRowsMock(t, newTableRowsMockPool(t))
	err := svc.DeleteColumn(context.Background(), uuid.New(), uuid.New(),
		DeleteColumnRequest{Schema: "public", TableName: "users"}, "bad col")
	require.Error(t, err)
}

func TestTableService_ListTableIndexes_success(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectQuery(`(?s)SELECT i\.relname.*pg_class t`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{
			"relname", "indisunique", "indisprimary", "amname", "indexdef", "indisvalid",
		}).AddRow("idx_email", true, false, "btree", "def", true))

	svc := tableSvcWithRowsMock(t, mock)
	list, err := svc.ListTableIndexes(context.Background(), uuid.New(), uuid.New(), "public", "users")
	require.NoError(t, err)
	assert.Len(t, list, 1)
}

func TestTableService_CreateTableIndex_success(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectExec(`CREATE UNIQUE INDEX "idx_email" ON "public"\."users" \("email"\)`).
		WillReturnResult(pgxmock.NewResult("CREATE INDEX", 0))

	svc := tableSvcWithRowsMock(t, mock)
	err := svc.CreateTableIndex(context.Background(), uuid.New(), uuid.New(), "public", "users",
		&model.CreateIndexRequest{Name: "idx_email", Columns: []string{"email"}, Unique: true})
	require.NoError(t, err)
}

func TestTableService_CreateTableIndex_duplicate(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectExec(`CREATE INDEX`).
		WillReturnError(&pgconn.PgError{Code: "42P07"})

	svc := tableSvcWithRowsMock(t, mock)
	err := svc.CreateTableIndex(context.Background(), uuid.New(), uuid.New(), "public", "users",
		&model.CreateIndexRequest{Name: "idx_email", Columns: []string{"email"}})
	require.ErrorIs(t, err, ErrIndexAlreadyExists)
}

func TestTableService_DropTableIndex_success(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectQuery(`(?s)SELECT ix\.indisprimary`).
		WithArgs("public", "users", "idx_email").
		WillReturnRows(pgxmock.NewRows([]string{"indisprimary"}).AddRow(false))
	mock.ExpectExec(`DROP INDEX "public"\."idx_email"`).
		WillReturnResult(pgxmock.NewResult("DROP INDEX", 0))

	svc := tableSvcWithRowsMock(t, mock)
	err := svc.DropTableIndex(context.Background(), uuid.New(), uuid.New(), "public", "users", "idx_email")
	require.NoError(t, err)
}

func TestSqlLiteralFromDefault(t *testing.T) {
	got := sqlLiteralFromDefault("hello")
	require.NotNil(t, got)
	assert.Equal(t, "'hello'", *got)

	b := sqlLiteralFromDefault(true)
	require.NotNil(t, b)
	assert.Equal(t, "TRUE", *b)

	assert.Nil(t, sqlLiteralFromDefault(nil))
	assert.Nil(t, sqlLiteralFromDefault(""))
}
