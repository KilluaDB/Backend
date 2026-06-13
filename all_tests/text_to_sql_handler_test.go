package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"backend/internal/postgres/service"
	"backend/internal/testutil"
	"backend/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockTextToSQLService struct {
	result *service.TextToSQLResponse
	err    error
}

func (m *mockTextToSQLService) GenerateSQL(ctx context.Context, userID uuid.UUID, req *service.TextToSQLRequest, projectID uuid.UUID) (*service.TextToSQLResponse, error) {
	return m.result, m.err
}

type staticTextToSQLRunnerSource struct {
	runner service.QueryRunner
	id     uuid.UUID
	err    error
}

func (s *staticTextToSQLRunnerSource) QueryRunner(ctx context.Context, userID, projectID uuid.UUID) (service.QueryRunner, uuid.UUID, error) {
	if s.err != nil {
		return nil, uuid.Nil, s.err
	}
	id := s.id
	if id == uuid.Nil {
		id = uuid.New()
	}
	return s.runner, id, nil
}

func newTextToSQLPool(t *testing.T) pgxmock.PgxPoolIface {
	t.Helper()
	mock, err := pgxmock.NewPool(pgxmock.QueryMatcherOption(pgxmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, mock.ExpectationsWereMet())
		mock.Close()
	})
	return mock
}

func textToSQLCtx(method, path string, userID uuid.UUID, projectID string, body any) (*gin.Context, *httptest.ResponseRecorder) {
	c, w := testutil.NewGinContext(method, path, body, nil)
	if userID != uuid.Nil {
		c.Set(utils.UserIDContextKey, userID)
	}
	if projectID != "" {
		c.Params = gin.Params{{Key: "id", Value: projectID}}
	}
	return c, w
}

func TestTextToSQLHandler_GenerateAndExecuteSQL(t *testing.T) {
	t.Run("no project id", func(t *testing.T) {
		svc := &mockTextToSQLService{}
		h := NewTextToSQLHandler(svc, nil)
		c, w := textToSQLCtx(http.MethodPost, "/projects//text-to-sql", uuid.New(), "", nil)
		h.GenerateAndExecuteSQL(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("unauthorized", func(t *testing.T) {
		svc := &mockTextToSQLService{}
		h := NewTextToSQLHandler(svc, nil)
		c, w := textToSQLCtx(http.MethodPost, "/", uuid.Nil, uuid.New().String(), nil)
		h.GenerateAndExecuteSQL(c)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("empty question", func(t *testing.T) {
		svc := &mockTextToSQLService{}
		h := NewTextToSQLHandler(svc, nil)
		body := map[string]string{"question": ""}
		c, w := textToSQLCtx(http.MethodPost, "/", uuid.New(), uuid.New().String(), body)
		h.GenerateAndExecuteSQL(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid projectId format", func(t *testing.T) {
		svc := &mockTextToSQLService{}
		h := NewTextToSQLHandler(svc, nil)
		body := map[string]string{"question": "list users"}
		c, w := textToSQLCtx(http.MethodPost, "/", uuid.New(), "not-a-uuid", body)
		h.GenerateAndExecuteSQL(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("ErrProjectNotFound", func(t *testing.T) {
		svc := &mockTextToSQLService{
			err: service.ErrProjectNotFound,
		}
		h := NewTextToSQLHandler(svc, nil)
		body := map[string]string{"question": "list users"}
		c, w := textToSQLCtx(http.MethodPost, "/", uuid.New(), uuid.New().String(), body)
		h.GenerateAndExecuteSQL(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("ErrNoRunningDBInstance", func(t *testing.T) {
		svc := &mockTextToSQLService{
			err: service.ErrNoRunningDBInstance,
		}
		h := NewTextToSQLHandler(svc, nil)
		body := map[string]string{"question": "list users"}
		c, w := textToSQLCtx(http.MethodPost, "/", uuid.New(), uuid.New().String(), body)
		h.GenerateAndExecuteSQL(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("ErrTextToSQLUnavailable", func(t *testing.T) {
		svc := &mockTextToSQLService{
			err: service.ErrTextToSQLUnavailable,
		}
		h := NewTextToSQLHandler(svc, nil)
		body := map[string]string{"question": "list users"}
		c, w := textToSQLCtx(http.MethodPost, "/", uuid.New(), uuid.New().String(), body)
		h.GenerateAndExecuteSQL(c)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("ErrTextToSQLInvalidResponse", func(t *testing.T) {
		svc := &mockTextToSQLService{
			err: service.ErrTextToSQLInvalidResponse,
		}
		h := NewTextToSQLHandler(svc, nil)
		body := map[string]string{"question": "list users"}
		c, w := textToSQLCtx(http.MethodPost, "/", uuid.New(), uuid.New().String(), body)
		h.GenerateAndExecuteSQL(c)
		assert.Equal(t, http.StatusBadGateway, w.Code)
	})

	t.Run("query execution error", func(t *testing.T) {
		svc := &mockTextToSQLService{
			result: &service.TextToSQLResponse{
				Success: true,
				SQL:     "SELECT 1",
			},
		}
		runnerSource := &staticTextToSQLRunnerSource{
			err: errors.New("execution failed"),
		}
		qsvc := service.NewQueryService(nil, 50)
		qsvc.SetRunnerSourceForTest(runnerSource)
		h := NewTextToSQLHandler(svc, qsvc)
		body := map[string]string{"question": "list users"}
		c, w := textToSQLCtx(http.MethodPost, "/", uuid.New(), uuid.New().String(), body)
		h.GenerateAndExecuteSQL(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("success", func(t *testing.T) {
		pool := newTextToSQLPool(t)
		pool.ExpectQuery("SELECT 1").WillReturnRows(
			pgxmock.NewRows([]string{"?column?"}).AddRow(int32(1)),
		)
		runnerSource := &staticTextToSQLRunnerSource{
			runner: pool,
		}
		qsvc := service.NewQueryService(nil, 50)
		qsvc.SetRunnerSourceForTest(runnerSource)

		svc := &mockTextToSQLService{
			result: &service.TextToSQLResponse{
				Success: true,
				SQL:     "SELECT 1",
			},
		}
		h := NewTextToSQLHandler(svc, qsvc)
		body := map[string]string{"question": "list users"}
		c, w := textToSQLCtx(http.MethodPost, "/", uuid.New(), uuid.New().String(), body)
		h.GenerateAndExecuteSQL(c)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]any
		require.NoError(t, testutil.ParseJSONResponse(w, &resp))
		data := resp["data"].(map[string]any)
		assert.Equal(t, "SELECT 1", data["sql"])
		assert.NotNil(t, data["result"])
		assert.NotNil(t, data["execution_id"])
	})
}
