package handler

import (
	"backend/internal/postgres/service"
	"backend/internal/responses"
	"backend/internal/services"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// PostgresHandler handles /projects/:id/postgres/* endpoints (tables, rows, columns).
// Table name comes from path param :table. Middleware ensures project is PostgreSQL.
type PostgresHandler struct {
	projectService *services.ProjectService
	tableService   *service.TableService
	recordService  *services.RecordService
}

func NewPostgresHandler(
	projectService *services.ProjectService,
	tableService *service.TableService,
	recordService *services.RecordService,
) *PostgresHandler {
	return &PostgresHandler{
		projectService: projectService,
		tableService:   tableService,
		recordService:  recordService,
	}
}

func postgresGetUserUUID(c *gin.Context) (uuid.UUID, bool) {
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

func postgresGetProjectUUID(c *gin.Context) (uuid.UUID, bool) {
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

// ListTables GET /postgres/tables
func (h *PostgresHandler) ListTables(c *gin.Context) {
	userUUID, ok := postgresGetUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectUUID, ok := postgresGetProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid project ID")
		return
	}
	tables, err := h.recordService.ListContainers(c.Request.Context(), projectUUID, userUUID)
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to list tables")
		return
	}
	responses.Success(c, http.StatusOK, tables, "Tables retrieved successfully")
}

// DeleteTableByPath DELETE /postgres/tables/:table
func (h *PostgresHandler) DeleteTableByPath(c *gin.Context) {
	table := c.Param("table")
	if table == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Table name is required")
		return
	}
	userUUID, ok := postgresGetUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectUUID, ok := postgresGetProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid project ID")
		return
	}
	req := service.DeleteTableRequest{Schema: "public", Table: table}
	result, err := h.tableService.DeleteTable(&req, userUUID, projectUUID)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Failed to delete table")
		return
	}
	responses.Success(c, http.StatusOK, gin.H{"result": result}, "Table deleted successfully")
}

// InsertRowWithTable POST /postgres/tables/:table/rows — body: { "values": { "col": "val", ... } }
func (h *PostgresHandler) InsertRowWithTable(c *gin.Context) {
	table := c.Param("table")
	if table == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Table name is required")
		return
	}
	userUUID, ok := postgresGetUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectUUID, ok := postgresGetProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid project ID")
		return
	}
	var body struct {
		Values map[string]interface{} `json:"values" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}
	req := services.InsertRowRequest{Table: table, Values: body.Values}
	result, err := h.projectService.InsertRow(userUUID, projectUUID, req)
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to insert row")
		return
	}
	responses.Success(c, http.StatusCreated, result, "Row inserted successfully")
}

// GetRows GET /postgres/tables/:table/rows — optional ?filter=<url-encoded-json>
func (h *PostgresHandler) GetRows(c *gin.Context) {
	table := c.Param("table")
	if table == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Table name is required")
		return
	}
	userUUID, ok := postgresGetUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectUUID, ok := postgresGetProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid project ID")
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

// UpdateRows PATCH /postgres/tables/:table/rows — body: { "filter": {}, "update": {} }
func (h *PostgresHandler) UpdateRows(c *gin.Context) {
	table := c.Param("table")
	if table == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Table name is required")
		return
	}
	userUUID, ok := postgresGetUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectUUID, ok := postgresGetProjectUUID(c)
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
	if err := h.recordService.UpdateRecords(c.Request.Context(), projectUUID, userUUID, table, body.Filter, body.Update); err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to update rows")
		return
	}
	responses.Success(c, http.StatusOK, nil, "Rows updated successfully")
}

// DeleteRows DELETE /postgres/tables/:table/rows — optional ?filter= or :row_id in path
func (h *PostgresHandler) DeleteRows(c *gin.Context) {
	table := c.Param("table")
	if table == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Table name is required")
		return
	}
	userUUID, ok := postgresGetUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectUUID, ok := postgresGetProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid project ID")
		return
	}
	rowID := c.Param("row_id")
	if rowID != "" {
		req := services.DeleteRowRequest{TableName: table}
		if err := h.projectService.DeleteRow(userUUID, projectUUID, req, rowID); err != nil {
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

// AddColumnWithTable POST /postgres/tables/:table/columns — body: { "name": "", "type": "" }
func (h *PostgresHandler) AddColumnWithTable(c *gin.Context) {
	table := c.Param("table")
	if table == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Table name is required")
		return
	}
	userUUID, ok := postgresGetUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectUUID, ok := postgresGetProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid project ID")
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
	req := services.AddColumnRequest{TableName: table, Name: body.Name, Type: body.Type, Default: body.Default}
	result, err := h.projectService.AddColumn(userUUID, projectUUID, req)
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to add column")
		return
	}
	responses.Success(c, http.StatusOK, result, "Column added successfully")
}

// DeleteColumnWithTable DELETE /postgres/tables/:table/columns/:column
func (h *PostgresHandler) DeleteColumnWithTable(c *gin.Context) {
	table := c.Param("table")
	column := c.Param("column")
	if table == "" || column == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Table and column are required")
		return
	}
	userUUID, ok := postgresGetUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectUUID, ok := postgresGetProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid project ID")
		return
	}
	req := services.DeleteColumnRequest{TableName: table}
	if err := h.projectService.DeleteColumn(userUUID, projectUUID, req, column); err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to delete column")
		return
	}
	responses.Success(c, http.StatusNoContent, nil, "Column deleted successfully")
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
