package handlers

import (
	"backend/internal/responses"
	"backend/internal/services"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ContainerHandler struct {
	recordService *services.RecordService
}

func NewContainerHandler(recordService *services.RecordService) *ContainerHandler {
	return &ContainerHandler{recordService: recordService}
}

// Helper to normalize userId from Gin context into uuid.UUID.
func getUserUUID(c *gin.Context) (uuid.UUID, bool) {
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

// --- Containers ---

// POST /api/v1/projects/:id/containers
type createContainerRequest struct {
	Name string `json:"name" binding:"required"`
}

func (h *ContainerHandler) CreateContainer(c *gin.Context) {
	userUUID, ok := getUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	projectIDStr := c.Param("id")
	projectUUID, err := uuid.Parse(projectIDStr)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid project ID")
		return
	}

	var req createContainerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	if err := h.recordService.CreateContainer(c.Request.Context(), projectUUID, userUUID, req.Name); err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to create container")
		return
	}

	responses.Success(c, http.StatusCreated, gin.H{"name": req.Name}, "Container created successfully")
}

// GET /api/v1/projects/:id/containers
func (h *ContainerHandler) ListContainers(c *gin.Context) {
	userUUID, ok := getUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	projectIDStr := c.Param("id")
	projectUUID, err := uuid.Parse(projectIDStr)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid project ID")
		return
	}

	containers, err := h.recordService.ListContainers(c.Request.Context(), projectUUID, userUUID)
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to list containers")
		return
	}

	responses.Success(c, http.StatusOK, containers, "Containers retrieved successfully")
}

// DELETE /api/v1/projects/:id/containers/:container
func (h *ContainerHandler) DeleteContainer(c *gin.Context) {
	userUUID, ok := getUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	projectIDStr := c.Param("id")
	projectUUID, err := uuid.Parse(projectIDStr)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid project ID")
		return
	}

	container := c.Param("container")
	if container == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Container name is required")
		return
	}

	if err := h.recordService.DeleteContainer(c.Request.Context(), projectUUID, userUUID, container); err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to delete container")
		return
	}

	responses.Success(c, http.StatusNoContent, nil, "Container deleted successfully")
}

// --- Records ---

// POST /api/v1/projects/:id/containers/:container/records
type insertRecordRequest struct {
	Data map[string]interface{} `json:"data" binding:"required"`
}

func (h *ContainerHandler) InsertRecord(c *gin.Context) {
	userUUID, ok := getUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	projectIDStr := c.Param("id")
	projectUUID, err := uuid.Parse(projectIDStr)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid project ID")
		return
	}

	container := c.Param("container")
	if container == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Container name is required")
		return
	}

	var req insertRecordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	if err := h.recordService.InsertRecord(c.Request.Context(), projectUUID, userUUID, container, req.Data); err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to insert record")
		return
	}

	responses.Success(c, http.StatusCreated, nil, "Record inserted successfully")
}

// GET /api/v1/projects/:id/containers/:container/records
type getRecordsRequest struct {
	Filter map[string]interface{} `json:"filter"`
}

func (h *ContainerHandler) GetRecords(c *gin.Context) {
	userUUID, ok := getUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	projectIDStr := c.Param("id")
	projectUUID, err := uuid.Parse(projectIDStr)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid project ID")
		return
	}

	container := c.Param("container")
	if container == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Container name is required")
		return
	}

	var req getRecordsRequest
	_ = c.ShouldBindJSON(&req) // optional body filter

	records, err := h.recordService.GetRecords(c.Request.Context(), projectUUID, userUUID, container, req.Filter)
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to get records")
		return
	}

	responses.Success(c, http.StatusOK, records, "Records retrieved successfully")
}

// PATCH /api/v1/projects/:id/containers/:container/records
type updateRecordsRequest struct {
	Filter map[string]interface{} `json:"filter"`
	Update map[string]interface{} `json:"update" binding:"required"`
}

func (h *ContainerHandler) UpdateRecords(c *gin.Context) {
	userUUID, ok := getUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	projectIDStr := c.Param("id")
	projectUUID, err := uuid.Parse(projectIDStr)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid project ID")
		return
	}

	container := c.Param("container")
	if container == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Container name is required")
		return
	}

	var req updateRecordsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	if err := h.recordService.UpdateRecords(c.Request.Context(), projectUUID, userUUID, container, req.Filter, req.Update); err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to update records")
		return
	}

	responses.Success(c, http.StatusOK, nil, "Records updated successfully")
}

// DELETE /api/v1/projects/:id/containers/:container/records
type deleteRecordsRequest struct {
	Filter map[string]interface{} `json:"filter"`
}

func (h *ContainerHandler) DeleteRecords(c *gin.Context) {
	userUUID, ok := getUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	projectIDStr := c.Param("id")
	projectUUID, err := uuid.Parse(projectIDStr)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid project ID")
		return
	}

	container := c.Param("container")
	if container == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Container name is required")
		return
	}

	var req deleteRecordsRequest
	_ = c.ShouldBindJSON(&req) // optional filter

	if err := h.recordService.DeleteRecords(c.Request.Context(), projectUUID, userUUID, container, req.Filter); err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to delete records")
		return
	}

	responses.Success(c, http.StatusNoContent, nil, "Records deleted successfully")
}

// --- Fields ---

// POST /api/v1/projects/:id/containers/:container/fields
type addFieldRequest struct {
	Field     string `json:"field" binding:"required"`
	FieldType string `json:"field_type"` // required for Postgres, ignored for Mongo
}

func (h *ContainerHandler) AddField(c *gin.Context) {
	userUUID, ok := getUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	projectIDStr := c.Param("id")
	projectUUID, err := uuid.Parse(projectIDStr)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid project ID")
		return
	}

	container := c.Param("container")
	if container == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Container name is required")
		return
	}

	var req addFieldRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	if err := h.recordService.AddField(c.Request.Context(), projectUUID, userUUID, container, req.Field, req.FieldType); err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to add field")
		return
	}

	responses.Success(c, http.StatusOK, nil, "Field added successfully")
}

// DELETE /api/v1/projects/:id/containers/:container/fields/:field
func (h *ContainerHandler) RemoveField(c *gin.Context) {
	userUUID, ok := getUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	projectIDStr := c.Param("id")
	projectUUID, err := uuid.Parse(projectIDStr)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid project ID")
		return
	}

	container := c.Param("container")
	field := c.Param("field")
	if container == "" || field == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Container and field are required")
		return
	}

	if err := h.recordService.RemoveField(c.Request.Context(), projectUUID, userUUID, container, field); err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to remove field")
		return
	}

	responses.Success(c, http.StatusNoContent, nil, "Field removed successfully")
}

