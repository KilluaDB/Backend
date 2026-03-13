package handler

import (
	"backend/internal/responses"
	"backend/internal/services"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// MongoDBHandler handles /projects/:id/mongodb/* endpoints (collections, documents).
// Middleware ensures project is MongoDB. Filter for GET/DELETE documents via query param ?filter=.
type MongoDBHandler struct {
	recordService *services.RecordService
}

func NewMongoDBHandler(recordService *services.RecordService) *MongoDBHandler {
	return &MongoDBHandler{recordService: recordService}
}

func mongoGetUserUUID(c *gin.Context) (uuid.UUID, bool) {
	userID, exists := c.Get("userId")
	if !exists {
		return uuid.Nil, false
	}
	switch v := userID.(type) {
	case uuid.UUID:
		return v, true
	case string:
		u, err := uuid.Parse(v)
		if err != nil {
			return uuid.Nil, false
		}
		return u, true
	default:
		return uuid.Nil, false
	}
}

func mongoGetProjectUUID(c *gin.Context) (uuid.UUID, bool) {
	idStr := c.Param("id")
	if idStr == "" {
		return uuid.Nil, false
	}
	u, err := uuid.Parse(idStr)
	if err != nil {
		return uuid.Nil, false
	}
	return u, true
}

// ListCollections GET /mongodb/collections
func (h *MongoDBHandler) ListCollections(c *gin.Context) {
	userUUID, ok := mongoGetUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectUUID, ok := mongoGetProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid project ID")
		return
	}
	collections, err := h.recordService.ListContainers(c.Request.Context(), projectUUID, userUUID)
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to list collections")
		return
	}
	responses.Success(c, http.StatusOK, collections, "Collections retrieved successfully")
}

// CreateCollection POST /mongodb/collections — body: { "name": "..." }
func (h *MongoDBHandler) CreateCollection(c *gin.Context) {
	userUUID, ok := mongoGetUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectUUID, ok := mongoGetProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid project ID")
		return
	}
	var body struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}
	if err := h.recordService.CreateContainer(c.Request.Context(), projectUUID, userUUID, body.Name); err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to create collection")
		return
	}
	responses.Success(c, http.StatusCreated, gin.H{"name": body.Name}, "Collection created successfully")
}

// DeleteCollection DELETE /mongodb/collections/:collection
func (h *MongoDBHandler) DeleteCollection(c *gin.Context) {
	collection := c.Param("collection")
	if collection == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Collection name is required")
		return
	}
	userUUID, ok := mongoGetUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectUUID, ok := mongoGetProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid project ID")
		return
	}
	if err := h.recordService.DeleteContainer(c.Request.Context(), projectUUID, userUUID, collection); err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to delete collection")
		return
	}
	responses.Success(c, http.StatusNoContent, nil, "Collection deleted successfully")
}

// GetDocuments GET /mongodb/collections/:collection/documents — optional ?filter=<url-encoded-json>
func (h *MongoDBHandler) GetDocuments(c *gin.Context) {
	collection := c.Param("collection")
	if collection == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Collection name is required")
		return
	}
	userUUID, ok := mongoGetUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectUUID, ok := mongoGetProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid project ID")
		return
	}
	filter := parseFilterFromQueryMongo(c)
	records, err := h.recordService.GetRecords(c.Request.Context(), projectUUID, userUUID, collection, filter)
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to get documents")
		return
	}
	responses.Success(c, http.StatusOK, records, "Documents retrieved successfully")
}

// InsertDocument POST /mongodb/collections/:collection/documents — body: { "data": { ... } }
func (h *MongoDBHandler) InsertDocument(c *gin.Context) {
	collection := c.Param("collection")
	if collection == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Collection name is required")
		return
	}
	userUUID, ok := mongoGetUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectUUID, ok := mongoGetProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid project ID")
		return
	}
	var body struct {
		Data map[string]interface{} `json:"data" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}
	if err := h.recordService.InsertRecord(c.Request.Context(), projectUUID, userUUID, collection, body.Data); err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to insert document")
		return
	}
	responses.Success(c, http.StatusCreated, nil, "Document inserted successfully")
}

// UpdateDocuments PATCH /mongodb/collections/:collection/documents — body: { "filter": {}, "update": {} }
func (h *MongoDBHandler) UpdateDocuments(c *gin.Context) {
	collection := c.Param("collection")
	if collection == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Collection name is required")
		return
	}
	userUUID, ok := mongoGetUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectUUID, ok := mongoGetProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid project ID")
		return
	}
	var body struct {
		Filter map[string]interface{} `json:"filter"`
		Update map[string]interface{} `json:"update" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}
	if err := h.recordService.UpdateRecords(c.Request.Context(), projectUUID, userUUID, collection, body.Filter, body.Update); err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to update documents")
		return
	}
	responses.Success(c, http.StatusOK, nil, "Documents updated successfully")
}

// DeleteDocuments DELETE /mongodb/collections/:collection/documents — optional ?filter=
func (h *MongoDBHandler) DeleteDocuments(c *gin.Context) {
	collection := c.Param("collection")
	if collection == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Collection name is required")
		return
	}
	userUUID, ok := mongoGetUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectUUID, ok := mongoGetProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid project ID")
		return
	}
	filter := parseFilterFromQueryMongo(c)
	if err := h.recordService.DeleteRecords(c.Request.Context(), projectUUID, userUUID, collection, filter); err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to delete documents")
		return
	}
	responses.Success(c, http.StatusNoContent, nil, "Documents deleted successfully")
}

// AddField POST /mongodb/collections/:collection/fields — body: { "field": "", "field_type": "" }
func (h *MongoDBHandler) AddField(c *gin.Context) {
	collection := c.Param("collection")
	if collection == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Collection name is required")
		return
	}
	userUUID, ok := mongoGetUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectUUID, ok := mongoGetProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid project ID")
		return
	}
	var body struct {
		Field     string `json:"field" binding:"required"`
		FieldType string `json:"field_type"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}
	if err := h.recordService.AddField(c.Request.Context(), projectUUID, userUUID, collection, body.Field, body.FieldType); err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to add field")
		return
	}
	responses.Success(c, http.StatusOK, nil, "Field added successfully")
}

// RemoveField DELETE /mongodb/collections/:collection/fields/:field
func (h *MongoDBHandler) RemoveField(c *gin.Context) {
	collection := c.Param("collection")
	field := c.Param("field")
	if collection == "" || field == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Collection and field are required")
		return
	}
	userUUID, ok := mongoGetUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectUUID, ok := mongoGetProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid project ID")
		return
	}
	if err := h.recordService.RemoveField(c.Request.Context(), projectUUID, userUUID, collection, field); err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to remove field")
		return
	}
	responses.Success(c, http.StatusNoContent, nil, "Field removed successfully")
}

func parseFilterFromQueryMongo(c *gin.Context) map[string]interface{} {
	q := c.Query("filter")
	if q == "" {
		return nil
	}
	var filter map[string]interface{}
	if err := json.Unmarshal([]byte(q), &filter); err != nil {
		return nil
	}
	return filter
}
