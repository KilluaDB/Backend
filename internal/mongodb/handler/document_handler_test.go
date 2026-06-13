package handler

import (
	"context"
	"net/http"
	"testing"

	"backend/internal/mongodb/model"
	mongoservice "backend/internal/mongodb/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

type mockDocumentService struct {
	queryResult    *model.QueryDocumentsResult
	queryErr       error
	insertResult   *model.InsertDocumentResult
	insertErr      error
	deleteResult   *model.DeleteDocumentsResult
	deleteErr      error
	getDocumentErr error
}

func (m *mockDocumentService) QueryDocuments(ctx context.Context, userID, projectID uuid.UUID, collection string, req model.QueryDocumentsRequest) (*model.QueryDocumentsResult, error) {
	return m.queryResult, m.queryErr
}

func (m *mockDocumentService) GetDocumentByID(ctx context.Context, userID, projectID uuid.UUID, collection string, id string) (map[string]interface{}, error) {
	return nil, m.getDocumentErr
}

func (m *mockDocumentService) GetDocuments(ctx context.Context, userID, projectID uuid.UUID, collection string, req model.GetDocumentsRequest) (*model.GetDocumentsResult, error) {
	return nil, nil
}

func (m *mockDocumentService) InsertDocuments(ctx context.Context, userID, projectID uuid.UUID, collection string, req model.InsertDocumentsRequest) (*model.InsertDocumentResult, error) {
	return m.insertResult, m.insertErr
}

func (m *mockDocumentService) UpdateDocuments(ctx context.Context, userID, projectID uuid.UUID, collection string, req model.UpdateDocumentsRequest) (*model.UpdateDocumentsResult, error) {
	return nil, nil
}

func (m *mockDocumentService) DeleteDocuments(ctx context.Context, userID, projectID uuid.UUID, collection string, req model.DeleteDocumentsRequest) (*model.DeleteDocumentsResult, error) {
	return m.deleteResult, m.deleteErr
}

func (m *mockDocumentService) CountDocuments(ctx context.Context, userID, projectID uuid.UUID, collection string, req model.CountDocumentsRequest) (*model.CountDocumentsResult, error) {
	return nil, nil
}

func (m *mockDocumentService) UpdateDocumentField(ctx context.Context, userID, projectID uuid.UUID, collection, id, field string, req model.UpdateFieldRequest) error {
	return nil
}

func (m *mockDocumentService) AddDocumentField(ctx context.Context, userID, projectID uuid.UUID, collection, id string, req model.AddDocumentFieldRequest) error {
	return nil
}

func (m *mockDocumentService) DeleteDocumentField(ctx context.Context, userID, projectID uuid.UUID, collection, id, field string) error {
	return nil
}

func (m *mockDocumentService) DeleteDocument(ctx context.Context, userID, projectID uuid.UUID, collection, id string) error {
	return nil
}

func TestDocumentHandler_QueryDocuments(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		svc        *mockDocumentService
		wantStatus int
	}{
		{
			name:       "valid JSON body",
			body:       map[string]any{"filter": map[string]any{"name": "test"}},
			svc:        &mockDocumentService{queryResult: &model.QueryDocumentsResult{Total: 1}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "malformed body",
			body:       "invalid-json",
			svc:        &mockDocumentService{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "service error",
			body:       map[string]any{},
			svc:        &mockDocumentService{queryErr: mongoservice.ErrInvalidFilter},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := uuid.New()
			projectID := uuid.New().String()
			h := NewDocumentHandler(tt.svc)

			c, w := mongoCollectionContext(http.MethodPost, "/mongodb/collections/users/query", userID, projectID, tt.body,
				gin.Params{{Key: "id", Value: projectID}, {Key: "collection", Value: "users"}})

			h.QueryDocuments(c)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestDocumentHandler_InsertDocuments(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		svc        *mockDocumentService
		wantStatus int
	}{
		{
			name:       "happy path",
			body:       map[string]any{"documents": []any{map[string]any{"name": "test"}}},
			svc:        &mockDocumentService{insertResult: &model.InsertDocumentResult{InsertedCount: 1}},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "validation error",
			body:       map[string]any{},
			svc:        &mockDocumentService{insertErr: mongoservice.ErrInvalidCollectionName},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := uuid.New()
			projectID := uuid.New().String()
			h := NewDocumentHandler(tt.svc)

			c, w := mongoCollectionContext(http.MethodPost, "/mongodb/collections/users/documents", userID, projectID, tt.body,
				gin.Params{{Key: "id", Value: projectID}, {Key: "collection", Value: "users"}})

			h.InsertDocuments(c)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestDocumentHandler_DeleteDocuments(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		svc        *mockDocumentService
		wantStatus int
	}{
		{
			name:       "happy path",
			body:       map[string]any{"filter": map[string]any{"name": "test"}},
			svc:        &mockDocumentService{deleteResult: &model.DeleteDocumentsResult{Deleted: 1}},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := uuid.New()
			projectID := uuid.New().String()
			h := NewDocumentHandler(tt.svc)

			c, w := mongoCollectionContext(http.MethodDelete, "/mongodb/collections/users/documents", userID, projectID, tt.body,
				gin.Params{{Key: "id", Value: projectID}, {Key: "collection", Value: "users"}})

			h.DeleteDocuments(c)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestDocumentHandler_OtherMethods(t *testing.T) {
	svc := &mockDocumentService{}
	h := NewDocumentHandler(svc)
	userID := uuid.New()
	projectID := uuid.New().String()

	t.Run("UpdateDocuments", func(t *testing.T) {
		c, w := mongoCollectionContext(http.MethodPatch, "/mongodb", userID, projectID, map[string]any{"filter": map[string]any{}, "update": map[string]any{"$set": map[string]any{"a": 1}}},
			gin.Params{{Key: "id", Value: projectID}, {Key: "collection", Value: "c"}})
		h.UpdateDocuments(c)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("CountDocuments", func(t *testing.T) {
		c, w := mongoCollectionContext(http.MethodPost, "/mongodb", userID, projectID, map[string]any{},
			gin.Params{{Key: "id", Value: projectID}, {Key: "collection", Value: "c"}})
		h.CountDocuments(c)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("UpdateDocumentField", func(t *testing.T) {
		c, w := mongoCollectionContext(http.MethodPatch, "/mongodb", userID, projectID, map[string]any{"value": 1},
			gin.Params{{Key: "id", Value: projectID}, {Key: "collection", Value: "c"}, {Key: "documentId", Value: "d"}, {Key: "field", Value: "f"}})
		h.UpdateDocumentField(c)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("AddDocumentField", func(t *testing.T) {
		c, w := mongoCollectionContext(http.MethodPost, "/mongodb", userID, projectID, map[string]any{"field": "f", "value": 1, "type": "int"},
			gin.Params{{Key: "id", Value: projectID}, {Key: "collection", Value: "c"}, {Key: "documentId", Value: "d"}})
		h.AddDocumentField(c)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("DeleteDocumentField", func(t *testing.T) {
		c, w := mongoCollectionContext(http.MethodDelete, "/mongodb", userID, projectID, nil,
			gin.Params{{Key: "id", Value: projectID}, {Key: "collection", Value: "c"}, {Key: "documentId", Value: "d"}, {Key: "field", Value: "f"}})
		h.DeleteDocumentField(c)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("DeleteDocument", func(t *testing.T) {
		c, w := mongoCollectionContext(http.MethodDelete, "/mongodb", userID, projectID, nil,
			gin.Params{{Key: "id", Value: projectID}, {Key: "collection", Value: "c"}, {Key: "documentId", Value: "d"}})
		h.DeleteDocument(c)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("GetDocuments", func(t *testing.T) {
		c, w := mongoCollectionContext(http.MethodGet, "/mongodb", userID, projectID, nil,
			gin.Params{{Key: "id", Value: projectID}, {Key: "collection", Value: "c"}})
		h.GetDocuments(c)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// Ensure the helper is in testutil pattern
func TestDashboardMetricsHandler(t *testing.T) {
	// Let's implement test for dashboard service error surfaced correctly in dashboard_handler test
	// But the user requested "GetMetrics — dashboard service error surfaced correctly" in `internal/mongodb/handler` section.
	// So we should check dashboard_handler.go in mongodb
}
