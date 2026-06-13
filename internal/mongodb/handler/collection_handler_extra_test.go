package handler

import (
	"errors"
	"net/http"
	"testing"

	mongoservice "backend/internal/mongodb/service"
	"backend/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests extend collection_handler_test.go to cover the remaining
// unauthorized / invalid-param / mapped-error branches that the primary table
// tests did not reach. They use the same mockCollectionService and helpers.

func TestNewMongoHandler(t *testing.T) {
	collection := NewCollectionHandler(mockCollectionService{})
	h := NewMongoHandler(collection, nil, nil)
	require.NotNil(t, h)
	assert.Same(t, collection, h.Collection)
}

func TestFailMongoInstanceError(t *testing.T) {
	// nil error: handlers never reach this arm (they only call it inside an
	// err != nil block), so cover it directly.
	c, _ := mongoCollectionContext(http.MethodGet, "/x", uuid.New(), uuid.New().String(), nil, nil)
	assert.False(t, failMongoInstanceError(c, nil))

	// A non-instance error is not handled here either.
	assert.False(t, failMongoInstanceError(c, errors.New("other")))

	// Instance errors are handled (return true).
	assert.True(t, failMongoInstanceError(c, service.ErrProjectNotAccessible))
	assert.True(t, failMongoInstanceError(c, service.ErrNoRunningInstance))
}

func TestCollectionHandler_ListCollections_genericError(t *testing.T) {
	// A non-instance error maps to 400 "Failed to list collections".
	projectID := uuid.New().String()
	h := NewCollectionHandler(mockCollectionService{listErr: errors.New("boom")})
	c, w := mongoCollectionContext(http.MethodGet, "/mongodb/collections", uuid.New(), projectID, nil,
		gin.Params{{Key: "id", Value: projectID}})

	h.ListCollections(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCollectionHandler_ListCollections_noRunningInstance(t *testing.T) {
	// Exercises the ErrNoRunningInstance arm of failMongoInstanceError -> 404.
	projectID := uuid.New().String()
	h := NewCollectionHandler(mockCollectionService{listErr: service.ErrNoRunningInstance})
	c, w := mongoCollectionContext(http.MethodGet, "/mongodb/collections", uuid.New(), projectID, nil,
		gin.Params{{Key: "id", Value: projectID}})

	h.ListCollections(c)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCollectionHandler_CreateCollection_invalidProjectID(t *testing.T) {
	// userID present but the :id param is not a UUID -> 400 from requireUserAndProject.
	h := NewCollectionHandler(mockCollectionService{})
	c, w := mongoCollectionContext(http.MethodPost, "/mongodb/collections", uuid.New(), "not-a-uuid",
		map[string]any{"name": "users"},
		gin.Params{{Key: "id", Value: "not-a-uuid"}})

	h.CreateCollection(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCollectionHandler_CreateCollection_genericError(t *testing.T) {
	// Unmapped service error falls through to the default 400 "Failed to create collection".
	projectID := uuid.New().String()
	h := NewCollectionHandler(mockCollectionService{createErr: errors.New("boom")})
	c, w := mongoCollectionContext(http.MethodPost, "/mongodb/collections", uuid.New(), projectID,
		map[string]any{"name": "users"},
		gin.Params{{Key: "id", Value: projectID}})

	h.CreateCollection(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCollectionHandler_AddField_branches(t *testing.T) {
	tests := []struct {
		name       string
		userID     uuid.UUID
		collection string
		body       any
		svc        mockCollectionService
		wantStatus int
	}{
		{
			name:       "missing collection param",
			userID:     uuid.New(),
			collection: "", // omitted -> 400 before auth check
			body:       map[string]any{"field": "status"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid body missing field",
			userID:     uuid.New(),
			collection: "users",
			body:       map[string]any{}, // field is required by binding
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "ErrInvalidCollectionName",
			userID:     uuid.New(),
			collection: "users",
			body:       map[string]any{"field": "status"},
			svc:        mockCollectionService{addErr: mongoservice.ErrInvalidCollectionName},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "ErrNoRunningInstance",
			userID:     uuid.New(),
			collection: "users",
			body:       map[string]any{"field": "status"},
			svc:        mockCollectionService{addErr: service.ErrNoRunningInstance},
			wantStatus: http.StatusNotFound,
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
			c, w := mongoCollectionContext(http.MethodPost, "/mongodb/collections/users/fields", tt.userID, projectID, tt.body, params)

			h.AddField(c)
			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestCollectionHandler_RemoveField_branches(t *testing.T) {
	tests := []struct {
		name       string
		userID     uuid.UUID
		projectID  string
		collection string
		field      string
		svc        mockCollectionService
		wantStatus int
	}{
		{
			name:       "missing field param",
			userID:     uuid.New(),
			projectID:  uuid.New().String(),
			collection: "users",
			field:      "", // omitted -> 400 before auth check
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid projectId",
			userID:     uuid.New(),
			projectID:  "not-a-uuid",
			collection: "users",
			field:      "status",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "ErrInvalidCollectionName",
			userID:     uuid.New(),
			projectID:  uuid.New().String(),
			collection: "users",
			field:      "status",
			svc:        mockCollectionService{removeErr: mongoservice.ErrInvalidCollectionName},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "ErrNoRunningInstance",
			userID:     uuid.New(),
			projectID:  uuid.New().String(),
			collection: "users",
			field:      "status",
			svc:        mockCollectionService{removeErr: service.ErrNoRunningInstance},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := gin.Params{{Key: "id", Value: tt.projectID}, {Key: "collection", Value: tt.collection}}
			if tt.field != "" {
				params = append(params, gin.Param{Key: "field", Value: tt.field})
			}
			h := NewCollectionHandler(tt.svc)
			c, w := mongoCollectionContext(http.MethodDelete, "/mongodb/collections/users/fields/status", tt.userID, tt.projectID, nil, params)

			h.RemoveField(c)
			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}
