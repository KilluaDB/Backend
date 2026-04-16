package handler

//
//import (
//	"backend/internal/mongodb/service"
//	"backend/internal/responses"
//	"net/http"
//	"strconv"
//
//	"github.com/gin-gonic/gin"
//	"github.com/gin-gonic/gin/binding"
//	"github.com/google/uuid"
//)
//
//type QueryHandler struct {
//	queryService *service.QueryService
//}
//
//func NewQueryHandler(queryService *service.QueryService) *QueryHandler {
//	return &QueryHandler{queryService: queryService}
//}
//
//// ExecuteQuery executes a Mongo query operation for a MongoDB project.
//func (h *QueryHandler) ExecuteQuery(c *gin.Context) {
//	projectId := c.Param("id")
//	if projectId == "" {
//		responses.Fail(c, http.StatusBadRequest, nil, "Project id is required")
//		return
//	}
//
//	userId, exists := c.Get("userId")
//	if !exists {
//		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
//		return
//	}
//
//	var userUUID uuid.UUID
//	switch v := userId.(type) {
//	case uuid.UUID:
//		userUUID = v
//	case string:
//		parsed, err := uuid.Parse(v)
//		if err != nil {
//			responses.Fail(c, http.StatusUnauthorized, err, "Invalid user ID format")
//			return
//		}
//		userUUID = parsed
//	default:
//		responses.Fail(c, http.StatusUnauthorized, nil, "Invalid user ID format")
//		return
//	}
//
//	projectUUID, err := uuid.Parse(projectId)
//	if err != nil {
//		responses.Fail(c, http.StatusBadRequest, err, "Invalid projectId format")
//		return
//	}
//
//	var req service.MongoQueryRequest
//	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
//		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body for MongoDB query")
//		return
//	}
//
//	result, exec, err := h.queryService.ExecuteMongoQuery(userUUID, &req, projectUUID)
//	if err != nil {
//		responses.Fail(c, http.StatusInternalServerError, err, "Failed to execute query")
//		return
//	}
//
//	var execMs int64 = 0
//	if exec.ExecutionTimeMs != nil {
//		execMs = int64(*exec.ExecutionTimeMs)
//	}
//
//	response := gin.H{
//		"result":            result,
//		"execution_id":      exec.ID,
//		"execution_time_ms": execMs,
//	}
//	responses.Success(c, http.StatusOK, response, "Query executed successfully")
//}
//
//// GetQueryHistory returns Mongo query history for the authenticated user.
//func (h *QueryHandler) GetQueryHistory(c *gin.Context) {
//	userId, exists := c.Get("userId")
//	if !exists {
//		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
//		return
//	}
//
//	limitStr := c.DefaultQuery("limit", "10")
//	limit, err := strconv.Atoi(limitStr)
//	if err != nil || limit < 1 {
//		limit = 10
//	}
//	if limit > 30 {
//		limit = 30
//	}
//
//	var userUUID uuid.UUID
//	switch v := userId.(type) {
//	case uuid.UUID:
//		userUUID = v
//	case string:
//		parsed, err := uuid.Parse(v)
//		if err != nil {
//			responses.Fail(c, http.StatusUnauthorized, nil, "Invalid user ID format")
//			return
//		}
//		userUUID = parsed
//	default:
//		responses.Fail(c, http.StatusUnauthorized, nil, "Invalid user ID format")
//		return
//	}
//
//	history, err := h.queryService.GetQueryHistory(userUUID, limit)
//	if err != nil {
//		responses.Fail(c, http.StatusInternalServerError, err, "Failed to get query history")
//		return
//	}
//
//	responses.Success(c, http.StatusOK, history, "Query history retrieved successfully")
//}
//
