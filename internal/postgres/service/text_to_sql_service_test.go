package service

import (
	"backend/internal/model"
	"backend/internal/postgres/infra"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockDSNProvider struct {
	dsn        string
	instanceID uuid.UUID
	err        error
}

func (m *mockDSNProvider) GetConnectionDSN(ctx context.Context, userID, projectID uuid.UUID) (string, uuid.UUID, error) {
	return m.dsn, m.instanceID, m.err
}

type mockProjectGetter struct {
	project *model.Project
	err     error
}

func (m *mockProjectGetter) GetByIDAndUserID(ctx context.Context, id, userID uuid.UUID) (*model.Project, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.project, nil
}

func newTextToSQLService(t *testing.T, dsn infra.DSNProvider, getter projectGetter, serverURL string) *TextToSQLService {
	t.Helper()
	t.Setenv("TEXT_TO_SQL", serverURL)
	return &TextToSQLService{
		baseURL: serverURL,
		httpClient: &http.Client{
			Timeout: 0,
		},
		projectRepo: getter,
		dsnProvider: dsn,
	}
}

func TestTextToSQLService_GenerateSQL(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	projectID := uuid.New()
	req := &TextToSQLRequest{Question: "list all users"}

	t.Run("project not found", func(t *testing.T) {
		svc := newTextToSQLService(t, &mockDSNProvider{}, &mockProjectGetter{}, "http://localhost:1")
		_, err := svc.GenerateSQL(ctx, userID, req, projectID)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrProjectNotFound)
	})

	t.Run("project repo error", func(t *testing.T) {
		repoErr := errors.New("db connection lost")
		svc := newTextToSQLService(t, &mockDSNProvider{}, &mockProjectGetter{err: repoErr}, "http://localhost:1")
		_, err := svc.GenerateSQL(ctx, userID, req, projectID)
		require.Error(t, err)
		assert.ErrorIs(t, err, repoErr)
	})

	t.Run("DSN resolution fails with no running instance", func(t *testing.T) {
		dsn := &mockDSNProvider{err: errors.New("no running database instance for this project")}
		svc := newTextToSQLService(t, dsn, &mockProjectGetter{project: &model.Project{}}, "http://localhost:1")
		_, err := svc.GenerateSQL(ctx, userID, req, projectID)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrNoRunningDBInstance)
	})

	t.Run("DSN parse failure", func(t *testing.T) {
		dsn := &mockDSNProvider{dsn: "not-a-url"}
		svc := newTextToSQLService(t, dsn, &mockProjectGetter{project: &model.Project{}}, "http://localhost:1")
		_, err := svc.GenerateSQL(ctx, userID, req, projectID)
		require.Error(t, err)
	})

	t.Run("HTTP request fails", func(t *testing.T) {
		dsn := &mockDSNProvider{dsn: "postgres://u:pass@host:5432/db"}
		svc := newTextToSQLService(t, dsn, &mockProjectGetter{project: &model.Project{}}, "http://127.0.0.1:1")
		_, err := svc.GenerateSQL(ctx, userID, req, projectID)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrTextToSQLUnavailable))
	})

	t.Run("upstream 500", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Internal Server Error"))
		}))
		t.Cleanup(upstream.Close)

		dsn := &mockDSNProvider{dsn: "postgres://u:pass@host:5432/db"}
		svc := newTextToSQLService(t, dsn, &mockProjectGetter{project: &model.Project{}}, upstream.URL)
		_, err := svc.GenerateSQL(ctx, userID, req, projectID)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrTextToSQLInvalidResponse))
	})

	t.Run("upstream returns invalid JSON", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("not json"))
		}))
		t.Cleanup(upstream.Close)

		dsn := &mockDSNProvider{dsn: "postgres://u:pass@host:5432/db"}
		svc := newTextToSQLService(t, dsn, &mockProjectGetter{project: &model.Project{}}, upstream.URL)
		_, err := svc.GenerateSQL(ctx, userID, req, projectID)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrTextToSQLInvalidResponse))
	})

	t.Run("upstream success=false", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(TextToSQLResponse{
				Success: false,
				Error:   "no schema",
			})
		}))
		t.Cleanup(upstream.Close)

		dsn := &mockDSNProvider{dsn: "postgres://u:pass@host:5432/db"}
		svc := newTextToSQLService(t, dsn, &mockProjectGetter{project: &model.Project{}}, upstream.URL)
		result, err := svc.GenerateSQL(ctx, userID, req, projectID)
		require.NoError(t, err)
		assert.False(t, result.Success)
	})

	t.Run("success", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify the request body contains the question and db_connection
			var body bytes.Buffer
			_, _ = body.ReadFrom(r.Body)
			assert.Contains(t, body.String(), "list all users")

			json.NewEncoder(w).Encode(TextToSQLResponse{
				Success: true,
				SQL:     "SELECT * FROM users",
			})
		}))
		t.Cleanup(upstream.Close)

		dsn := &mockDSNProvider{dsn: "postgres://u:pass@host:5432/db"}
		svc := newTextToSQLService(t, dsn, &mockProjectGetter{project: &model.Project{}}, upstream.URL)
		result, err := svc.GenerateSQL(ctx, userID, req, projectID)
		require.NoError(t, err)
		assert.True(t, result.Success)
		assert.Equal(t, "SELECT * FROM users", result.SQL)
	})
}
