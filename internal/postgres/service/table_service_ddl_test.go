package service

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"backend/internal/postgres/model"
	"backend/internal/postgres/repository"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockTablePoolSource struct {
	pool pgPoolRunner
	err  error
}

func (m mockTablePoolSource) TablePool(ctx context.Context, userID, projectID uuid.UUID) (pgPoolRunner, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.pool, nil
}

func tableSvcWithMock(t *testing.T, mock pgxmock.PgxPoolIface) *TableService {
	t.Helper()
	svc := NewTableService(stubInstanceConn{}, repository.NewTableRepository())
	svc.poolSource = mockTablePoolSource{pool: mock}
	return svc
}

func TestTableService_CreateTable_success(t *testing.T) {
	mock := newMockPool(t)
	req := &model.CreateTableRequest{
		Table:   "users",
		Columns: []model.TableColumnDef{{Name: "id", Type: "INTEGER", Primary: true}},
	}
	sql, err := repository.BuildCreateTableSQL(req)
	require.NoError(t, err)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(sql)).WillReturnResult(pgxmock.NewResult("CREATE TABLE", 0))
	mock.ExpectCommit()

	svc := tableSvcWithMock(t, mock)
	result, err := svc.CreateTable(context.Background(), req, uuid.New(), uuid.New())
	require.NoError(t, err)
	assert.Equal(t, int64(0), result.RowsAffected)
}

func TestTableService_CreateTable_duplicate(t *testing.T) {
	mock := newMockPool(t)
	req := &model.CreateTableRequest{
		Table:   "users",
		Columns: []model.TableColumnDef{{Name: "id", Type: "INTEGER", Primary: true}},
	}
	sql, err := repository.BuildCreateTableSQL(req)
	require.NoError(t, err)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(sql)).WillReturnError(&pgconn.PgError{Code: "42P07"})
	mock.ExpectRollback()

	svc := tableSvcWithMock(t, mock)
	_, err = svc.CreateTable(context.Background(), req, uuid.New(), uuid.New())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTableAlreadyExists)
}

func TestTableService_CreateTable_beginFails(t *testing.T) {
	mock := newMockPool(t)
	mock.ExpectBegin().WillReturnError(errors.New("begin failed"))

	svc := tableSvcWithMock(t, mock)
	_, err := svc.CreateTable(context.Background(), &model.CreateTableRequest{
		Table:   "t",
		Columns: []model.TableColumnDef{{Name: "id", Type: "INTEGER"}},
	}, uuid.New(), uuid.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start transaction")
}

func TestTableService_DeleteTable_success(t *testing.T) {
	mock := newMockPool(t)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE "public"."users" CASCADE`)).WillReturnResult(pgxmock.NewResult("DROP TABLE", 0))
	mock.ExpectCommit()

	svc := tableSvcWithMock(t, mock)
	result, err := svc.DeleteTable(context.Background(), &model.DeleteTableRequest{
		Schema: "public",
		Table:  "users",
	}, uuid.New(), uuid.New())
	require.NoError(t, err)
	assert.Equal(t, int64(0), result.RowsAffected)
}

func TestTableService_DeleteTable_notFound(t *testing.T) {
	mock := newMockPool(t)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE "public"."missing" CASCADE`)).WillReturnError(&pgconn.PgError{Code: "42P01"})
	mock.ExpectRollback()

	svc := tableSvcWithMock(t, mock)
	_, err := svc.DeleteTable(context.Background(), &model.DeleteTableRequest{
		Schema: "public",
		Table:  "missing",
	}, uuid.New(), uuid.New())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTableNotFound)
}

func TestTableService_poolSourceError(t *testing.T) {
	svc := NewTableService(stubInstanceConn{}, repository.NewTableRepository())
	svc.poolSource = mockTablePoolSource{err: errors.New("pool unavailable")}

	_, err := svc.CreateTable(context.Background(), &model.CreateTableRequest{
		Table:   "t",
		Columns: []model.TableColumnDef{{Name: "id", Type: "INTEGER"}},
	}, uuid.New(), uuid.New())
	require.Error(t, err)
	assert.EqualError(t, err, "pool unavailable")
}

func TestTableService_DeleteTable_genericExecError(t *testing.T) {
	mock := newMockPool(t)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE "public"."users" CASCADE`)).
		WillReturnError(errors.New("disk full"))
	mock.ExpectRollback()

	svc := tableSvcWithMock(t, mock)
	_, err := svc.DeleteTable(context.Background(), &model.DeleteTableRequest{
		Schema: "public",
		Table:  "users",
	}, uuid.New(), uuid.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete table")
	assert.Contains(t, err.Error(), "disk full")
}

func TestTableService_DeleteTable_beginError(t *testing.T) {
	mock := newMockPool(t)
	mock.ExpectBegin().WillReturnError(errors.New("connection lost"))

	svc := tableSvcWithMock(t, mock)
	_, err := svc.DeleteTable(context.Background(), &model.DeleteTableRequest{
		Schema: "public",
		Table:  "users",
	}, uuid.New(), uuid.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start transaction")
}

func TestTableService_DeleteTable_commitError(t *testing.T) {
	mock := newMockPool(t)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE "public"."users" CASCADE`)).
		WillReturnResult(pgxmock.NewResult("DROP TABLE", 0))
	mock.ExpectCommit().WillReturnError(errors.New("commit failed"))
	mock.ExpectRollback()

	svc := tableSvcWithMock(t, mock)
	_, err := svc.DeleteTable(context.Background(), &model.DeleteTableRequest{
		Schema: "public",
		Table:  "users",
	}, uuid.New(), uuid.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to commit transaction")
}

func TestTableService_AddColumn_poolError(t *testing.T) {
	svc := NewTableService(stubInstanceConn{}, repository.NewTableRepository())
	svc.poolSource = mockTablePoolSource{err: errors.New("pool unavailable")}

	_, err := svc.AddColumn(context.Background(), uuid.New(), uuid.New(), AddColumnRequest{
		Schema: "public", TableName: "users", Name: "x", Type: "TEXT",
	})
	require.Error(t, err)
	assert.EqualError(t, err, "pool unavailable")
}

func TestTableService_AddColumn_fkValidationErrors(t *testing.T) {
	svc := tableSvcWithMock(t, newMockPool(t))
	ctx := context.Background()

	t.Run("invalid FK schema", func(t *testing.T) {
		_, err := svc.AddColumn(ctx, uuid.New(), uuid.New(), AddColumnRequest{
			Schema: "public", TableName: "users", Name: "x", Type: "INTEGER",
			ForeignKeys: []model.AddColumnForeignKey{
				{Schema: "bad-schema", Table: "ref", ForeignColumn: "id"},
			},
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidTableRequest)
		assert.Contains(t, err.Error(), "foreign_keys[0]")
	})

	t.Run("invalid FK table", func(t *testing.T) {
		_, err := svc.AddColumn(ctx, uuid.New(), uuid.New(), AddColumnRequest{
			Schema: "public", TableName: "users", Name: "x", Type: "INTEGER",
			ForeignKeys: []model.AddColumnForeignKey{
				{Table: "bad-table", ForeignColumn: "id"},
			},
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidTableRequest)
	})

	t.Run("invalid FK foreign column", func(t *testing.T) {
		_, err := svc.AddColumn(ctx, uuid.New(), uuid.New(), AddColumnRequest{
			Schema: "public", TableName: "users", Name: "x", Type: "INTEGER",
			ForeignKeys: []model.AddColumnForeignKey{
				{Table: "ref", ForeignColumn: "bad col"},
			},
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidTableRequest)
	})
}

func TestTableService_UpdateTable_renameOnly(t *testing.T) {
	mock := newMockPool(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT 1
		FROM information_schema.tables
		WHERE table_schema = $1 AND table_name = $2 AND table_type = 'BASE TABLE'
		LIMIT 1`)).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"1"}).AddRow(1))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE "public"."users" RENAME TO "users_v2"`)).
		WillReturnResult(pgxmock.NewResult("ALTER TABLE", 1))
	mock.ExpectCommit()

	svc := tableSvcWithMock(t, mock)
	result, err := svc.UpdateTable(context.Background(), uuid.New(), uuid.New(), "public", "users", &model.UpdateTableRequest{Table: "users_v2"})
	require.NoError(t, err)
	assert.Greater(t, result.RowsAffected, int64(0))
}

func TestTableService_UpdateTable_schemaMove(t *testing.T) {
	mock := newMockPool(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT 1
		FROM information_schema.tables
		WHERE table_schema = $1 AND table_name = $2 AND table_type = 'BASE TABLE'
		LIMIT 1`)).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"1"}).AddRow(1))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE "public"."users" SET SCHEMA "app"`)).
		WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))
	mock.ExpectCommit()

	svc := tableSvcWithMock(t, mock)
	result, err := svc.UpdateTable(context.Background(), uuid.New(), uuid.New(), "public", "users", &model.UpdateTableRequest{Schema: "app"})
	require.NoError(t, err)
	assert.Greater(t, result.RowsAffected, int64(0))
}

func TestTableService_UpdateTable_noChanges(t *testing.T) {
	svc := tableSvcWithMock(t, newMockPool(t))
	_, err := svc.UpdateTable(context.Background(), uuid.New(), uuid.New(), "public", "users", &model.UpdateTableRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidTableRequest)
	assert.Contains(t, err.Error(), "no changes")
}

func TestTableService_UpdateTable_emptyColumnsList(t *testing.T) {
	svc := tableSvcWithMock(t, newMockPool(t))
	_, err := svc.UpdateTable(context.Background(), uuid.New(), uuid.New(), "public", "users", &model.UpdateTableRequest{Columns: []model.TableColumnDef{}})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidTableRequest)
	assert.Contains(t, err.Error(), "empty columns list")
}

func TestTableService_UpdateTable_FKOnlySync_removeAll(t *testing.T) {
	mock := newMockPool(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT 1
		FROM information_schema.tables
		WHERE table_schema = $1 AND table_name = $2 AND table_type = 'BASE TABLE'
		LIMIT 1`)).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"1"}).AddRow(1))

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT.*constraint_name.*FROM information_schema.table_constraints.*FOREIGN KEY.*`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"constraint_name", "column_name", "foreign_table_schema", "foreign_table_name", "foreign_column_name", "update_rule", "delete_rule"}).
			AddRow("fk_users_ref_id", "ref_id", "public", "ref_table", "id", "NO ACTION", "NO ACTION"))

	mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE "public"."users" DROP CONSTRAINT "fk_users_ref_id"`)).
		WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))
	mock.ExpectCommit()

	svc := tableSvcWithMock(t, mock)
	result, err := svc.UpdateTable(context.Background(), uuid.New(), uuid.New(), "public", "users", &model.UpdateTableRequest{
		ForeignKeys: &model.TableForeignKeyDef{References: []model.ForeignKeyRef{}},
	})
	require.NoError(t, err)
	assert.Greater(t, result.RowsAffected, int64(0))
}

func TestTableService_UpdateTable_columnTypeChange(t *testing.T) {
	mock := newMockPool(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT 1
		FROM information_schema.tables
		WHERE table_schema = $1 AND table_name = $2 AND table_type = 'BASE TABLE'
		LIMIT 1`)).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"1"}).AddRow(1))
	mock.ExpectBegin()

	mock.ExpectQuery(`(?s)SELECT column_name, data_type.*FROM information_schema.columns.*`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"column_name", "data_type", "udt_name", "character_maximum_length", "is_nullable", "column_default", "is_identity"}).
			AddRow("id", "integer", "int4", nil, "NO", nil, false))

	mock.ExpectQuery(`(?s)SELECT kcu.column_name.*FROM information_schema.table_constraints.*PRIMARY KEY.*`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"column_name"}))

	mock.ExpectQuery(`(?s)SELECT DISTINCT.*FROM information_schema.table_constraints.*UNIQUE.*`).
		WithArgs("public", "users", "id", "public").
		WillReturnRows(pgxmock.NewRows([]string{"table_name", "column_name"}))

	mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE "public"."users" ALTER COLUMN "id" TYPE TEXT USING "id"::TEXT`)).
		WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))
	mock.ExpectCommit()

	svc := tableSvcWithMock(t, mock)
	result, err := svc.UpdateTable(context.Background(), uuid.New(), uuid.New(), "public", "users", &model.UpdateTableRequest{
		Columns: []model.TableColumnDef{{Name: "id", Type: "TEXT"}},
	})
	require.NoError(t, err)
	assert.Greater(t, result.RowsAffected, int64(0))
}

func TestTableService_UpdateTable_tableNotFound(t *testing.T) {
	mock := newMockPool(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT 1
		FROM information_schema.tables
		WHERE table_schema = $1 AND table_name = $2 AND table_type = 'BASE TABLE'
		LIMIT 1`)).
		WithArgs("public", "users").
		WillReturnError(pgx.ErrNoRows)

	svc := tableSvcWithMock(t, mock)
	_, err := svc.UpdateTable(context.Background(), uuid.New(), uuid.New(), "public", "users", &model.UpdateTableRequest{Table: "users_v2"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTableNotFound)
}

// --- Pure helper unit tests --------------------------------------------------
// NOTE: these test the actual production signatures, which differ from the
// task brief. normalizedTypeFromPGDetail/typesCompatibleForUpdate take a
// model.ColumnDetail (not bare strings), defaultsComparable takes two *string
// (not interface{}), and generateFKConstraintName takes (table, localCol,
// refTable, seq). Behavior is asserted as the code actually behaves.

func detailType(dt string) model.ColumnDetail { return model.ColumnDetail{DataType: dt} }

func TestNormalizedTypeFromPGDetail(t *testing.T) {
	tests := []struct {
		name     string
		dataType string
		want     string
	}{
		{"integer", "integer", "INTEGER"},
		{"bigint", "bigint", "BIGINT"},
		{"smallint", "smallint", "SMALLINT"},
		{"character varying", "character varying", "VARCHAR"},
		{"text", "text", "TEXT"},
		{"boolean", "boolean", "BOOLEAN"},
		{"double precision", "double precision", "DOUBLE PRECISION"},
		{"timestamp without tz", "timestamp without time zone", "TIMESTAMP"},
		{"timestamp with tz", "timestamp with time zone", "TIMESTAMPTZ"},
		{"uuid", "uuid", "UUID"},
		{"jsonb", "jsonb", "JSONB"},
		{"json", "json", "JSON"},
		{"date", "date", "DATE"},
		{"numeric", "numeric", "NUMERIC"},
		{"case/space normalized input", "  Integer ", "INTEGER"},
		{"unknown collapses spaces", "my custom type", "MYCUSTOMTYPE"},
		{"unknown single word", "citext", "CITEXT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizedTypeFromPGDetail(detailType(tt.dataType)))
		})
	}
}

func TestPhysicalTypeFromDetail(t *testing.T) {
	maxLen := 255
	charLen := 10
	tests := []struct {
		name   string
		detail model.ColumnDetail
		want   string
	}{
		{"varchar with length", model.ColumnDetail{DataType: "character varying", CharMaxLength: &maxLen}, "VARCHAR(255)"},
		{"varchar without length", model.ColumnDetail{DataType: "character varying"}, "VARCHAR"},
		{"char with length", model.ColumnDetail{DataType: "character", CharMaxLength: &charLen}, "CHAR(10)"},
		{"char without length", model.ColumnDetail{DataType: "character"}, "CHAR"},
		{"falls through to normalized", model.ColumnDetail{DataType: "integer"}, "INTEGER"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, physicalTypeFromDetail(tt.detail))
		})
	}
}

func TestNormalizeUserColumnType(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"int alias", "int", "INTEGER"},
		{"int4 alias", "int4", "INTEGER"},
		{"int8 alias", "int8", "BIGINT"},
		{"int2 alias", "int2", "SMALLINT"},
		{"bool alias", "bool", "BOOLEAN"},
		{"decimal alias", "decimal", "NUMERIC"},
		{"varchar with length strips parens", "varchar(100)", "VARCHAR"},
		{"already uppercase unchanged", "INTEGER", "INTEGER"},
		{"trimmed and uppercased", "  text ", "TEXT"},
		{"array suffix is not stripped", "integer[]", "INTEGER[]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeUserColumnType(tt.in))
		})
	}
}

func TestDefaultsComparable(t *testing.T) {
	tests := []struct {
		name string
		req  *string
		pg   *string
		want bool
	}{
		{"both nil", nil, nil, true},
		{"req empty pg nil", strPtr("  "), nil, true},
		{"req nil pg set", nil, strPtr("5"), false},
		{"req set pg nil", strPtr("5"), nil, false},
		{"string with cast", strPtr("hello"), strPtr("'hello'::text"), true},
		{"numeric equal", strPtr("42"), strPtr("42"), true},
		{"bool equal", strPtr("true"), strPtr("true"), true},
		{"mismatched numbers", strPtr("5"), strPtr("6"), false},
		{"mismatched strings", strPtr("hello"), strPtr("'world'::text"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, defaultsComparable(tt.req, tt.pg))
		})
	}
}

func TestTypesCompatibleForUpdate(t *testing.T) {
	varcharLen := 255
	tests := []struct {
		name    string
		reqType string
		detail  model.ColumnDetail
		want    bool
	}{
		{"identical normalized", "INTEGER", detailType("integer"), true},
		{"alias normalizes equal", "INT", detailType("integer"), true},
		{"sized varchar exact match", "VARCHAR(255)", model.ColumnDetail{DataType: "character varying", CharMaxLength: &varcharLen}, true},
		{"incompatible integer vs uuid", "INTEGER", detailType("uuid"), false},
		// TEXT and VARCHAR do NOT normalize to the same type, so this is false
		// (contrary to the task brief's assumption of a compatibility list).
		{"text vs varchar not compatible", "TEXT", detailType("character varying"), false},
		{"differing sizes not compatible", "VARCHAR(100)", model.ColumnDetail{DataType: "character varying", CharMaxLength: &varcharLen}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, typesCompatibleForUpdate(tt.reqType, tt.detail))
		})
	}
}

func TestGenerateFKConstraintName(t *testing.T) {
	name := generateFKConstraintName("orders", "user_id", "users", 1)
	assert.NotEmpty(t, name)
	assert.Contains(t, name, "orders")
	assert.Contains(t, name, "user_id")
	assert.Contains(t, name, "users")
	assert.Equal(t, "fk_orders_user_id_users_1", name)

	// Deterministic: same inputs -> same output.
	assert.Equal(t, name, generateFKConstraintName("orders", "user_id", "users", 1))

	// Stays within the PostgreSQL 63-char identifier limit even for long inputs.
	long := generateFKConstraintName(
		"a_very_long_table_name_that_exceeds_limits",
		"another_long_local_column_name",
		"a_long_referenced_table_name",
		42,
	)
	assert.LessOrEqual(t, len(long), 63)
}

func TestFKEdgeKey(t *testing.T) {
	// Uppercases all parts and normalizes the FK rules (RESTRICT -> NO ACTION).
	key := fkEdgeKey("user_id", "public", "users", "id", "cascade", "restrict")
	assert.Equal(t, "USER_ID|PUBLIC|USERS|ID|CASCADE|NO ACTION", key)

	// Case-insensitive: differing input case yields the same key (deterministic).
	assert.Equal(t, key, fkEdgeKey("USER_ID", "PUBLIC", "USERS", "ID", "CASCADE", "RESTRICT"))
}

func TestNormalizeFKRule(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"no action", "NO ACTION", "NO ACTION"},
		{"cascade", "CASCADE", "CASCADE"},
		{"empty defaults to no action", "", "NO ACTION"},
		{"restrict defaults to no action", "RESTRICT", "NO ACTION"},
		{"lowercase uppercased", "set null", "SET NULL"},
		{"trimmed and uppercased", "  cascade  ", "CASCADE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeFKRule(tt.in))
		})
	}
}

func strPtr(s string) *string { return &s }
