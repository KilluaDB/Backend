package handler

import (
	"backend/internal/postgres/service"
	"backend/internal/responses"
	"backend/internal/services"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type TableHandler struct {
	tableService *service.TableService
}

func NewTableHandler(tableService *service.TableService) *TableHandler {
	return &TableHandler{
		tableService: tableService,
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

	var req service.CreateTableRequest
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

	var req service.DeleteTableRequest
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
