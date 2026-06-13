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

func newRepoMock(t *testing.T) pgxmock.PgxPoolIface {
	t.Helper()
	mock, err := pgxmock.NewPool(pgxmock.QueryMatcherOption(pgxmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, mock.ExpectationsWereMet())
		mock.Close()
	})
	return mock
}

func TestSchemaRepository_GetTables_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT table_name.*information_schema\.tables`).
		WithArgs("public").
		WillReturnRows(pgxmock.NewRows([]string{"table_name"}).AddRow("users").AddRow("orders"))

	repo := NewSchemaRepository(mock)
	tables, err := repo.GetTables(context.Background(), "public")
	require.NoError(t, err)
	assert.Equal(t, []string{"users", "orders"}, tables)
}

func TestSchemaRepository_GetTables_empty(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT table_name.*information_schema\.tables`).
		WithArgs("public").
		WillReturnRows(pgxmock.NewRows([]string{"table_name"}))

	repo := NewSchemaRepository(mock)
	tables, err := repo.GetTables(context.Background(), "public")
	require.NoError(t, err)
	assert.Empty(t, tables)
}

func TestSchemaRepository_GetTables_queryError(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT table_name.*information_schema\.tables`).
		WithArgs("public").
		WillReturnError(errors.New("query failed"))

	repo := NewSchemaRepository(mock)
	_, err := repo.GetTables(context.Background(), "public")
	require.Error(t, err)
}

func TestSchemaRepository_TableExists_true(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT 1.*information_schema\.tables`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"?column?"}).AddRow(1))

	repo := NewSchemaRepository(mock)
	ok, err := repo.TableExists(context.Background(), "public", "users")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestSchemaRepository_TableExists_false(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT 1.*information_schema\.tables`).
		WithArgs("public", "missing").
		WillReturnError(pgx.ErrNoRows)

	repo := NewSchemaRepository(mock)
	ok, err := repo.TableExists(context.Background(), "public", "missing")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestSchemaRepository_TableExists_error(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT 1.*information_schema\.tables`).
		WithArgs("public", "users").
		WillReturnError(errors.New("db down"))

	repo := NewSchemaRepository(mock)
	_, err := repo.TableExists(context.Background(), "public", "users")
	require.Error(t, err)
}

func TestSchemaRepository_GetColumnDetails_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT column_name, data_type.*information_schema\.columns`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{
			"column_name", "data_type", "udt_name", "character_maximum_length",
			"is_nullable", "column_default", "is_identity",
		}).AddRow("id", "integer", "int4", nil, "NO", nil, false))

	repo := NewSchemaRepository(mock)
	cols, err := repo.GetColumnDetails(context.Background(), "public", "users")
	require.NoError(t, err)
	require.Len(t, cols, 1)
	assert.Equal(t, "id", cols[0].Name)
	assert.False(t, cols[0].IsNullable)
}

func TestSchemaRepository_GetPrimaryKeys_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT kcu\.column_name.*PRIMARY KEY`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"column_name"}).AddRow("id"))

	repo := NewSchemaRepository(mock)
	pks, err := repo.GetPrimaryKeys(context.Background(), "public", "users")
	require.NoError(t, err)
	assert.Equal(t, []string{"id"}, pks)
}

func TestSchemaRepository_GetForeignKeys_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT.*constraint_name.*FOREIGN KEY`).
		WithArgs("public", "orders").
		WillReturnRows(pgxmock.NewRows([]string{
			"constraint_name", "column_name", "foreign_table_schema", "foreign_table_name",
			"foreign_column_name", "update_rule", "delete_rule",
		}).AddRow("fk_user", "user_id", "public", "users", "id", "NO ACTION", "CASCADE"))

	repo := NewSchemaRepository(mock)
	fks, err := repo.GetForeignKeys(context.Background(), "public", "orders")
	require.NoError(t, err)
	require.Len(t, fks, 1)
	assert.Equal(t, "user_id", fks[0].FromColumn)
}

func TestSchemaRepository_ListSchemas_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT schema_name.*information_schema\.schemata`).
		WillReturnRows(pgxmock.NewRows([]string{"schema_name"}).AddRow("public"))

	repo := NewSchemaRepository(mock)
	names, err := repo.ListSchemas(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"public"}, names)
}

func TestSchemaRepository_GetColumns_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT column_name, data_type, is_nullable.*information_schema\.columns`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
			AddRow("email", "character varying", "YES"))

	repo := NewSchemaRepository(mock)
	cols, err := repo.GetColumns(context.Background(), "public", "users")
	require.NoError(t, err)
	require.Len(t, cols, 1)
	assert.True(t, cols[0].Nullable)
}

func TestSchemaRepository_GetUniqueConstraintsBatch_empty(t *testing.T) {
	repo := NewSchemaRepository(newRepoMock(t))
	m, err := repo.GetUniqueConstraintsBatch(context.Background(), "public", nil)
	require.NoError(t, err)
	assert.Empty(t, m)
}

func TestSchemaRepository_GetUniqueConstraintsBatch_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT DISTINCT tc\.table_name.*UNIQUE`).
		WithArgs("public", "users", "email", "public").
		WillReturnRows(pgxmock.NewRows([]string{"table_name", "column_name"}).AddRow("users", "email"))

	repo := NewSchemaRepository(mock)
	m, err := repo.GetUniqueConstraintsBatch(context.Background(), "public", []TableColumn{
		{Table: "users", Column: "email"},
	})
	require.NoError(t, err)
	assert.True(t, m["users:email"])
}

// --- Error branches for schema repository functions ---

func TestSchemaRepository_GetTables_scanError(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT table_name.*information_schema\.tables`).
		WithArgs("public").
		WillReturnRows(pgxmock.NewRows([]string{"table_name"}).AddRow("ok").RowError(0, errors.New("bad scan")))

	repo := NewSchemaRepository(mock)
	_, err := repo.GetTables(context.Background(), "public")
	require.Error(t, err)
}

func TestSchemaRepository_GetTables_rowsErr(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT table_name.*information_schema\.tables`).
		WithArgs("public").
		WillReturnRows(pgxmock.NewRows([]string{"table_name"}).AddRow("users").CloseError(errors.New("iter error")))

	repo := NewSchemaRepository(mock)
	_, err := repo.GetTables(context.Background(), "public")
	require.Error(t, err)
}

func TestSchemaRepository_GetColumns_queryError(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT column_name, data_type, is_nullable.*information_schema\.columns`).
		WithArgs("public", "users").
		WillReturnError(errors.New("query error"))

	repo := NewSchemaRepository(mock)
	_, err := repo.GetColumns(context.Background(), "public", "users")
	require.Error(t, err)
}

func TestSchemaRepository_GetColumns_scanError(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT column_name, data_type, is_nullable.*information_schema\.columns`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"c", "d", "n"}).AddRow("ok", "ok", "ok").RowError(0, errors.New("bad scan")))

	repo := NewSchemaRepository(mock)
	_, err := repo.GetColumns(context.Background(), "public", "users")
	require.Error(t, err)
}

func TestSchemaRepository_GetColumns_rowsErr(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT column_name, data_type, is_nullable.*information_schema\.columns`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"c", "d", "n"}).AddRow("email", "text", "YES").CloseError(errors.New("iter error")))

	repo := NewSchemaRepository(mock)
	_, err := repo.GetColumns(context.Background(), "public", "users")
	require.Error(t, err)
}

func TestSchemaRepository_GetColumnDetails_queryError(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT column_name, data_type.*information_schema\.columns`).
		WithArgs("public", "users").
		WillReturnError(errors.New("query error"))

	repo := NewSchemaRepository(mock)
	_, err := repo.GetColumnDetails(context.Background(), "public", "users")
	require.Error(t, err)
}

func TestSchemaRepository_ListSchemas_queryError(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT schema_name.*information_schema\.schemata`).
		WillReturnError(errors.New("query error"))

	repo := NewSchemaRepository(mock)
	_, err := repo.ListSchemas(context.Background())
	require.Error(t, err)
}

func TestSchemaRepository_GetColumnDetails_scanError(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT column_name, data_type.*information_schema\.columns`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{
			"column_name", "data_type", "udt_name", "character_maximum_length",
			"is_nullable", "column_default", "is_identity",
		}).AddRow("ok", "ok", "ok", nil, "ok", nil, false).RowError(0, errors.New("bad scan")))

	repo := NewSchemaRepository(mock)
	_, err := repo.GetColumnDetails(context.Background(), "public", "users")
	require.Error(t, err)
}

func TestSchemaRepository_GetColumnDetails_withOptionalFields(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT column_name, data_type.*information_schema\.columns`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{
			"column_name", "data_type", "udt_name", "character_maximum_length",
			"is_nullable", "column_default", "is_identity",
		}).AddRow("id", "integer", "int4", 32, "NO", "nextval('seq')", false))

	repo := NewSchemaRepository(mock)
	cols, err := repo.GetColumnDetails(context.Background(), "public", "users")
	require.NoError(t, err)
	require.Len(t, cols, 1)
	require.NotNil(t, cols[0].CharMaxLength)
	assert.Equal(t, 32, *cols[0].CharMaxLength)
	require.NotNil(t, cols[0].ColumnDefault)
	assert.Equal(t, "nextval('seq')", *cols[0].ColumnDefault)
}

func TestSchemaRepository_GetColumnDetails_rowsErr(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT column_name, data_type.*information_schema\.columns`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{
			"column_name", "data_type", "udt_name", "character_maximum_length",
			"is_nullable", "column_default", "is_identity",
		}).AddRow("id", "integer", "int4", nil, "YES", nil, false).CloseError(errors.New("iter error")))

	repo := NewSchemaRepository(mock)
	_, err := repo.GetColumnDetails(context.Background(), "public", "users")
	require.Error(t, err)
}

func TestSchemaRepository_GetPrimaryKeys_queryError(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT kcu\.column_name.*PRIMARY KEY`).
		WithArgs("public", "users").
		WillReturnError(errors.New("query error"))

	repo := NewSchemaRepository(mock)
	_, err := repo.GetPrimaryKeys(context.Background(), "public", "users")
	require.Error(t, err)
}

func TestSchemaRepository_GetPrimaryKeys_scanError(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT kcu\.column_name.*PRIMARY KEY`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"column_name"}).AddRow("ok").RowError(0, errors.New("bad scan")))

	repo := NewSchemaRepository(mock)
	_, err := repo.GetPrimaryKeys(context.Background(), "public", "users")
	require.Error(t, err)
}

func TestSchemaRepository_GetPrimaryKeys_rowsErr(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT kcu\.column_name.*PRIMARY KEY`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"column_name"}).AddRow("id").CloseError(errors.New("iter error")))

	repo := NewSchemaRepository(mock)
	_, err := repo.GetPrimaryKeys(context.Background(), "public", "users")
	require.Error(t, err)
}

func TestSchemaRepository_GetForeignKeys_queryError(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT.*constraint_name.*FOREIGN KEY`).
		WithArgs("public", "orders").
		WillReturnError(errors.New("query error"))

	repo := NewSchemaRepository(mock)
	_, err := repo.GetForeignKeys(context.Background(), "public", "orders")
	require.Error(t, err)
}

func TestSchemaRepository_GetForeignKeys_scanError(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT.*constraint_name.*FOREIGN KEY`).
		WithArgs("public", "orders").
		WillReturnRows(pgxmock.NewRows([]string{
			"constraint_name", "column_name", "foreign_table_schema", "foreign_table_name",
			"foreign_column_name", "update_rule", "delete_rule",
		}).AddRow("ok", "ok", "ok", "ok", "ok", "ok", "ok").RowError(0, errors.New("bad scan")))

	repo := NewSchemaRepository(mock)
	_, err := repo.GetForeignKeys(context.Background(), "public", "orders")
	require.Error(t, err)
}

func TestSchemaRepository_GetForeignKeys_rowsErr(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT.*constraint_name.*FOREIGN KEY`).
		WithArgs("public", "orders").
		WillReturnRows(pgxmock.NewRows([]string{
			"constraint_name", "column_name", "foreign_table_schema", "foreign_table_name",
			"foreign_column_name", "update_rule", "delete_rule",
		}).AddRow("fk", "uid", "public", "users", "id", "NO ACTION", "CASCADE").CloseError(errors.New("iter error")))

	repo := NewSchemaRepository(mock)
	_, err := repo.GetForeignKeys(context.Background(), "public", "orders")
	require.Error(t, err)
}

func TestSchemaRepository_ListSchemas_scanError(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT schema_name.*information_schema\.schemata`).
		WillReturnRows(pgxmock.NewRows([]string{"schema_name"}).AddRow("ok").RowError(0, errors.New("bad scan")))

	repo := NewSchemaRepository(mock)
	_, err := repo.ListSchemas(context.Background())
	require.Error(t, err)
}

func TestSchemaRepository_ListSchemas_rowsErr(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT schema_name.*information_schema\.schemata`).
		WillReturnRows(pgxmock.NewRows([]string{"schema_name"}).AddRow("public").CloseError(errors.New("iter error")))

	repo := NewSchemaRepository(mock)
	_, err := repo.ListSchemas(context.Background())
	require.Error(t, err)
}

func TestSchemaRepository_GetUniqueConstraintsBatch_queryError(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT DISTINCT`).
		WithArgs("public", "users", "email", "public").
		WillReturnError(errors.New("query error"))

	repo := NewSchemaRepository(mock)
	_, err := repo.GetUniqueConstraintsBatch(context.Background(), "public", []TableColumn{
		{Table: "users", Column: "email"},
	})
	require.Error(t, err)
}

func TestSchemaRepository_GetUniqueConstraintsBatch_scanError(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT DISTINCT`).
		WithArgs("public", "users", "email", "public").
		WillReturnRows(pgxmock.NewRows([]string{"table_name", "column_name"}).AddRow("ok", "ok").RowError(0, errors.New("bad scan")))

	repo := NewSchemaRepository(mock)
	_, err := repo.GetUniqueConstraintsBatch(context.Background(), "public", []TableColumn{
		{Table: "users", Column: "email"},
	})
	require.Error(t, err)
}

func TestSchemaRepository_GetUniqueConstraintsBatch_rowsErr(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectQuery(`(?s)SELECT DISTINCT`).
		WithArgs("public", "users", "email", "public").
		WillReturnRows(pgxmock.NewRows([]string{"table_name", "column_name"}).AddRow("users", "email").CloseError(errors.New("iter error")))

	repo := NewSchemaRepository(mock)
	_, err := repo.GetUniqueConstraintsBatch(context.Background(), "public", []TableColumn{
		{Table: "users", Column: "email"},
	})
	require.Error(t, err)
}
