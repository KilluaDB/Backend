package service

import (
	"context"
	"errors"
	"testing"

	"backend/internal/postgres/model"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An invalid schema name must be rejected by every row/table method before any
// pool access. "bad schema" has a space, so it fails isValidIdentifier.
func TestTableService_RowMethods_invalidSchema(t *testing.T) {
	svc := tableSvcWithRowsMock(t, newTableRowsMockPool(t))
	ctx := context.Background()
	u, p := uuid.New(), uuid.New()
	bad := "bad schema"

	cases := []struct {
		name string
		call func() error
	}{
		{"GetTables", func() error { _, e := svc.GetTables(ctx, p, u, bad); return e }},
		{"GetTableMetadata", func() error { _, e := svc.GetTableMetadata(ctx, p, u, bad, "users"); return e }},
		{"GetRows", func() error { _, e := svc.GetRows(ctx, p, u, bad, "users", nil, 1, 0, false); return e }},
		{"UpdateRows", func() error {
			return svc.UpdateRows(ctx, p, u, bad, "users", nil, map[string]interface{}{"email": "x"})
		}},
		{"DeleteRowsByFilter", func() error { return svc.DeleteRowsByFilter(ctx, u, p, bad, "users", nil) }},
		{"InsertRow", func() error {
			_, e := svc.InsertRow(ctx, u, p, InsertRowRequest{Schema: bad, Table: "users", Values: map[string]interface{}{"a": 1}})
			return e
		}},
		{"AddColumn", func() error {
			_, e := svc.AddColumn(ctx, u, p, AddColumnRequest{Schema: bad, TableName: "users", Name: "c", Type: "TEXT"})
			return e
		}},
		{"DeleteColumn", func() error {
			return svc.DeleteColumn(ctx, u, p, DeleteColumnRequest{Schema: bad, TableName: "users"}, "col")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Error(t, c.call())
		})
	}
}

// A valid schema but invalid table name must be rejected before pool access.
func TestTableService_RowMethods_invalidTable(t *testing.T) {
	svc := tableSvcWithRowsMock(t, newTableRowsMockPool(t))
	ctx := context.Background()
	u, p := uuid.New(), uuid.New()
	badTable := "bad table"

	cases := []struct {
		name string
		call func() error
	}{
		{"GetTableMetadata", func() error { _, e := svc.GetTableMetadata(ctx, p, u, "public", badTable); return e }},
		{"GetRows", func() error { _, e := svc.GetRows(ctx, p, u, "public", badTable, nil, 1, 0, false); return e }},
		{"UpdateRows", func() error {
			return svc.UpdateRows(ctx, p, u, "public", badTable, nil, map[string]interface{}{"email": "x"})
		}},
		{"DeleteRowsByFilter", func() error { return svc.DeleteRowsByFilter(ctx, u, p, "public", badTable, nil) }},
		{"InsertRow", func() error {
			_, e := svc.InsertRow(ctx, u, p, InsertRowRequest{Schema: "public", Table: badTable, Values: map[string]interface{}{"a": 1}})
			return e
		}},
		{"AddColumn", func() error {
			_, e := svc.AddColumn(ctx, u, p, AddColumnRequest{Schema: "public", TableName: badTable, Name: "c", Type: "TEXT"})
			return e
		}},
		{"DeleteColumn", func() error {
			return svc.DeleteColumn(ctx, u, p, DeleteColumnRequest{Schema: "public", TableName: badTable}, "col")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Error(t, c.call())
		})
	}
}

func TestTableService_UpdateRows_invalidColumns(t *testing.T) {
	svc := tableSvcWithRowsMock(t, newTableRowsMockPool(t))
	ctx := context.Background()
	u, p := uuid.New(), uuid.New()

	t.Run("invalid update column", func(t *testing.T) {
		err := svc.UpdateRows(ctx, p, u, "public", "users", nil, map[string]interface{}{"bad col": 1})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid column name")
	})

	t.Run("invalid filter column", func(t *testing.T) {
		err := svc.UpdateRows(ctx, p, u, "public", "users",
			map[string]interface{}{"bad col": 1}, map[string]interface{}{"email": "x"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid column name")
	})
}

func TestTableService_DeleteColumn_invalidColumnName(t *testing.T) {
	svc := tableSvcWithRowsMock(t, newTableRowsMockPool(t))
	// Valid table, invalid column name -> hits the columnName validator branch.
	err := svc.DeleteColumn(context.Background(), uuid.New(), uuid.New(),
		DeleteColumnRequest{Schema: "public", TableName: "users"}, "bad col")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid column name")
}

func TestTableService_AddColumn_invalidForeignKeys(t *testing.T) {
	svc := tableSvcWithRowsMock(t, newTableRowsMockPool(t))
	ctx := context.Background()
	u, p := uuid.New(), uuid.New()

	base := func(fk model.AddColumnForeignKey) AddColumnRequest {
		return AddColumnRequest{
			Schema: "public", TableName: "users", Name: "ref_id", Type: "INTEGER",
			ForeignKeys: []model.AddColumnForeignKey{fk},
		}
	}

	cases := []struct {
		name string
		fk   model.AddColumnForeignKey
	}{
		{"invalid fk schema", model.AddColumnForeignKey{Schema: "bad schema", Table: "depts", ForeignColumn: "id"}},
		{"invalid fk table", model.AddColumnForeignKey{Schema: "public", Table: "bad table", ForeignColumn: "id"}},
		{"invalid fk column", model.AddColumnForeignKey{Schema: "public", Table: "depts", ForeignColumn: "bad col"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := svc.AddColumn(ctx, u, p, base(c.fk))
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalidTableRequest)
		})
	}
}

func TestSqlLiteralFromDefault_numeric(t *testing.T) {
	// Non-string, non-bool values take the default fmt.Sprintf branch.
	got := sqlLiteralFromDefault(42)
	require.NotNil(t, got)
	assert.Equal(t, "42", *got)

	f := sqlLiteralFromDefault(3.5)
	require.NotNil(t, f)
	assert.Equal(t, "3.5", *f)
}

func TestTableService_GetRows_selectError(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectQuery(`SELECT \* FROM "public"\."users" LIMIT \$1 OFFSET \$2`).
		WithArgs(2, 0).
		WillReturnError(errors.New("select boom"))

	svc := tableSvcWithRowsMock(t, mock)
	_, err := svc.GetRows(context.Background(), uuid.New(), uuid.New(), "public", "users", nil, 1, 0, false)
	require.Error(t, err)
}

func TestTableService_GetRows_countError(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectQuery(`SELECT \* FROM "public"\."users" LIMIT \$1 OFFSET \$2`).
		WithArgs(2, 0).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM "public"\."users"`).
		WillReturnError(errors.New("count boom"))

	svc := tableSvcWithRowsMock(t, mock)
	_, err := svc.GetRows(context.Background(), uuid.New(), uuid.New(), "public", "users", nil, 1, 0, true)
	require.Error(t, err)
}

func TestTableService_InsertRow_repoError(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectQuery(`(?s)SELECT EXISTS.*information_schema\.columns`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"exists", "data_type"}).AddRow(false, ""))
	mock.ExpectExec(`INSERT INTO "public"\."users"`).
		WithArgs("a@test.com").
		WillReturnError(errors.New("insert boom"))

	svc := tableSvcWithRowsMock(t, mock)
	_, err := svc.InsertRow(context.Background(), uuid.New(), uuid.New(), InsertRowRequest{
		Schema: "public", Table: "users", Values: map[string]interface{}{"email": "a@test.com"},
	})
	require.Error(t, err)
}

func TestTableService_DeleteColumn_repoError(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" DROP COLUMN "nickname"`).
		WillReturnError(errors.New("drop boom"))

	svc := tableSvcWithRowsMock(t, mock)
	err := svc.DeleteColumn(context.Background(), uuid.New(), uuid.New(),
		DeleteColumnRequest{Schema: "public", TableName: "users"}, "nickname")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete column")
}

func TestTableService_GetTableMetadata_columnDetailsError(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectQuery(`(?s)SELECT 1.*information_schema\.tables`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"?column?"}).AddRow(1))
	mock.ExpectQuery(`(?s)SELECT column_name, data_type.*information_schema\.columns`).
		WithArgs("public", "users").
		WillReturnError(errors.New("columns boom"))

	svc := tableSvcWithRowsMock(t, mock)
	_, err := svc.GetTableMetadata(context.Background(), uuid.New(), uuid.New(), "public", "users")
	require.Error(t, err)
}

func TestTableService_AddColumn_tableExistsError(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectQuery(`(?s)SELECT 1.*information_schema\.tables`).
		WithArgs("public", "users").
		WillReturnError(errors.New("exists boom"))

	svc := tableSvcWithRowsMock(t, mock)
	_, err := svc.AddColumn(context.Background(), uuid.New(), uuid.New(), AddColumnRequest{
		Schema: "public", TableName: "users", Name: "nickname", Type: "TEXT",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to check table")
}
