package handler

import (
	"backend/internal/postgres/model"
	"backend/internal/postgres/repository"
	pgservice "backend/internal/postgres/service"
	"backend/internal/response"
	"backend/internal/service"
	"backend/internal/utils"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type TableHandler struct {
	tableService *pgservice.TableService
}

func NewTableHandler(tableService *pgservice.TableService) *TableHandler {
	return &TableHandler{
		tableService: tableService,
	}
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

// parseFilterForDeleteRows uses JSON body {"filter": {...}} when present (typical for frontend),
// otherwise ?filter= URL-encoded JSON (same shape as GET rows).
func parseFilterForDeleteRows(c *gin.Context) map[string]interface{} {
	ct := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type")))
	if strings.HasPrefix(ct, "application/json") && c.Request.ContentLength > 0 {
		var body struct {
			Filter map[string]interface{} `json:"filter"`
		}
		if err := c.ShouldBindJSON(&body); err == nil && body.Filter != nil {
			return body.Filter
		}
	}
	return parseFilterFromQuery(c)
}

// requireUserAndProject parses auth user and path project id. On failure writes 400 for invalid :id or 401 for missing user.
func requireUserAndProject(c *gin.Context) (userUUID, projectUUID uuid.UUID, ok bool) {
	u, p, ok, projErr := utils.UserAndProjectFromGin(c)
	if !ok {
		if projErr != nil {
			pgFail(c, http.StatusBadRequest, projErr, "Invalid projectId format")
		} else {
			pgFail(c, http.StatusUnauthorized, nil, "Unauthorized")
		}
		return uuid.Nil, uuid.Nil, false
	}
	return u, p, true
}

// failTableInstanceError maps connection/access errors to 404; returns true if handled.
func failTableInstanceError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, service.ErrProjectNotAccessible) || errors.Is(err, service.ErrNoRunningInstance) {
		pgFail(c, http.StatusNotFound, err, "Project not found or database instance not ready")
		return true
	}
	return false
}

// schemaQueryOrDefault reads `schema` from the query string, defaulting empty to "public", validates it, or writes 400.
func schemaQueryOrDefault(c *gin.Context) (string, bool) {
	raw := c.Query("schema")
	if err := pgservice.ValidatePostgresSchemaName(raw); err != nil {
		pgFail(c, http.StatusBadRequest, err, err.Error())
		return "", false
	}
	return pgservice.PostgresSchema(raw), true
}

// CreateTable post /postgres/tables
func (h *TableHandler) CreateTable(c *gin.Context) {
	userUUID, ok := utils.UserIDFromGin(c)
	if !ok {
		pgFail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectUUID, err := utils.ProjectIDFromGin(c)
	if err != nil {
		pgFail(c, http.StatusBadRequest, err, "Invalid projectId format")
		return
	}

	var req model.CreateTableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pgFail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	result, err := h.tableService.CreateTable(c.Request.Context(), &req, userUUID, projectUUID)
	if err != nil {
		switch {
		case errors.Is(err, pgservice.ErrInvalidTableRequest):
			pgFail(c, http.StatusBadRequest, err, "Invalid request: schema, table, or column names or types are invalid")
			return
		case errors.Is(err, pgservice.ErrTableAlreadyExists):
			pgFail(c, http.StatusConflict, err, "Table already exists")
			return
		case errors.Is(err, service.ErrProjectNotAccessible), errors.Is(err, service.ErrNoRunningInstance):
			pgFail(c, http.StatusNotFound, err, "Project not found or database instance not ready")
			return
		default:
			pgFail(c, http.StatusBadRequest, err, "Failed to create table")
			return
		}
	}

	res := gin.H{
		"result": result,
	}

	response.Success(c, http.StatusCreated, res, "Table created successfully")
}

// GetTables GET /postgres/tables
func (h *TableHandler) GetTables(c *gin.Context) {
	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}
	schema, ok := schemaQueryOrDefault(c)
	if !ok {
		return
	}
	tables, err := h.tableService.GetTables(c.Request.Context(), projectUUID, userUUID, schema)
	if err != nil {
		if failTableInstanceError(c, err) {
			return
		}
		pgFail(c, http.StatusBadRequest, err, "Failed to list tables")
		return
	}
	response.Success(c, http.StatusOK, tables, "Tables retrieved successfully")
}

// GetTable GET /postgres/tables/:table — column and key metadata (no rows).
func (h *TableHandler) GetTable(c *gin.Context) {
	table := c.Param("table")
	if table == "" {
		pgFail(c, http.StatusBadRequest, nil, "Table name is required")
		return
	}
	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}
	schema, ok := schemaQueryOrDefault(c)
	if !ok {
		return
	}
	meta, err := h.tableService.GetTableMetadata(c.Request.Context(), projectUUID, userUUID, schema, table)
	if err != nil {
		if errors.Is(err, pgservice.ErrTableNotFound) {
			pgFail(c, http.StatusNotFound, err, "Table does not exist")
			return
		}
		if failTableInstanceError(c, err) {
			return
		}
		pgFail(c, http.StatusBadRequest, err, err.Error())
		return
	}
	response.Success(c, http.StatusOK, meta, "Table metadata retrieved successfully")
}

// DeleteTable DELETE /postgres/tables/:table
func (h *TableHandler) DeleteTable(c *gin.Context) {
	userUUID, ok := utils.UserIDFromGin(c)
	if !ok {
		pgFail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectUUID, err := utils.ProjectIDFromGin(c)
	if err != nil {
		pgFail(c, http.StatusBadRequest, err, "Invalid projectId format")
		return
	}

	var req model.DeleteTableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pgFail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	result, err := h.tableService.DeleteTable(c.Request.Context(), &req, userUUID, projectUUID)
	if err != nil {
		switch {
		case errors.Is(err, pgservice.ErrInvalidTableRequest):
			pgFail(c, http.StatusBadRequest, err, "Invalid request: schema or table name is invalid")
			return
		case errors.Is(err, pgservice.ErrTableNotFound):
			pgFail(c, http.StatusNotFound, err, "Table does not exist")
			return
		case errors.Is(err, service.ErrProjectNotAccessible), errors.Is(err, service.ErrNoRunningInstance):
			pgFail(c, http.StatusNotFound, err, "Project not found or database instance not ready")
			return
		default:
			pgFail(c, http.StatusBadRequest, err, "Failed to delete table")
			return
		}
	}

	res := gin.H{
		"result": result,
	}

	response.Success(c, http.StatusOK, res, "Table deleted successfully")
}

// UpdateTable PATCH /postgres/tables/:table
func (h *TableHandler) UpdateTable(c *gin.Context) {
	table := c.Param("table")
	if table == "" {
		pgFail(c, http.StatusBadRequest, nil, "Table name is required")
		return
	}
	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}
	schema, ok := schemaQueryOrDefault(c)
	if !ok {
		return
	}
	var req model.UpdateTableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pgFail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	result, err := h.tableService.UpdateTable(c.Request.Context(), userUUID, projectUUID, schema, table, &req)
	if err != nil {
		switch {
		case errors.Is(err, pgservice.ErrInvalidTableRequest):
			pgFail(c, http.StatusBadRequest, err, err.Error())
			return
		case errors.Is(err, pgservice.ErrTableNotFound):
			pgFail(c, http.StatusNotFound, err, "Table does not exist")
			return
		case errors.Is(err, service.ErrProjectNotAccessible), errors.Is(err, service.ErrNoRunningInstance):
			pgFail(c, http.StatusNotFound, err, "Project not found or database instance not ready")
			return
		default:
			pgFail(c, http.StatusBadRequest, err, err.Error())
			return
		}
	}

	response.Success(c, http.StatusOK, gin.H{"result": result}, "Table updated successfully")
}

// InsertRow POST /postgres/tables/:table/rows
func (h *TableHandler) InsertRow(c *gin.Context) {
	table := c.Param("table")
	if table == "" {
		pgFail(c, http.StatusBadRequest, nil, "Table name is required")
		return
	}
	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}
	schema, ok := schemaQueryOrDefault(c)
	if !ok {
		return
	}
	var body struct {
		Values map[string]interface{} `json:"values" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		pgFail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}
	req := pgservice.InsertRowRequest{Schema: schema, Table: table, Values: body.Values}
	result, err := h.tableService.InsertRow(c.Request.Context(), userUUID, projectUUID, req)
	if err != nil {
		if failTableInstanceError(c, err) {
			return
		}
		pgFail(c, http.StatusBadRequest, err, err.Error())
		return
	}
	response.Success(c, http.StatusCreated, result, "Row inserted successfully")
}

// GetRows GET /postgres/tables/:table/rows
func (h *TableHandler) GetRows(c *gin.Context) {
	table := c.Param("table")
	if table == "" {
		pgFail(c, http.StatusBadRequest, nil, "Table name is required")
		return
	}
	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}
	schema, ok := schemaQueryOrDefault(c)
	if !ok {
		return
	}
	filter := parseFilterFromQuery(c)

	limit := repository.MaxGetRowsLimit
	if ls := c.Query("limit"); ls != "" {
		l, err := strconv.Atoi(ls)
		if err != nil || l < 1 {
			pgFail(c, http.StatusBadRequest, nil, "limit must be a positive integer")
			return
		}
		if l > repository.MaxGetRowsLimit {
			pgFail(c, http.StatusBadRequest, nil, "limit cannot exceed "+strconv.Itoa(repository.MaxGetRowsLimit))
			return
		}
		limit = l
	}

	offset := 0
	if os := c.Query("offset"); os != "" {
		o, err := strconv.Atoi(os)
		if err != nil || o < 0 {
			pgFail(c, http.StatusBadRequest, nil, "offset must be a non-negative integer")
			return
		}
		offset = o
	}

	includeTotal := strings.EqualFold(c.Query("include_total"), "1") || strings.EqualFold(c.Query("include_total"), "true")

	records, err := h.tableService.GetRows(c.Request.Context(), projectUUID, userUUID, schema, table, filter, limit, offset, includeTotal)
	if err != nil {
		if failTableInstanceError(c, err) {
			return
		}
		pgFail(c, http.StatusBadRequest, err, err.Error())
		return
	}
	response.Success(c, http.StatusOK, records, "Rows retrieved successfully")
}

// UpdateRows PATCH /postgres/tables/:table/rows
func (h *TableHandler) UpdateRows(c *gin.Context) {
	table := c.Param("table")
	if table == "" {
		pgFail(c, http.StatusBadRequest, nil, "Table name is required")
		return
	}
	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}
	schema, ok := schemaQueryOrDefault(c)
	if !ok {
		return
	}
	var body struct {
		Filter map[string]interface{} `json:"filter"`
		Update map[string]interface{} `json:"update" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		pgFail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}
	if err := h.tableService.UpdateRows(c.Request.Context(), projectUUID, userUUID, schema, table, body.Filter, body.Update); err != nil {
		if failTableInstanceError(c, err) {
			return
		}
		pgFail(c, http.StatusBadRequest, err, err.Error())
		return
	}
	response.Success(c, http.StatusOK, nil, "Rows updated successfully")
}

// DeleteRows DELETE /postgres/tables/:table/rows
func (h *TableHandler) DeleteRows(c *gin.Context) {
	table := c.Param("table")
	if table == "" {
		pgFail(c, http.StatusBadRequest, nil, "Table name is required")
		return
	}
	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}
	schema, ok := schemaQueryOrDefault(c)
	if !ok {
		return
	}
	filter := parseFilterForDeleteRows(c)
	if err := h.tableService.DeleteRowsByFilter(c.Request.Context(), userUUID, projectUUID, schema, table, filter); err != nil {
		if failTableInstanceError(c, err) {
			return
		}
		pgFail(c, http.StatusBadRequest, err, err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

// AddColumn POST /postgres/tables/:table/columns
func (h *TableHandler) AddColumn(c *gin.Context) {
	table := c.Param("table")
	if table == "" {
		pgFail(c, http.StatusBadRequest, nil, "Table name is required")
		return
	}
	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}
	schema, ok := schemaQueryOrDefault(c)
	if !ok {
		return
	}
	var body struct {
		Name        string                      `json:"name" binding:"required"`
		Type        string                      `json:"type" binding:"required"`
		Default     interface{}                 `json:"default,omitempty"`
		Primary     bool                        `json:"primary,omitempty"`
		IsUnique    bool                        `json:"is_unique,omitempty"`
		IsIdentity  bool                        `json:"is_identity,omitempty"`
		Nullable    *bool                       `json:"nullable,omitempty"`
		ForeignKeys []model.AddColumnForeignKey `json:"foreign_keys,omitempty"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		pgFail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}
	nullable := true
	if body.Nullable != nil {
		nullable = *body.Nullable
	}
	req := pgservice.AddColumnRequest{
		Schema:      schema,
		TableName:   table,
		Name:        body.Name,
		Type:        body.Type,
		Default:     body.Default,
		Primary:     body.Primary,
		IsUnique:    body.IsUnique,
		IsIdentity:  body.IsIdentity,
		Nullable:    nullable,
		ForeignKeys: body.ForeignKeys,
	}
	result, err := h.tableService.AddColumn(c.Request.Context(), userUUID, projectUUID, req)
	if err != nil {
		switch {
		case errors.Is(err, pgservice.ErrInvalidTableRequest):
			pgFail(c, http.StatusBadRequest, err, err.Error())
			return
		case errors.Is(err, pgservice.ErrTableNotFound):
			pgFail(c, http.StatusNotFound, err, "Table does not exist")
			return
		}
		if failTableInstanceError(c, err) {
			return
		}
		pgFail(c, http.StatusBadRequest, err, err.Error())
		return
	}
	response.Success(c, http.StatusOK, result, "Column added successfully")
}

// DropColumn DELETE /postgres/tables/:table/columns/:column
func (h *TableHandler) DropColumn(c *gin.Context) {
	table := c.Param("table")
	column := c.Param("column")
	if table == "" || column == "" {
		pgFail(c, http.StatusBadRequest, nil, "Table and column are required")
		return
	}
	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}
	schema, ok := schemaQueryOrDefault(c)
	if !ok {
		return
	}
	req := pgservice.DeleteColumnRequest{Schema: schema, TableName: table}
	if err := h.tableService.DeleteColumn(c.Request.Context(), userUUID, projectUUID, req, column); err != nil {
		if failTableInstanceError(c, err) {
			return
		}
		pgFail(c, http.StatusBadRequest, err, err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

// ListIndexes GET /postgres/tables/:table/indexes
func (h *TableHandler) ListIndexes(c *gin.Context) {
	table := c.Param("table")
	if table == "" {
		pgFail(c, http.StatusBadRequest, nil, "Table name is required")
		return
	}
	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}
	schema, ok := schemaQueryOrDefault(c)
	if !ok {
		return
	}
	list, err := h.tableService.ListTableIndexes(c.Request.Context(), projectUUID, userUUID, schema, table)
	if err != nil {
		if errors.Is(err, pgservice.ErrTableNotFound) {
			pgFail(c, http.StatusNotFound, err, "Table does not exist")
			return
		}
		if failTableInstanceError(c, err) {
			return
		}
		pgFail(c, http.StatusBadRequest, err, err.Error())
		return
	}
	response.Success(c, http.StatusOK, list, "Indexes listed successfully")
}

// CreateIndex POST /postgres/tables/:table/indexes
func (h *TableHandler) CreateIndex(c *gin.Context) {
	table := c.Param("table")
	if table == "" {
		pgFail(c, http.StatusBadRequest, nil, "Table name is required")
		return
	}
	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}
	schema, ok := schemaQueryOrDefault(c)
	if !ok {
		return
	}
	var req model.CreateIndexRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pgFail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}
	if err := h.tableService.CreateTableIndex(c.Request.Context(), projectUUID, userUUID, schema, table, &req); err != nil {
		switch {
		case errors.Is(err, pgservice.ErrInvalidTableRequest):
			pgFail(c, http.StatusBadRequest, err, err.Error())
			return
		case errors.Is(err, pgservice.ErrIndexAlreadyExists):
			pgFail(c, http.StatusConflict, err, "Index already exists")
			return
		case errors.Is(err, pgservice.ErrTableNotFound):
			pgFail(c, http.StatusNotFound, err, "Table does not exist")
			return
		}
		if failTableInstanceError(c, err) {
			return
		}
		pgFail(c, http.StatusBadRequest, err, err.Error())
		return
	}
	method := req.Method
	if method == "" {
		method = "btree"
	}
	response.Success(c, http.StatusCreated, gin.H{
		"name":    req.Name,
		"table":   table,
		"columns": req.Columns,
		"unique":  req.Unique,
		"method":  method,
	}, "Index created successfully")
}

// DropIndex DELETE /postgres/tables/:table/indexes/:index
func (h *TableHandler) DropIndex(c *gin.Context) {
	table := c.Param("table")
	indexName := c.Param("index")
	if table == "" || indexName == "" {
		pgFail(c, http.StatusBadRequest, nil, "Table and index name are required")
		return
	}
	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}
	schema, ok := schemaQueryOrDefault(c)
	if !ok {
		return
	}
	if err := h.tableService.DropTableIndex(c.Request.Context(), projectUUID, userUUID, schema, table, indexName); err != nil {
		switch {
		case errors.Is(err, repository.ErrIndexNotFound):
			pgFail(c, http.StatusNotFound, err, "Index does not exist on this table")
			return
		case errors.Is(err, repository.ErrCannotDropPrimaryIndex):
			pgFail(c, http.StatusBadRequest, err, "Cannot drop primary key index")
			return
		}
		if failTableInstanceError(c, err) {
			return
		}
		pgFail(c, http.StatusBadRequest, err, err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}
