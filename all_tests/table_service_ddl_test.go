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
