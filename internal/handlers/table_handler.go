package handlers

import (
	"fmt"
	_ "log"
	"my_project/internal/responses"
	"my_project/internal/services"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type TableHandler struct {
	tableService *services.TableService
}

func NewTableHandler(tableService *services.TableService) *TableHandler {
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

	var req services.CreateTableRequest
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
		responses.Fail(c, http.StatusBadRequest, err, "Error while creating the table")
		return
	}

	response := gin.H{
		"result": result,
	}

	responses.Success(c, http.StatusOK, response, "Table created successfully")
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

	var req services.DeleteTableRequest
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
		responses.Fail(c, http.StatusBadRequest, err, "Cannot delete the given table")
		return
	}

	response := gin.H {
		"result": result,
	}

	responses.Success(c, http.StatusOK, response, "Table deleted successfully")
}

// func (h *TableHandler) UpdateTable(c *gin.Context) {
// 	projectId := c.Param("id")
// 	if projectId == "" {
// 		responses.Fail(c, http.StatusBadRequest, nil, "Project id is required")
// 		return
// 	}

// 	userId, exists := c.Get("userId")
// 	if !exists {
// 		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
// 		return
// 	}

// 	var req services.UpdateTableRequest
// 	if err := c.ShouldBindJSON(&req); err != nil {
// 		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body")
// 		return
// 	}

// 	userUUID, err := h.toUUID(userId)
// 	if err != nil {
// 		responses.Fail(c, http.StatusUnauthorized, err, "Invalid user Id format")
// 		return
// 	}

// 	projectUUID, err := uuid.Parse(projectId)
// 	if err != nil {
// 		responses.Fail(c, http.StatusBadRequest, err, "Invalid projectId format")
// 		return
// 	}

// 	result, err := h.tableService.UpdateTable(&req, userUUID, projectUUID)
// 	if err != nil {
// 		responses.Fail(c, http.StatusBadRequest, err, "Cannot delete the given table")
// 		return
// 	}

// 	response := gin.H {
// 		"result": result,
// 	}

// 	responses.Success(c, http.StatusOK, response, "Table updated successfully")
// }

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
