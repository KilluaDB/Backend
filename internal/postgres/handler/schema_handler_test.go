package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	pgservice "backend/internal/postgres/service"
	"backend/internal/service"
	"backend/internal/testutil"
	"backend/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockSchemaService struct {
	visualizeSchemaResult string
	visualizeSchemaErr    error
	listSchemasResult     []string
	listSchemasErr        error
	cachePendingDDLErr    error
	applyDDLErr           error
}

func (m *mockSchemaService) VisualizeSchema(ctx context.Context, userID, projectID uuid.UUID, schema string) (string, error) {
	return m.visualizeSchemaResult, m.visualizeSchemaErr
}

func (m *mockSchemaService) ListSchemas(ctx context.Context, userID, projectID uuid.UUID) ([]string, error) {
	return m.listSchemasResult, m.listSchemasErr
}

func (m *mockSchemaService) CachePendingDDL(ctx context.Context, projectID uuid.UUID, ddl string) error {
	return m.cachePendingDDLErr
}

func (m *mockSchemaService) ApplyDDL(ctx context.Context, userID, projectID uuid.UUID) error {
	return m.applyDDLErr
}

func schemaCtx(method, path string, userID uuid.UUID, projectID string, body any) (*gin.Context, *httptest.ResponseRecorder) {
	c, w := testutil.NewGinContext(method, path, body, nil)
	if userID != uuid.Nil {
		c.Set(utils.UserIDContextKey, userID)
	}
	c.Params = gin.Params{{Key: "id", Value: projectID}}
	return c, w
}

func TestSchemaHandler_VisualizeSchema(t *testing.T) {
	t.Run("unauthorized", func(t *testing.T) {
		svc := &mockSchemaService{}
		h := NewSchemaHandler(svc)
		c, w := schemaCtx(http.MethodGet, "/", uuid.Nil, uuid.New().String(), nil)
		h.VisualizeSchema(c)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("invalid projectId", func(t *testing.T) {
		svc := &mockSchemaService{}
		h := NewSchemaHandler(svc)
		c, w := schemaCtx(http.MethodGet, "/", uuid.New(), "not-a-uuid", nil)
		h.VisualizeSchema(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("project not found", func(t *testing.T) {
		svc := &mockSchemaService{
			visualizeSchemaErr: service.ErrProjectNotFound,
		}
		h := NewSchemaHandler(svc)
		c, w := schemaCtx(http.MethodGet, "/", uuid.New(), uuid.New().String(), nil)
		h.VisualizeSchema(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("service error", func(t *testing.T) {
		svc := &mockSchemaService{
			visualizeSchemaErr: errors.New("db error"),
		}
		h := NewSchemaHandler(svc)
		c, w := schemaCtx(http.MethodGet, "/", uuid.New(), uuid.New().String(), nil)
		h.VisualizeSchema(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("custom schema param", func(t *testing.T) {
		svc := &mockSchemaService{
			visualizeSchemaResult: "erDiagram CUSTOM",
		}
		h := NewSchemaHandler(svc)
		pid := uuid.New()
		c, w := schemaCtx(http.MethodGet, "/?schema=myschema", uuid.New(), pid.String(), nil)
		h.VisualizeSchema(c)
		assert.Equal(t, http.StatusOK, w.Code)

		var body map[string]any
		require.NoError(t, testutil.ParseJSONResponse(w, &body))
		data := body["data"].(map[string]any)
		assert.Equal(t, "myschema", data["schema"])
		assert.Equal(t, "erDiagram CUSTOM", data["mermaid"])
	})

	t.Run("success", func(t *testing.T) {
		svc := &mockSchemaService{
			visualizeSchemaResult: "erDiagram USER ||--o{ POST : has",
		}
		h := NewSchemaHandler(svc)
		pid := uuid.New()
		c, w := schemaCtx(http.MethodGet, "/?schema=public", uuid.New(), pid.String(), nil)
		h.VisualizeSchema(c)
		assert.Equal(t, http.StatusOK, w.Code)

		var body map[string]any
		require.NoError(t, testutil.ParseJSONResponse(w, &body))
		data := body["data"].(map[string]any)
		assert.Equal(t, "public", data["schema"])
		assert.Equal(t, "erDiagram USER ||--o{ POST : has", data["mermaid"])
	})
}

func TestSchemaHandler_ListSchemas(t *testing.T) {
	t.Run("unauthorized", func(t *testing.T) {
		svc := &mockSchemaService{}
		h := NewSchemaHandler(svc)
		c, w := schemaCtx(http.MethodGet, "/", uuid.Nil, uuid.New().String(), nil)
		h.ListSchemas(c)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("invalid projectId", func(t *testing.T) {
		svc := &mockSchemaService{}
		h := NewSchemaHandler(svc)
		c, w := schemaCtx(http.MethodGet, "/", uuid.New(), "not-a-uuid", nil)
		h.ListSchemas(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("project not accessible", func(t *testing.T) {
		svc := &mockSchemaService{
			listSchemasErr: service.ErrProjectNotAccessible,
		}
		h := NewSchemaHandler(svc)
		c, w := schemaCtx(http.MethodGet, "/", uuid.New(), uuid.New().String(), nil)
		h.ListSchemas(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("no running instance", func(t *testing.T) {
		svc := &mockSchemaService{
			listSchemasErr: service.ErrNoRunningInstance,
		}
		h := NewSchemaHandler(svc)
		c, w := schemaCtx(http.MethodGet, "/", uuid.New(), uuid.New().String(), nil)
		h.ListSchemas(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("default error", func(t *testing.T) {
		svc := &mockSchemaService{
			listSchemasErr: errors.New("unexpected pool error"),
		}
		h := NewSchemaHandler(svc)
		c, w := schemaCtx(http.MethodGet, "/", uuid.New(), uuid.New().String(), nil)
		h.ListSchemas(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)

		var body map[string]any
		require.NoError(t, testutil.ParseJSONResponse(w, &body))
		assert.Contains(t, body["message"], "unexpected pool error")
	})

	t.Run("success", func(t *testing.T) {
		schemas := []string{"public", "app"}
		svc := &mockSchemaService{
			listSchemasResult: schemas,
		}
		h := NewSchemaHandler(svc)
		pid := uuid.New()
		c, w := schemaCtx(http.MethodGet, "/", uuid.New(), pid.String(), nil)
		h.ListSchemas(c)
		assert.Equal(t, http.StatusOK, w.Code)

		var body map[string]any
		require.NoError(t, testutil.ParseJSONResponse(w, &body))
		data := body["data"].(map[string]any)
		got := data["schemas"].([]any)
		assert.Equal(t, len(schemas), len(got))
		for i := range schemas {
			assert.Equal(t, schemas[i], got[i])
		}
	})
}

func TestSchemaHandler_ApplySchemaDDL(t *testing.T) {
	t.Run("unauthorized", func(t *testing.T) {
		svc := &mockSchemaService{}
		h := NewSchemaHandler(svc)
		c, w := schemaCtx(http.MethodPost, "/", uuid.Nil, uuid.New().String(), nil)
		h.ApplySchemaDDL(c)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("invalid projectId", func(t *testing.T) {
		svc := &mockSchemaService{}
		h := NewSchemaHandler(svc)
		c, w := schemaCtx(http.MethodPost, "/", uuid.New(), "not-a-uuid", nil)
		h.ApplySchemaDDL(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("no pending DDL", func(t *testing.T) {
		svc := &mockSchemaService{
			applyDDLErr: pgservice.ErrNoPendingDDL,
		}
		h := NewSchemaHandler(svc)
		c, w := schemaCtx(http.MethodPost, "/", uuid.New(), uuid.New().String(), nil)
		h.ApplySchemaDDL(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("service error", func(t *testing.T) {
		svc := &mockSchemaService{
			applyDDLErr: errors.New("apply failed"),
		}
		h := NewSchemaHandler(svc)
		c, w := schemaCtx(http.MethodPost, "/", uuid.New(), uuid.New().String(), nil)
		h.ApplySchemaDDL(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("success", func(t *testing.T) {
		svc := &mockSchemaService{}
		h := NewSchemaHandler(svc)
		pid := uuid.New()
		c, w := schemaCtx(http.MethodPost, "/", uuid.New(), pid.String(), nil)
		h.ApplySchemaDDL(c)
		assert.Equal(t, http.StatusCreated, w.Code)
	})
}

func TestSchemaHandler_GenerateSchemaFromText(t *testing.T) {
	t.Run("unauthorized", func(t *testing.T) {
		svc := &mockSchemaService{}
		h := NewSchemaHandler(svc)
		c, w := schemaCtx(http.MethodPost, "/", uuid.Nil, uuid.New().String(), nil)
		h.GenerateSchemaFromText(c)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("invalid projectId", func(t *testing.T) {
		svc := &mockSchemaService{}
		h := NewSchemaHandler(svc)
		c, w := schemaCtx(http.MethodPost, "/", uuid.New(), "not-a-uuid", nil)
		h.GenerateSchemaFromText(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid body", func(t *testing.T) {
		svc := &mockSchemaService{}
		h := NewSchemaHandler(svc)
		c, w := schemaCtx(http.MethodPost, "/", uuid.New(), uuid.New().String(), "not-json")
		h.GenerateSchemaFromText(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)

		var body map[string]any
		require.NoError(t, testutil.ParseJSONResponse(w, &body))
		assert.Contains(t, body["message"], "Invalid request body")
	})

	t.Run("missing SCHEMA_AI_BASE_URL", func(t *testing.T) {
		t.Setenv("SCHEMA_AI_BASE_URL", "")
		svc := &mockSchemaService{}
		h := NewSchemaHandler(svc)
		pid := uuid.New()
		body := map[string]string{"requirement_text": "create a users table"}
		c, w := schemaCtx(http.MethodPost, "/", uuid.New(), pid.String(), body)
		h.GenerateSchemaFromText(c)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("upstream non-2xx", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("upstream error"))
		}))
		t.Cleanup(upstream.Close)
		t.Setenv("SCHEMA_AI_BASE_URL", upstream.URL)

		svc := &mockSchemaService{}
		h := NewSchemaHandler(svc)
		pid := uuid.New()
		body := map[string]string{"requirement_text": "create a users table"}
		c, w := schemaCtx(http.MethodPost, "/", uuid.New(), pid.String(), body)
		h.GenerateSchemaFromText(c)
		assert.Equal(t, http.StatusBadGateway, w.Code)
	})

	t.Run("upstream invalid JSON", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`not-json`))
		}))
		t.Cleanup(upstream.Close)
		t.Setenv("SCHEMA_AI_BASE_URL", upstream.URL)

		svc := &mockSchemaService{}
		h := NewSchemaHandler(svc)
		pid := uuid.New()
		body := map[string]string{"requirement_text": "create a users table"}
		c, w := schemaCtx(http.MethodPost, "/", uuid.New(), pid.String(), body)
		h.GenerateSchemaFromText(c)
		assert.Equal(t, http.StatusBadGateway, w.Code)
	})

	t.Run("upstream success=false", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(schemaGenerateResponse{
				Success: false,
				Message: "generation failed",
				Error:   strPtr("invalid requirements"),
			})
		}))
		t.Cleanup(upstream.Close)
		t.Setenv("SCHEMA_AI_BASE_URL", upstream.URL)

		svc := &mockSchemaService{}
		h := NewSchemaHandler(svc)
		pid := uuid.New()
		body := map[string]string{"requirement_text": "create a users table"}
		c, w := schemaCtx(http.MethodPost, "/", uuid.New(), pid.String(), body)
		h.GenerateSchemaFromText(c)
		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	})

	t.Run("upstream success=true", func(t *testing.T) {
		mmd := "erDiagram USER ||--o{ POST : has"
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(schemaGenerateResponse{
				Success: true,
				Message: "generated",
				Mmd:     &mmd,
				DDL:     strPtr("CREATE TABLE users..."),
			})
		}))
		t.Cleanup(upstream.Close)
		t.Setenv("SCHEMA_AI_BASE_URL", upstream.URL)

		svc := &mockSchemaService{}
		h := NewSchemaHandler(svc)
		pid := uuid.New()
		body := map[string]string{"requirement_text": "create a users table"}
		c, w := schemaCtx(http.MethodPost, "/", uuid.New(), pid.String(), body)
		h.GenerateSchemaFromText(c)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]any
		require.NoError(t, testutil.ParseJSONResponse(w, &resp))
		data := resp["data"].(map[string]any)
		assert.Equal(t, mmd, data["mermaid"])
	})
}

func strPtr(s string) *string {
	return &s
}
