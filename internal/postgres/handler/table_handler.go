package handler

import (
	"backend/internal/postgres/model"
	"backend/internal/postgres/service"
	"backend/internal/responses"
	"backend/internal/services"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type TableHandler struct {
	tableService *service.TableService
	recordService *services.RecordService
}

func NewTableHandler(tableService *service.TableService, recordService *services.RecordService) *TableHandler {
	return &TableHandler{
		tableService:  tableService,
		recordService: recordService,
	}
}

func (h *TableHandler) CreateTable(c *gin.Context) {
	projectId := c.Param("id")
	if projectId == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Project id is required")
		return
	}

	userId, exists := c.Get("userId")
	if !exists {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	var req model.CreateTableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	userUUID, err := h.toUUID(userId)
	if err != nil {
		responses.Fail(c, http.StatusUnauthorized, err, "Invalid user ID format")
		return
	}

	projectUUID, err := uuid.Parse(projectId)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid projectId format")
		return
	}

	result, err := h.tableService.CreateTable(&req, userUUID, projectUUID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidTableRequest):
			responses.Fail(c, http.StatusBadRequest, err, "Invalid request: schema, table, or column names or types are invalid")
			return
		case errors.Is(err, service.ErrTableAlreadyExists):
			responses.Fail(c, http.StatusConflict, err, "Table already exists")
			return
		case errors.Is(err, services.ErrProjectNotAccessible), errors.Is(err, services.ErrNoRunningInstance):
			responses.Fail(c, http.StatusNotFound, err, "Project not found or database instance not ready")
			return
		default:
			responses.Fail(c, http.StatusInternalServerError, err, "Failed to create table")
			return
		}
	}

	response := gin.H{
		"result": result,
	}

	responses.Success(c, http.StatusCreated, response, "Table created successfully")
}

func (h *TableHandler) DeleteTable(c *gin.Context) {
	projectId := c.Param("id")
	if projectId == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Project id is required")
		return
	}

	userId, exists := c.Get("userId")
	if !exists {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	var req model.DeleteTableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	userUUID, err := h.toUUID(userId)
	if err != nil {
		responses.Fail(c, http.StatusUnauthorized, err, "Invalid user Id format")
		return
	}

	projectUUID, err := uuid.Parse(projectId)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid projectId format")
		return
	}

	result, err := h.tableService.DeleteTable(&req, userUUID, projectUUID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidTableRequest):
			responses.Fail(c, http.StatusBadRequest, err, "Invalid request: schema or table name is invalid")
			return
		case errors.Is(err, service.ErrTableNotFound):
			responses.Fail(c, http.StatusNotFound, err, "Table does not exist")
			return
		case errors.Is(err, services.ErrProjectNotAccessible), errors.Is(err, services.ErrNoRunningInstance):
			responses.Fail(c, http.StatusNotFound, err, "Project not found or database instance not ready")
			return
		default:
			responses.Fail(c, http.StatusInternalServerError, err, "Failed to delete table")
			return
		}
	}

	response := gin.H{
		"result": result,
	}

	responses.Success(c, http.StatusOK, response, "Table deleted successfully")
}

func (h *TableHandler) toUUID(userId any) (uuid.UUID, error) {
	switch v := userId.(type) {
	case uuid.UUID:
		return v, nil
	case string:
		parsed, err := uuid.Parse(v)
		if err != nil {
			return uuid.Nil, err
		}
		return parsed, nil
	default:
		return uuid.Nil, fmt.Errorf("invalid user Id type: %T", v)
	}
}

func (h *TableHandler) getUserAndProjectUUID(c *gin.Context) (userUUID, projectUUID uuid.UUID, ok bool) {
	userID, exists := c.Get("userId")
	if !exists {
		return uuid.Nil, uuid.Nil, false
	}
	userUUID, err := h.toUUID(userID)
	if err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	projectID := c.Param("id")
	if projectID == "" {
		return uuid.Nil, uuid.Nil, false
	}
	projectUUID, err = uuid.Parse(projectID)
	if err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	return userUUID, projectUUID, true
}

func parseFilterFromQuery(c *gin.Context) map[string]interface{} {
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

// InsertRowWithTable POST /postgres/tables/:table/rows
func (h *TableHandler) InsertRowWithTable(c *gin.Context) {
	table := c.Param("table")
	if table == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Table name is required")
		return
	}
	userUUID, projectUUID, ok := h.getUserAndProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	var body struct {
		Values map[string]interface{} `json:"values" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}
	req := service.InsertRowRequest{Table: table, Values: body.Values}
	result, err := h.tableService.InsertRow(c.Request.Context(), userUUID, projectUUID, req)
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to insert row")
		return
	}
	responses.Success(c, http.StatusCreated, result, "Row inserted successfully")
}

// GetRows GET /postgres/tables/:table/rows
func (h *TableHandler) GetRows(c *gin.Context) {
	table := c.Param("table")
	if table == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Table name is required")
		return
	}
	userUUID, projectUUID, ok := h.getUserAndProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	filter := parseFilterFromQuery(c)
	records, err := h.recordService.GetRecords(c.Request.Context(), projectUUID, userUUID, table, filter)
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to get rows")
		return
	}
	responses.Success(c, http.StatusOK, records, "Rows retrieved successfully")
}

// UpdateRows PATCH /postgres/tables/:table/rows
func (h *TableHandler) UpdateRows(c *gin.Context) {
	table := c.Param("table")
	if table == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Table name is required")
		return
	}
	userUUID, projectUUID, ok := h.getUserAndProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
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
	if err := h.recordService.UpdateRecords(c.Request.Context(), projectUUID, userUUID, table, body.Filter, body.Update); err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to update rows")
		return
	}
	responses.Success(c, http.StatusOK, nil, "Rows updated successfully")
}

// DeleteRows DELETE /postgres/tables/:table/rows or .../rows/:row_id
func (h *TableHandler) DeleteRows(c *gin.Context) {
	table := c.Param("table")
	if table == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Table name is required")
		return
	}
	userUUID, projectUUID, ok := h.getUserAndProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	rowID := c.Param("row_id")
	if rowID != "" {
		req := service.DeleteRowRequest{TableName: table}
		if err := h.tableService.DeleteRow(c.Request.Context(), userUUID, projectUUID, req, rowID); err != nil {
			if err.Error() == "row not found" {
				responses.Fail(c, http.StatusNotFound, err, "Row not found")
				return
			}
			responses.Fail(c, http.StatusInternalServerError, err, "Failed to delete row")
			return
		}
		responses.Success(c, http.StatusNoContent, nil, "Row deleted successfully")
		return
	}
	filter := parseFilterFromQuery(c)
	if err := h.recordService.DeleteRecords(c.Request.Context(), projectUUID, userUUID, table, filter); err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to delete rows")
		return
	}
	responses.Success(c, http.StatusNoContent, nil, "Rows deleted successfully")
}

// AddColumnWithTable POST /postgres/tables/:table/columns
func (h *TableHandler) AddColumnWithTable(c *gin.Context) {
	table := c.Param("table")
	if table == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Table name is required")
		return
	}
	userUUID, projectUUID, ok := h.getUserAndProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	var body struct {
		Name    string      `json:"name" binding:"required"`
		Type    string      `json:"type" binding:"required"`
		Default interface{} `json:"default,omitempty"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}
	req := service.AddColumnRequest{TableName: table, Name: body.Name, Type: body.Type, Default: body.Default}
	result, err := h.tableService.AddColumn(c.Request.Context(), userUUID, projectUUID, req)
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to add column")
		return
	}
	responses.Success(c, http.StatusOK, result, "Column added successfully")
}

// DeleteColumnWithTable DELETE /postgres/tables/:table/columns/:column
func (h *TableHandler) DeleteColumnWithTable(c *gin.Context) {
	table := c.Param("table")
	column := c.Param("column")
	if table == "" || column == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Table and column are required")
		return
	}
	userUUID, projectUUID, ok := h.getUserAndProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	req := service.DeleteColumnRequest{TableName: table}
	if err := h.tableService.DeleteColumn(c.Request.Context(), userUUID, projectUUID, req, column); err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to delete column")
		return
	}
	responses.Success(c, http.StatusNoContent, nil, "Column deleted successfully")
}
