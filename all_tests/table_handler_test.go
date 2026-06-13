package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"backend/internal/postgres/model"
	"backend/internal/postgres/repository"
	pgservice "backend/internal/postgres/service"
	"backend/internal/service"
	"backend/internal/testutil"
	"backend/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type staticTablePoolSource struct {
	pool pgservice.TablePoolRunner
	err  error
}

func (s staticTablePoolSource) TablePool(ctx context.Context, userID, projectID uuid.UUID) (pgservice.TablePoolRunner, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.pool, nil
}

func newTableHandlerMockPool(t *testing.T) pgxmock.PgxPoolIface {
	t.Helper()
	mock, err := pgxmock.NewPool(pgxmock.QueryMatcherOption(pgxmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, mock.ExpectationsWereMet())
		mock.Close()
	})
	return mock
}

func newTableHandler(t *testing.T, mock pgxmock.PgxPoolIface) *TableHandler {
	t.Helper()
	svc := pgservice.NewTableService(nil, repository.NewTableRepository())
	svc.SetPoolSourceForTest(staticTablePoolSource{pool: mock})
	return NewTableHandler(svc)
}

func tableCtx(method, path string, uid, pid uuid.UUID, body any, params gin.Params) (*gin.Context, *httptest.ResponseRecorder) {
	c, w := testutil.NewGinContext(method, path, body, nil)
	if uid != uuid.Nil {
		c.Set(utils.UserIDContextKey, uid)
	}
	c.Params = params
	return c, w
}

func TestTableHandler_CreateTable_unauthorized(t *testing.T) {
	h := NewTableHandler(pgservice.NewTableService(nil, repository.NewTableRepository()))
	c, w := tableCtx(http.MethodPost, "/", uuid.Nil, uuid.New(), map[string]any{"table": "t"}, gin.Params{{Key: "id", Value: uuid.New().String()}})
	h.CreateTable(c)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestTableHandler_CreateTable_success(t *testing.T) {
	mock := newTableHandlerMockPool(t)
	req := &model.CreateTableRequest{
		Table:   "users",
		Columns: []model.TableColumnDef{{Name: "id", Type: "INTEGER", Primary: true}},
	}
	_ = req
	mock.ExpectBegin()
	mock.ExpectExec(`(?s)CREATE TABLE.*users`).WillReturnResult(pgxmock.NewResult("CREATE TABLE", 0))
	mock.ExpectCommit()

	h := newTableHandler(t, mock)
	pid := uuid.New()
	c, w := tableCtx(http.MethodPost, "/", uuid.New(), pid, req, gin.Params{{Key: "id", Value: pid.String()}})
	h.CreateTable(c)
	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestTableHandler_CreateTable_validationError(t *testing.T) {
	h := newTableHandler(t, newTableHandlerMockPool(t))
	pid := uuid.New()
	c, w := tableCtx(http.MethodPost, "/", uuid.New(), pid, map[string]any{"table": "bad table"}, gin.Params{{Key: "id", Value: pid.String()}})
	h.CreateTable(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTableHandler_GetTables_success(t *testing.T) {
	mock := newTableHandlerMockPool(t)
	mock.ExpectQuery(`(?s)SELECT table_name.*information_schema\.tables`).
		WithArgs("public").
		WillReturnRows(pgxmock.NewRows([]string{"table_name"}).AddRow("users"))

	h := newTableHandler(t, mock)
	pid := uuid.New()
	c, w := tableCtx(http.MethodGet, "/?schema=public", uuid.New(), pid, nil, gin.Params{{Key: "id", Value: pid.String()}})
	h.GetTables(c)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestTableHandler_GetTables_invalidSchema(t *testing.T) {
	h := newTableHandler(t, newTableHandlerMockPool(t))
	pid := uuid.New()
	c, w := tableCtx(http.MethodGet, "/?schema=bad-schema", uuid.New(), pid, nil, gin.Params{{Key: "id", Value: pid.String()}})
	h.GetTables(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTableHandler_GetTable_success(t *testing.T) {
	mock := newTableHandlerMockPool(t)
	mock.ExpectQuery(`(?s)SELECT 1.*information_schema\.tables`).WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"?column?"}).AddRow(1))
	mock.ExpectQuery(`(?s)SELECT column_name, data_type.*information_schema\.columns`).WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{
			"column_name", "data_type", "udt_name", "character_maximum_length",
			"is_nullable", "column_default", "is_identity",
		}).AddRow("id", "integer", "int4", nil, "NO", nil, false))
	mock.ExpectQuery(`(?s)SELECT kcu\.column_name.*PRIMARY KEY`).WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"column_name"}).AddRow("id"))
	mock.ExpectQuery(`(?s)SELECT.*FOREIGN KEY`).WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{
			"constraint_name", "column_name", "foreign_table_schema", "foreign_table_name",
			"foreign_column_name", "update_rule", "delete_rule",
		}))

	h := newTableHandler(t, mock)
	pid := uuid.New()
	c, w := tableCtx(http.MethodGet, "/?schema=public", uuid.New(), pid, nil,
		gin.Params{{Key: "id", Value: pid.String()}, {Key: "table", Value: "users"}})
	h.GetTable(c)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestTableHandler_GetTable_missingTableParam(t *testing.T) {
	h := newTableHandler(t, newTableHandlerMockPool(t))
	pid := uuid.New()
	c, w := tableCtx(http.MethodGet, "/", uuid.New(), pid, nil, gin.Params{{Key: "id", Value: pid.String()}})
	h.GetTable(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTableHandler_DeleteTable_success(t *testing.T) {
	mock := newTableHandlerMockPool(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DROP TABLE "public"."users" CASCADE`).WillReturnResult(pgxmock.NewResult("DROP TABLE", 0))
	mock.ExpectCommit()

	h := newTableHandler(t, mock)
	pid := uuid.New()
	c, w := tableCtx(http.MethodDelete, "/", uuid.New(), pid,
		map[string]string{"schema": "public", "table": "users"},
		gin.Params{{Key: "id", Value: pid.String()}})
	h.DeleteTable(c)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestTableHandler_DeleteTable_notFound(t *testing.T) {
	mock := newTableHandlerMockPool(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DROP TABLE "public"."ghost" CASCADE`).WillReturnError(&pgconn.PgError{Code: "42P01"})
	mock.ExpectRollback()

	h := newTableHandler(t, mock)
	pid := uuid.New()
	c, w := tableCtx(http.MethodDelete, "/", uuid.New(), pid,
		map[string]string{"schema": "public", "table": "ghost"},
		gin.Params{{Key: "id", Value: pid.String()}})
	h.DeleteTable(c)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestTableHandler_UpdateTable_success(t *testing.T) {
	mock := newTableHandlerMockPool(t)
	mock.ExpectQuery(`(?s)SELECT 1.*information_schema\.tables`).WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"?column?"}).AddRow(1))
	mock.ExpectBegin()
	mock.ExpectExec(`ALTER TABLE "public"\."users" RENAME TO "users_new"`).
		WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))
	mock.ExpectCommit()

	h := newTableHandler(t, mock)
	pid := uuid.New()
	c, w := tableCtx(http.MethodPatch, "/?schema=public", uuid.New(), pid,
		map[string]string{"table": "users_new"},
		gin.Params{{Key: "id", Value: pid.String()}, {Key: "table", Value: "users"}})
	h.UpdateTable(c)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestTableHandler_InsertRow_success(t *testing.T) {
	mock := newTableHandlerMockPool(t)
	mock.ExpectQuery(`(?s)SELECT EXISTS.*information_schema\.columns`).WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"exists", "data_type"}).AddRow(false, ""))
	mock.ExpectExec(`(?s)INSERT INTO "public"\."users"`).WithArgs("a@test.com").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	h := newTableHandler(t, mock)
	pid := uuid.New()
	c, w := tableCtx(http.MethodPost, "/?schema=public", uuid.New(), pid,
		map[string]any{"values": map[string]any{"email": "a@test.com"}},
		gin.Params{{Key: "id", Value: pid.String()}, {Key: "table", Value: "users"}})
	h.InsertRow(c)
	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestTableHandler_GetRows_success(t *testing.T) {
	mock := newTableHandlerMockPool(t)
	mock.ExpectQuery(`(?s)SELECT \* FROM.*users.*LIMIT`).WithArgs(11, 0).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(1))

	h := newTableHandler(t, mock)
	pid := uuid.New()
	c, w := tableCtx(http.MethodGet, "/?schema=public&limit=10", uuid.New(), pid, nil,
		gin.Params{{Key: "id", Value: pid.String()}, {Key: "table", Value: "users"}})
	h.GetRows(c)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestTableHandler_GetRows_invalidLimit(t *testing.T) {
	h := newTableHandler(t, newTableHandlerMockPool(t))
	pid := uuid.New()
	c, w := tableCtx(http.MethodGet, "/?schema=public&limit=0", uuid.New(), pid, nil,
		gin.Params{{Key: "id", Value: pid.String()}, {Key: "table", Value: "users"}})
	h.GetRows(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTableHandler_UpdateRows_success(t *testing.T) {
	mock := newTableHandlerMockPool(t)
	mock.ExpectExec(`(?s)UPDATE.*users.*SET`).WithArgs("new@test.com").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	h := newTableHandler(t, mock)
	pid := uuid.New()
	c, w := tableCtx(http.MethodPatch, "/?schema=public", uuid.New(), pid,
		map[string]any{"update": map[string]any{"email": "new@test.com"}},
		gin.Params{{Key: "id", Value: pid.String()}, {Key: "table", Value: "users"}})
	h.UpdateRows(c)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestTableHandler_DeleteRows_success(t *testing.T) {
	mock := newTableHandlerMockPool(t)
	mock.ExpectExec(`(?s)DELETE FROM.*users`).
		WillReturnResult(pgxmock.NewResult("DELETE", 2))

	h := newTableHandler(t, mock)
	pid := uuid.New()
	c, _ := tableCtx(http.MethodDelete, "/?schema=public", uuid.New(), pid, nil,
		gin.Params{{Key: "id", Value: pid.String()}, {Key: "table", Value: "users"}})
	h.DeleteRows(c)
	assert.Equal(t, http.StatusNoContent, c.Writer.Status())
}

func TestTableHandler_AddColumn_success(t *testing.T) {
	mock := newTableHandlerMockPool(t)
	mock.ExpectQuery(`(?s)SELECT 1.*information_schema\.tables`).WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"?column?"}).AddRow(1))
	mock.ExpectQuery(`(?s)SELECT kcu\.column_name.*PRIMARY KEY`).WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"column_name"}))
	mock.ExpectBegin()
	mock.ExpectExec(`ALTER TABLE "public"\."users" ADD COLUMN "bio" TEXT`).
		WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))
	mock.ExpectQuery(`(?s)SELECT ordinal_position`).WithArgs("public", "users", "bio").
		WillReturnRows(pgxmock.NewRows([]string{"ordinal_position"}).AddRow(int64(2)))
	mock.ExpectCommit()

	h := newTableHandler(t, mock)
	pid := uuid.New()
	c, w := tableCtx(http.MethodPost, "/?schema=public", uuid.New(), pid,
		map[string]any{"name": "bio", "type": "TEXT"},
		gin.Params{{Key: "id", Value: pid.String()}, {Key: "table", Value: "users"}})
	h.AddColumn(c)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestTableHandler_DropColumn_success(t *testing.T) {
	mock := newTableHandlerMockPool(t)
	mock.ExpectExec(`(?s)ALTER TABLE.*DROP COLUMN "bio"`).
		WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))

	h := newTableHandler(t, mock)
	pid := uuid.New()
	c, _ := tableCtx(http.MethodDelete, "/?schema=public", uuid.New(), pid, nil,
		gin.Params{{Key: "id", Value: pid.String()}, {Key: "table", Value: "users"}, {Key: "column", Value: "bio"}})
	h.DropColumn(c)
	assert.Equal(t, http.StatusNoContent, c.Writer.Status())
}

func TestTableHandler_ListIndexes_success(t *testing.T) {
	mock := newTableHandlerMockPool(t)
	mock.ExpectQuery(`(?s)SELECT i\.relname.*pg_class t`).WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{
			"relname", "indisunique", "indisprimary", "amname", "indexdef", "indisvalid",
		}).AddRow("idx_email", true, false, "btree", "def", true))

	h := newTableHandler(t, mock)
	pid := uuid.New()
	c, w := tableCtx(http.MethodGet, "/?schema=public", uuid.New(), pid, nil,
		gin.Params{{Key: "id", Value: pid.String()}, {Key: "table", Value: "users"}})
	h.ListIndexes(c)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestTableHandler_CreateIndex_success(t *testing.T) {
	mock := newTableHandlerMockPool(t)
	mock.ExpectExec(`(?s)CREATE INDEX "idx_email"`).
		WillReturnResult(pgxmock.NewResult("CREATE INDEX", 0))

	h := newTableHandler(t, mock)
	pid := uuid.New()
	c, w := tableCtx(http.MethodPost, "/?schema=public", uuid.New(), pid,
		map[string]any{"name": "idx_email", "columns": []string{"email"}},
		gin.Params{{Key: "id", Value: pid.String()}, {Key: "table", Value: "users"}})
	h.CreateIndex(c)
	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestTableHandler_DropIndex_success(t *testing.T) {
	mock := newTableHandlerMockPool(t)
	mock.ExpectQuery(`(?s)SELECT ix\.indisprimary`).WithArgs("public", "users", "idx_email").
		WillReturnRows(pgxmock.NewRows([]string{"indisprimary"}).AddRow(false))
	mock.ExpectExec(`(?s)DROP INDEX.*idx_email`).
		WillReturnResult(pgxmock.NewResult("DROP INDEX", 0))

	h := newTableHandler(t, mock)
	pid := uuid.New()
	c, _ := tableCtx(http.MethodDelete, "/?schema=public", uuid.New(), pid, nil,
		gin.Params{{Key: "id", Value: pid.String()}, {Key: "table", Value: "users"}, {Key: "index", Value: "idx_email"}})
	h.DropIndex(c)
	assert.Equal(t, http.StatusNoContent, c.Writer.Status())
}

func TestTableHandler_DropIndex_notFound(t *testing.T) {
	mock := newTableHandlerMockPool(t)
	mock.ExpectQuery(`(?s)SELECT ix\.indisprimary`).WithArgs("public", "users", "missing").
		WillReturnError(pgx.ErrNoRows)

	h := newTableHandler(t, mock)
	pid := uuid.New()
	c, w := tableCtx(http.MethodDelete, "/?schema=public", uuid.New(), pid, nil,
		gin.Params{{Key: "id", Value: pid.String()}, {Key: "table", Value: "users"}, {Key: "index", Value: "missing"}})
	h.DropIndex(c)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestTableHandler_projectNotAccessible(t *testing.T) {
	svc := pgservice.NewTableService(nil, repository.NewTableRepository())
	svc.SetPoolSourceForTest(staticTablePoolSource{err: service.ErrProjectNotAccessible})
	h := NewTableHandler(svc)

	pid := uuid.New()
	c, w := tableCtx(http.MethodGet, "/?schema=public", uuid.New(), pid, nil, gin.Params{{Key: "id", Value: pid.String()}})
	h.GetTables(c)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestParseFilterFromQuery(t *testing.T) {
	c, _ := tableCtx(http.MethodGet, "/?filter=%7B%22id%22%3A1%7D", uuid.New(), uuid.New(), nil, nil)
	assert.Equal(t, float64(1), parseFilterFromQuery(c)["id"])

	c2, _ := tableCtx(http.MethodGet, "/?filter=not-json", uuid.New(), uuid.New(), nil, nil)
	assert.Nil(t, parseFilterFromQuery(c2))
}
