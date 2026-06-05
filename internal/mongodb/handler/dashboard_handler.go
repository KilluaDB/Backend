package handler

import (
	mongoservice "backend/internal/mongodb/service"
	"backend/internal/response"
	"net/http"

	"github.com/gin-gonic/gin"
)

type MongoDashboardHandler struct {
	metrics *mongoservice.MongoDashboardMetricsService
}

func NewMongoDashboardHandler(metrics *mongoservice.MongoDashboardMetricsService) *MongoDashboardHandler {
	return &MongoDashboardHandler{metrics: metrics}
}

func (h *MongoDashboardHandler) GetMetrics(c *gin.Context) {
	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}

	metrics, err := h.metrics.GetMetrics(c.Request.Context(), userUUID, projectUUID)
	if err != nil {
		if failMongoInstanceError(c, err) {
			return
		}
		response.Fail(c, http.StatusInternalServerError, err, "Failed to retrieve metrics")
		return
	}
	
	response.Success(c, http.StatusOK, metrics, "Metrics retrieved successfully")
}