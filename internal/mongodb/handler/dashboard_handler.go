package handler

import (
	"context"
	"net/http"

	"backend/internal/mongodb/model"
	"backend/internal/response"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type dashboardMetricsServiceI interface {
	GetMetrics(ctx context.Context, userID, projectID uuid.UUID) (*model.MongoDashboardMetrics, error)
}

type MongoDashboardHandler struct {
	metrics dashboardMetricsServiceI
}

func NewMongoDashboardHandler(metrics dashboardMetricsServiceI) *MongoDashboardHandler {
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
