package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"backend/internal/mongodb/model"
	mongoservice "backend/internal/mongodb/service"
	"backend/internal/service"
	"backend/internal/testutil"
	"backend/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockCollectionService struct {
	listCollections []string
	listErr         error
	createErr       error
	deleteErr       error
	addResult       *model.FieldUpdateResult
	addErr          error
	removeResult    *model.FieldUpdateResult
	removeErr       error
}

func (m mockCollectionService) ListCollections(ctx context.Context, userID, projectID uuid.UUID) ([]string, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.listCollections, nil
}

func (m mockCollectionService) CreateCollection(ctx context.Context, userID, projectID uuid.UUID, name string) error {
	return m.createErr
}

func (m mockCollectionService) DeleteCollection(ctx context.Context, userID, projectID uuid.UUID, name string) error {
	return m.deleteErr
}

func (m mockCollectionService) AddField(ctx context.Context, userID, projectID uuid.UUID, collection string, req model.AddFieldRequest) (*model.FieldUpdateResult, error) {
	if m.addErr != nil {
		return nil, m.addErr
	}
	return m.addResult, nil
}

func (m mockCollectionService) RemoveField(ctx context.Context, userID, projectID uuid.UUID, collection, field string) (*model.FieldUpdateResult, error) {
	if m.removeErr != nil {
		return nil, m.removeErr
	}
	return m.removeResult, nil
}

func TestCollectionHandler_ListCollections(t *testing.T) {
	tests := []struct {
		name       string
		userID     uuid.UUID
		projectID  string
		svc        mockCollectionService
		wantStatus int
		assertBody func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name:       "unauthorized",
			projectID:  uuid.New().String(),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid projectId",
			userID:     uuid.New(),
			projectID:  "not-a-uuid",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "ErrProjectNotAccessible",
			userID:     uuid.New(),
			projectID:  uuid.New().String(),
			svc:        mockCollectionService{listErr: service.ErrProjectNotAccessible},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "success",
			userID:     uuid.New(),
			projectID:  uuid.New().String(),
			svc:        mockCollectionService{listCollections: []string{"users", "orders"}},
			wantStatus: http.StatusOK,
			assertBody: func(t *testing.T, w *httptest.ResponseRecorder) {
				data := responseData(t, w)
				_, ok := data["collections"]
				assert.True(t, ok)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewCollectionHandler(tt.svc)
			c, w := mongoCollectionContext(http.MethodGet, "/mongodb/collections", tt.userID, tt.projectID, nil,
				gin.Params{{Key: "id", Value: tt.projectID}})

			h.ListCollections(c)

			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.assertBody != nil {
				tt.assertBody(t, w)
			}
		})
	}
}

func TestCollectionHandler_CreateCollection(t *testing.T) {
	tests := []struct {
		name       string
		userID     uuid.UUID
		projectID  string
		body       any
		svc        mockCollectionService
		wantStatus int
	}{
		{
			name:       "unauthorized",
			projectID:  uuid.New().String(),
			body:       map[string]any{"name": "users"},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid body",
			userID:     uuid.New(),
			projectID:  uuid.New().String(),
			body:       map[string]any{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "ErrInvalidCollectionName",
			userID:     uuid.New(),
			projectID:  uuid.New().String(),
			body:       map[string]any{"name": "bad\x00name"},
			svc:        mockCollectionService{createErr: mongoservice.ErrInvalidCollectionName},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "ErrCollectionAlreadyExists",
			userID:     uuid.New(),
			projectID:  uuid.New().String(),
			body:       map[string]any{"name": "users"},
			svc:        mockCollectionService{createErr: mongoservice.ErrCollectionAlreadyExists},
			wantStatus: http.StatusConflict,
		},
		{
			name:       "ErrProjectNotAccessible",
			userID:     uuid.New(),
			projectID:  uuid.New().String(),
			body:       map[string]any{"name": "users"},
			svc:        mockCollectionService{createErr: service.ErrProjectNotAccessible},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "success",
			userID:     uuid.New(),
			projectID:  uuid.New().String(),
			body:       map[string]any{"name": "users"},
			wantStatus: http.StatusCreated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewCollectionHandler(tt.svc)
			c, w := mongoCollectionContext(http.MethodPost, "/mongodb/collections", tt.userID, tt.projectID, tt.body,
				gin.Params{{Key: "id", Value: tt.projectID}})

			h.CreateCollection(c)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestCollectionHandler_DeleteCollection(t *testing.T) {
	tests := []struct {
		name       string
		userID     uuid.UUID
		collection string
		svc        mockCollectionService
		wantStatus int
	}{
		{
			name:       "missing collection param",
			userID:     uuid.New(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unauthorized",
			collection: "users",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "ErrCollectionNotFound",
			userID:     uuid.New(),
			collection: "users",
			svc:        mockCollectionService{deleteErr: mongoservice.ErrCollectionNotFound},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "ErrInvalidCollectionName",
			userID:     uuid.New(),
			collection: "bad\x00name",
			svc:        mockCollectionService{deleteErr: mongoservice.ErrInvalidCollectionName},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "ErrProjectNotAccessible",
			userID:     uuid.New(),
			collection: "users",
			svc:        mockCollectionService{deleteErr: service.ErrProjectNotAccessible},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "internal error",
			userID:     uuid.New(),
			collection: "users",
			svc:        mockCollectionService{deleteErr: errors.New("db down")},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "success",
			userID:     uuid.New(),
			collection: "users",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectID := uuid.New().String()
			params := gin.Params{{Key: "id", Value: projectID}}
			if tt.collection != "" {
				params = append(params, gin.Param{Key: "collection", Value: tt.collection})
			}
			h := NewCollectionHandler(tt.svc)
			c, w := mongoCollectionContext(http.MethodDelete, "/mongodb/collections/placeholder", tt.userID, projectID, nil, params)

			h.DeleteCollection(c)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestCollectionHandler_AddField(t *testing.T) {
	tests := []struct {
		name       string
		userID     uuid.UUID
		svc        mockCollectionService
		wantStatus int
		assertBody func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name:       "unauthorized",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "ErrInvalidFieldName",
			userID:     uuid.New(),
			svc:        mockCollectionService{addErr: mongoservice.ErrInvalidFieldName},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "ErrCollectionNotFound",
			userID:     uuid.New(),
			svc:        mockCollectionService{addErr: mongoservice.ErrCollectionNotFound},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "ErrProjectNotAccessible",
			userID:     uuid.New(),
			svc:        mockCollectionService{addErr: service.ErrProjectNotAccessible},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "internal error",
			userID:     uuid.New(),
			svc:        mockCollectionService{addErr: errors.New("unexpected")},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "success",
			userID:     uuid.New(),
			svc:        mockCollectionService{addResult: &model.FieldUpdateResult{Matched: 5, Modified: 3}},
			wantStatus: http.StatusOK,
			assertBody: assertFieldCounts(5, 3),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectID := uuid.New().String()
			h := NewCollectionHandler(tt.svc)
			c, w := mongoCollectionContext(http.MethodPost, "/mongodb/collections/users/fields", tt.userID, projectID,
				map[string]any{"field": "status", "default": "active"},
				gin.Params{{Key: "id", Value: projectID}, {Key: "collection", Value: "users"}})

			h.AddField(c)

			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.assertBody != nil {
				tt.assertBody(t, w)
			}
		})
	}
}

func TestCollectionHandler_RemoveField(t *testing.T) {
	tests := []struct {
		name       string
		userID     uuid.UUID
		svc        mockCollectionService
		wantStatus int
		assertBody func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name:       "unauthorized",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "ErrInvalidFieldName",
			userID:     uuid.New(),
			svc:        mockCollectionService{removeErr: mongoservice.ErrInvalidFieldName},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "ErrCollectionNotFound",
			userID:     uuid.New(),
			svc:        mockCollectionService{removeErr: mongoservice.ErrCollectionNotFound},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "ErrProjectNotAccessible",
			userID:     uuid.New(),
			svc:        mockCollectionService{removeErr: service.ErrProjectNotAccessible},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "internal error",
			userID:     uuid.New(),
			svc:        mockCollectionService{removeErr: errors.New("unexpected")},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "success",
			userID:     uuid.New(),
			svc:        mockCollectionService{removeResult: &model.FieldUpdateResult{Matched: 4, Modified: 2}},
			wantStatus: http.StatusOK,
			assertBody: assertFieldCounts(4, 2),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectID := uuid.New().String()
			h := NewCollectionHandler(tt.svc)
			c, w := mongoCollectionContext(http.MethodDelete, "/mongodb/collections/users/fields/status", tt.userID, projectID, nil,
				gin.Params{{Key: "id", Value: projectID}, {Key: "collection", Value: "users"}, {Key: "field", Value: "status"}})

			h.RemoveField(c)

			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.assertBody != nil {
				tt.assertBody(t, w)
			}
		})
	}
}

func mongoCollectionContext(method, path string, userID uuid.UUID, projectID string, body any, params gin.Params) (*gin.Context, *httptest.ResponseRecorder) {
	c, w := testutil.NewGinContext(method, path, body, nil)
	if userID != uuid.Nil {
		c.Set(utils.UserIDContextKey, userID)
	}
	c.Params = params
	return c, w
}

func responseData(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, testutil.ParseJSONResponse(w, &body))
	data, ok := body["data"].(map[string]any)
	require.True(t, ok)
	return data
}

func assertFieldCounts(matched, modified int64) func(t *testing.T, w *httptest.ResponseRecorder) {
	return func(t *testing.T, w *httptest.ResponseRecorder) {
		t.Helper()
		data := responseData(t, w)
		assert.Equal(t, float64(matched), data["matched"])
		assert.Equal(t, float64(modified), data["modified"])
	}
}

var _ collectionServiceI = mockCollectionService{}
var _ = errors.New
