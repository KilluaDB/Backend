package handler

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	coreService "backend/internal/service"
	mongoservice "backend/internal/mongodb/service"
)

func TestDocumentHandler_GetDocument(t *testing.T) {
	tests := []struct {
		name       string
		docId      string
		svc        *mockDocumentService
		wantStatus int
	}{
		{
			name:       "success",
			docId:      "123",
			svc:        &mockDocumentService{},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing id",
			docId:      "",
			svc:        &mockDocumentService{},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := uuid.New()
			projectID := uuid.New().String()
			h := NewDocumentHandler(tt.svc)

			params := gin.Params{{Key: "id", Value: projectID}, {Key: "collection", Value: "users"}}
			if tt.docId != "" {
				params = append(params, gin.Param{Key: "docId", Value: tt.docId})
			}

			c, w := mongoCollectionContext(http.MethodGet, "/mongodb/collections/users/documents/123", userID, projectID, nil, params)
			h.GetDocument(c)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestDocumentHandler_GetDocuments(t *testing.T) {
	tests := []struct {
		name       string
		svc        *mockDocumentService
		wantStatus int
	}{
		{
			name:       "success",
			svc:        &mockDocumentService{},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := uuid.New()
			projectID := uuid.New().String()
			h := NewDocumentHandler(tt.svc)

			c, w := mongoCollectionContext(http.MethodGet, "/mongodb/collections/users/documents", userID, projectID, nil,
				gin.Params{{Key: "id", Value: projectID}, {Key: "collection", Value: "users"}})
			h.GetDocuments(c)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestDocumentHandler_UpdateDocuments(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		svc        *mockDocumentService
		wantStatus int
	}{
		{
			name:       "success",
			body:       map[string]any{"filter": map[string]any{}, "update": map[string]any{}},
			svc:        &mockDocumentService{},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := uuid.New()
			projectID := uuid.New().String()
			h := NewDocumentHandler(tt.svc)

			c, w := mongoCollectionContext(http.MethodPut, "/mongodb/collections/users/documents", userID, projectID, tt.body,
				gin.Params{{Key: "id", Value: projectID}, {Key: "collection", Value: "users"}})
			h.UpdateDocuments(c)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestDocumentHandler_CountDocuments(t *testing.T) {
	tests := []struct {
		name       string
		svc        *mockDocumentService
		wantStatus int
	}{
		{
			name:       "success",
			svc:        &mockDocumentService{},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := uuid.New()
			projectID := uuid.New().String()
			h := NewDocumentHandler(tt.svc)

			c, w := mongoCollectionContext(http.MethodPost, "/mongodb/collections/users/count", userID, projectID, nil,
				gin.Params{{Key: "id", Value: projectID}, {Key: "collection", Value: "users"}})
			h.CountDocuments(c)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestDocumentHandler_UpdateDocumentField(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		svc        *mockDocumentService
		wantStatus int
	}{
		{
			name:       "success",
			body:       map[string]any{"value": "test"},
			svc:        &mockDocumentService{},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := uuid.New()
			projectID := uuid.New().String()
			h := NewDocumentHandler(tt.svc)

			c, w := mongoCollectionContext(http.MethodPut, "/mongodb/collections/users/documents/123/fields/name", userID, projectID, tt.body,
				gin.Params{{Key: "id", Value: projectID}, {Key: "collection", Value: "users"}, {Key: "docId", Value: "123"}, {Key: "field", Value: "name"}})
			h.UpdateDocumentField(c)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestDocumentHandler_AddDocumentField(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		svc        *mockDocumentService
		wantStatus int
	}{
		{
			name:       "success",
			body:       map[string]any{"field": "name", "value": "test", "type": "string"},
			svc:        &mockDocumentService{},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := uuid.New()
			projectID := uuid.New().String()
			h := NewDocumentHandler(tt.svc)

			c, w := mongoCollectionContext(http.MethodPost, "/mongodb/collections/users/documents/123/fields", userID, projectID, tt.body,
				gin.Params{{Key: "id", Value: projectID}, {Key: "collection", Value: "users"}, {Key: "docId", Value: "123"}})
			h.AddDocumentField(c)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestDocumentHandler_DeleteDocumentField(t *testing.T) {
	tests := []struct {
		name       string
		svc        *mockDocumentService
		wantStatus int
	}{
		{
			name:       "success",
			svc:        &mockDocumentService{},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := uuid.New()
			projectID := uuid.New().String()
			h := NewDocumentHandler(tt.svc)

			c, w := mongoCollectionContext(http.MethodDelete, "/mongodb/collections/users/documents/123/fields/name", userID, projectID, nil,
				gin.Params{{Key: "id", Value: projectID}, {Key: "collection", Value: "users"}, {Key: "docId", Value: "123"}, {Key: "field", Value: "name"}})
			h.DeleteDocumentField(c)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestDocumentHandler_DeleteDocumentSingle(t *testing.T) {
	tests := []struct {
		name       string
		svc        *mockDocumentService
		wantStatus int
	}{
		{
			name:       "success",
			svc:        &mockDocumentService{},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := uuid.New()
			projectID := uuid.New().String()
			h := NewDocumentHandler(tt.svc)

			c, w := mongoCollectionContext(http.MethodDelete, "/mongodb/collections/users/documents/123", userID, projectID, nil,
				gin.Params{{Key: "id", Value: projectID}, {Key: "collection", Value: "users"}, {Key: "docId", Value: "123"}})
			h.DeleteDocument(c)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestHandleDocumentError(t *testing.T) {
	tests := []struct {
		err        error
		wantStatus int
	}{
		{mongoservice.ErrDocumentNotFound, http.StatusNotFound},
		{mongoservice.ErrInvalidDocumentID, http.StatusBadRequest},
		{mongoservice.ErrInvalidFilter, http.StatusBadRequest},
		{mongoservice.ErrInvalidUpdate, http.StatusBadRequest},
		{mongoservice.ErrInvalidCollectionName, http.StatusBadRequest},
		{mongoservice.ErrTypeMismatch, http.StatusConflict},
		{mongoservice.ErrInvalidFieldType, http.StatusBadRequest},
		{mongoservice.ErrFieldAlreadyExists, http.StatusConflict},
		{coreService.ErrNoRunningInstance, http.StatusNotFound}, // triggers failMongoInstanceError
		{assert.AnError, http.StatusBadRequest},                 // default
	}

	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			userID := uuid.New()
			projectID := uuid.New().String()
			
			// Use GetDocument to test the error handler
			svc := &mockDocumentService{
				getDocumentErr: tt.err,
			}
			h := NewDocumentHandler(svc)

			c, w := mongoCollectionContext(http.MethodGet, "/mongodb/collections/users/documents/123", userID, projectID, nil,
				gin.Params{{Key: "id", Value: projectID}, {Key: "collection", Value: "users"}, {Key: "docId", Value: "123"}})
			
			h.GetDocument(c)
			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}
