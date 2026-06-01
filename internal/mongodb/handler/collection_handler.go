package handler

import (
	"backend/internal/mongodb/model"
	mongoservice "backend/internal/mongodb/service"
	"backend/internal/response"
	"backend/internal/service"
	"backend/internal/utils"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// CollectionHandler handles MongoDB collection endpoints.
type CollectionHandler struct {
	collectionService *mongoservice.CollectionService
}

func NewCollectionHandler(collectionService *mongoservice.CollectionService) *CollectionHandler {
	return &CollectionHandler{
		collectionService: collectionService,
	}
}

func requireUserAndProject(c *gin.Context) (userUUID, projectUUID uuid.UUID, ok bool) {
	u, p, ok, projErr := utils.UserAndProjectFromGin(c)
	if !ok {
		if projErr != nil {
			response.Fail(c, http.StatusBadRequest, projErr, "Invalid projectId format")
		} else {
			response.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		}
		return uuid.Nil, uuid.Nil, false
	}
	return u, p, true
}

func failMongoInstanceError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, service.ErrProjectNotAccessible) || errors.Is(err, service.ErrNoRunningInstance) {
		response.Fail(c, http.StatusNotFound, err, "Project not found or database instance not ready")
		return true
	}
	return false
}

// ListCollections GET /mongodb/collections
func (h *CollectionHandler) ListCollections(c *gin.Context) {
	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}
	list, err := h.collectionService.ListCollections(c.Request.Context(), userUUID, projectUUID)
	if err != nil {
		if failMongoInstanceError(c, err) {
			return
		}
		response.Fail(c, http.StatusBadRequest, err, "Failed to list collections")
		return
	}
	response.Success(c, http.StatusOK, gin.H{"collections": list}, "Collections retrieved successfully")
}

// CreateCollection POST /mongodb/collections
func (h *CollectionHandler) CreateCollection(c *gin.Context) {
	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}
	var req model.CreateCollectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}
	if err := h.collectionService.CreateCollection(c.Request.Context(), userUUID, projectUUID, req.Name); err != nil {
		switch {
		case errors.Is(err, mongoservice.ErrInvalidCollectionName):
			response.Fail(c, http.StatusBadRequest, err, "Invalid collection name")
			return
		case errors.Is(err, mongoservice.ErrCollectionAlreadyExists):
			response.Fail(c, http.StatusConflict, err, "Collection already exists")
			return
		case failMongoInstanceError(c, err):
			return
		default:
			response.Fail(c, http.StatusBadRequest, err, "Failed to create collection")
			return
		}
	}
	response.Success(c, http.StatusCreated, gin.H{"name": req.Name}, "Collection created successfully")
}

// DeleteCollection DELETE /mongodb/collections/:collection
func (h *CollectionHandler) DeleteCollection(c *gin.Context) {
	collection := c.Param("collection")
	if collection == "" {
		response.Fail(c, http.StatusBadRequest, nil, "Collection name is required")
		return
	}
	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}
	if err := h.collectionService.DeleteCollection(c.Request.Context(), userUUID, projectUUID, collection); err != nil {
		switch {
		case errors.Is(err, mongoservice.ErrInvalidCollectionName):
			response.Fail(c, http.StatusBadRequest, err, "Invalid collection name")
			return
		case errors.Is(err, mongoservice.ErrCollectionNotFound):
			response.Fail(c, http.StatusNotFound, err, "Collection does not exist")
			return
		case failMongoInstanceError(c, err):
			return
		default:
			response.Fail(c, http.StatusBadRequest, err, "Failed to delete collection")
			return
		}
	}
	response.Success(c, http.StatusOK, gin.H{"name": collection}, "Collection deleted successfully")
}

// AddField POST /mongodb/collections/:collection/fields
func (h *CollectionHandler) AddField(c *gin.Context) {
	collection := c.Param("collection")
	if collection == "" {
		response.Fail(c, http.StatusBadRequest, nil, "Collection name is required")
		return
	}
	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}
	var req model.AddFieldRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}
	result, err := h.collectionService.AddField(c.Request.Context(), userUUID, projectUUID, collection, req)
	if err != nil {
		switch {
		case errors.Is(err, mongoservice.ErrInvalidCollectionName):
			response.Fail(c, http.StatusBadRequest, err, "Invalid collection name")
			return
		case errors.Is(err, mongoservice.ErrInvalidFieldName):
			response.Fail(c, http.StatusBadRequest, err, "Invalid field name")
			return
		case errors.Is(err, mongoservice.ErrCollectionNotFound):
			response.Fail(c, http.StatusNotFound, err, "Collection does not exist")
			return
		case failMongoInstanceError(c, err):
			return
		default:
			response.Fail(c, http.StatusBadRequest, err, "Failed to add field")
			return
		}
	}
	response.Success(c, http.StatusOK, result, "Field added successfully")
}

// RemoveField DELETE /mongodb/collections/:collection/fields/:field
func (h *CollectionHandler) RemoveField(c *gin.Context) {
	collection := c.Param("collection")
	field := c.Param("field")
	if collection == "" || field == "" {
		response.Fail(c, http.StatusBadRequest, nil, "Collection and field are required")
		return
	}
	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}
	result, err := h.collectionService.RemoveField(c.Request.Context(), userUUID, projectUUID, collection, field)
	if err != nil {
		switch {
		case errors.Is(err, mongoservice.ErrInvalidCollectionName):
			response.Fail(c, http.StatusBadRequest, err, "Invalid collection name")
			return
		case errors.Is(err, mongoservice.ErrInvalidFieldName):
			response.Fail(c, http.StatusBadRequest, err, "Invalid field name")
			return
		case errors.Is(err, mongoservice.ErrCollectionNotFound):
			response.Fail(c, http.StatusNotFound, err, "Collection does not exist")
			return
		case failMongoInstanceError(c, err):
			return
		default:
			response.Fail(c, http.StatusBadRequest, err, "Failed to remove field")
			return
		}
	}
	response.Success(c, http.StatusOK, result, "Field removed successfully")
}
