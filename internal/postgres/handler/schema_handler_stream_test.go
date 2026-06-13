package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestSchemaHandler_GenerateSchemaFromTextStream(t *testing.T) {
	validBody := map[string]string{"requirement_text": "create a users table"}

	t.Run("unauthorized", func(t *testing.T) {
		h := NewSchemaHandler(&mockSchemaService{})
		c, w := schemaCtx(http.MethodPost, "/", uuid.Nil, uuid.New().String(), validBody)
		h.GenerateSchemaFromTextStream(c)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("invalid projectId", func(t *testing.T) {
		h := NewSchemaHandler(&mockSchemaService{})
		c, w := schemaCtx(http.MethodPost, "/", uuid.New(), "not-a-uuid", validBody)
		h.GenerateSchemaFromTextStream(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid body", func(t *testing.T) {
		h := NewSchemaHandler(&mockSchemaService{})
		// Missing requirement_text -> binding "required" fails.
		c, w := schemaCtx(http.MethodPost, "/", uuid.New(), uuid.New().String(), map[string]string{})
		h.GenerateSchemaFromTextStream(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing SCHEMA_AI_BASE_URL", func(t *testing.T) {
		t.Setenv("SCHEMA_AI_BASE_URL", "")
		h := NewSchemaHandler(&mockSchemaService{})
		c, w := schemaCtx(http.MethodPost, "/", uuid.New(), uuid.New().String(), validBody)
		h.GenerateSchemaFromTextStream(c)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("upstream unreachable", func(t *testing.T) {
		// Start then immediately close to obtain a dead URL -> client.Do fails.
		dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := dead.URL
		dead.Close()
		t.Setenv("SCHEMA_AI_BASE_URL", url)

		h := NewSchemaHandler(&mockSchemaService{})
		c, w := schemaCtx(http.MethodPost, "/", uuid.New(), uuid.New().String(), validBody)
		h.GenerateSchemaFromTextStream(c)
		assert.Equal(t, http.StatusBadGateway, w.Code)
	})

	t.Run("upstream non-2xx", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("boom"))
		}))
		t.Cleanup(upstream.Close)
		t.Setenv("SCHEMA_AI_BASE_URL", upstream.URL)

		h := NewSchemaHandler(&mockSchemaService{})
		c, w := schemaCtx(http.MethodPost, "/", uuid.New(), uuid.New().String(), validBody)
		h.GenerateSchemaFromTextStream(c)
		assert.Equal(t, http.StatusBadGateway, w.Code)
	})

	t.Run("upstream SSE proxied", func(t *testing.T) {
		const streamed = "data: {\"step\":\"start\"}\n\ndata: {\"step\":\"done\"}\n\n"
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Default ModelName should have been filled in before proxying.
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(streamed))
		}))
		t.Cleanup(upstream.Close)
		t.Setenv("SCHEMA_AI_BASE_URL", upstream.URL)

		h := NewSchemaHandler(&mockSchemaService{})
		c, w := schemaCtx(http.MethodPost, "/", uuid.New(), uuid.New().String(), validBody)
		h.GenerateSchemaFromTextStream(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
		assert.Contains(t, w.Body.String(), "\"step\":\"done\"")
	})
}

// Covers GenerateSchemaFromText's client.Do failure branch (upstream unreachable -> 502).
func TestSchemaHandler_GenerateSchemaFromText_upstreamUnreachable(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := dead.URL
	dead.Close()
	t.Setenv("SCHEMA_AI_BASE_URL", url)

	h := NewSchemaHandler(&mockSchemaService{})
	c, w := schemaCtx(http.MethodPost, "/", uuid.New(), uuid.New().String(),
		map[string]string{"requirement_text": "create a users table"})
	h.GenerateSchemaFromText(c)
	assert.Equal(t, http.StatusBadGateway, w.Code)
}
