package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"

	pgservice "backend/internal/postgres/service"
	"backend/internal/service"
	"backend/internal/testutil"
	"backend/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type staticRunnerSource struct {
	runner pgservice.QueryRunner
	id     uuid.UUID
	err    error
}

func (s staticRunnerSource) QueryRunner(ctx context.Context, userID, projectID uuid.UUID) (pgservice.QueryRunner, uuid.UUID, error) {
	if s.err != nil {
		return nil, uuid.Nil, s.err
	}
	id := s.id
	if id == uuid.Nil {
		id = uuid.New()
	}
	return s.runner, id, nil
}

func newHandlerMockPool(t *testing.T) pgxmock.PgxPoolIface {
	t.Helper()
	mock, err := pgxmock.NewPool(pgxmock.QueryMatcherOption(pgxmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, mock.ExpectationsWereMet())
		mock.Close()
	})
	return mock
}

func TestQueryHandler_ExecuteQuery_unauthorized(t *testing.T) {
	h := NewQueryHandler(pgservice.NewQueryService(nil, 50))
	c, w := testutil.NewGinContext(http.MethodPost, "/projects/"+uuid.New().String()+"/query", map[string]string{"query": "SELECT 1"}, nil)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}

	h.ExecuteQuery(c)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestQueryHandler_ExecuteQuery_invalidProjectID(t *testing.T) {
	h := NewQueryHandler(pgservice.NewQueryService(nil, 50))
	c, w := testutil.NewGinContext(http.MethodPost, "/projects/bad/query", map[string]string{"query": "SELECT 1"}, nil)
	c.Set(utils.UserIDContextKey, uuid.New())
	c.Params = gin.Params{{Key: "id", Value: "not-a-uuid"}}

	h.ExecuteQuery(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestQueryHandler_ExecuteQuery_emptyQuery(t *testing.T) {
	h := NewQueryHandler(pgservice.NewQueryService(nil, 50))
	pid := uuid.New()
	c, w := testutil.NewGinContext(http.MethodPost, "/projects/"+pid.String()+"/query", map[string]string{"query": ""}, nil)
	c.Set(utils.UserIDContextKey, uuid.New())
	c.Params = gin.Params{{Key: "id", Value: pid.String()}}

	h.ExecuteQuery(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestQueryHandler_ExecuteQuery_invalidSQL(t *testing.T) {
	h := NewQueryHandler(pgservice.NewQueryService(nil, 50))
	pid := uuid.New()
	c, w := testutil.NewGinContext(http.MethodPost, "/projects/"+pid.String()+"/query", map[string]string{"query": "DROP DATABASE x"}, nil)
	c.Set(utils.UserIDContextKey, uuid.New())
	c.Params = gin.Params{{Key: "id", Value: pid.String()}}

	h.ExecuteQuery(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestQueryHandler_ExecuteQuery_projectNotFound(t *testing.T) {
	svc := pgservice.NewQueryService(nil, 50)
	svc.SetRunnerSourceForTest(staticRunnerSource{err: service.ErrProjectNotAccessible})
	h := NewQueryHandler(svc)

	pid := uuid.New()
	c, w := testutil.NewGinContext(http.MethodPost, "/projects/"+pid.String()+"/query", map[string]string{"query": "SELECT 1"}, nil)
	c.Set(utils.UserIDContextKey, uuid.New())
	c.Params = gin.Params{{Key: "id", Value: pid.String()}}

	h.ExecuteQuery(c)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestQueryHandler_ExecuteQuery_success(t *testing.T) {
	mock := newHandlerMockPool(t)
	mock.ExpectQuery("SELECT 1").WillReturnRows(
		pgxmock.NewRows([]string{"?column?"}).AddRow(int32(1)),
	)

	svc := pgservice.NewQueryService(nil, 50)
	svc.SetRunnerSourceForTest(staticRunnerSource{runner: mock})
	h := NewQueryHandler(svc)

	pid := uuid.New()
	c, w := testutil.NewGinContext(http.MethodPost, "/projects/"+pid.String()+"/query", map[string]string{"query": "SELECT 1"}, nil)
	c.Set(utils.UserIDContextKey, uuid.New())
	c.Params = gin.Params{{Key: "id", Value: pid.String()}}

	h.ExecuteQuery(c)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestQueryHandler_GetQueryHistory_unauthorized(t *testing.T) {
	h := NewQueryHandler(pgservice.NewQueryService(nil, 50))
	c, w := testutil.NewGinContext(http.MethodGet, "/projects/"+uuid.New().String()+"/query/history", nil, nil)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}

	h.GetQueryHistory(c)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestQueryHandler_GetQueryHistory_extensionDisabled(t *testing.T) {
	mock := newHandlerMockPool(t)
	mock.ExpectQuery(`SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements')`).
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))

	svc := pgservice.NewQueryService(nil, 50)
	svc.SetRunnerSourceForTest(staticRunnerSource{runner: mock})
	h := NewQueryHandler(svc)

	pid := uuid.New()
	c, w := testutil.NewGinContext(http.MethodGet, "/projects/"+pid.String()+"/query/history?limit=5", nil, nil)
	c.Set(utils.UserIDContextKey, uuid.New())
	c.Params = gin.Params{{Key: "id", Value: pid.String()}}

	h.GetQueryHistory(c)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestQueryHandler_GetQueryHistory_serviceError(t *testing.T) {
	svc := pgservice.NewQueryService(nil, 50)
	svc.SetRunnerSourceForTest(staticRunnerSource{err: errors.New("db down")})
	h := NewQueryHandler(svc)

	pid := uuid.New()
	c, w := testutil.NewGinContext(http.MethodGet, "/projects/"+pid.String()+"/query/history", nil, nil)
	c.Set(utils.UserIDContextKey, uuid.New())
	c.Params = gin.Params{{Key: "id", Value: pid.String()}}

	h.GetQueryHistory(c)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
