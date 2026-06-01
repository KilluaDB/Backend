package handler

import (
	"backend/internal/postgres/service"
	"backend/internal/responses"
	"backend/internal/services"
	"backend/internal/utils"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

type DashboardHandler struct {
	overview *service.DashboardOverviewService
	metrics  *service.DashboardMetricsService
}

func NewDashboardHandler(
	overview *service.DashboardOverviewService,
	metrics *service.DashboardMetricsService,
) *DashboardHandler {
	return &DashboardHandler{overview: overview, metrics: metrics}
}

func (h *DashboardHandler) GetOverview(c *gin.Context) {
	userID, projectID, ok, projectErr := utils.UserAndProjectFromGin(c)
	if !ok {
		if projectErr != nil {
			pgFail(c, http.StatusBadRequest, projectErr, "Invalid projectId format")
		} else {
			pgFail(c, http.StatusUnauthorized, nil, "Unauthorized")
		}
		return
	}

	overview, err := h.overview.GetOverview(c.Request.Context(), userID, projectID)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrProjectNotAccessible), errors.Is(err, services.ErrNoRunningInstance):
			pgFail(c, http.StatusNotFound, err, "Project not found or database instance not ready")
			return
		default:
			pgFail(c, http.StatusInternalServerError, err, "Failed to retrieve overview")
			return
		}
	}
	responses.Success(c, http.StatusOK, overview, "Overview retrieved successfully")
}

func (h *DashboardHandler) GetMetrics(c *gin.Context) {
	userID, projectID, ok, projectErr := utils.UserAndProjectFromGin(c)
	if !ok {
		if projectErr != nil {
			pgFail(c, http.StatusBadRequest, projectErr, "Invalid projectId format")
		} else {
			pgFail(c, http.StatusUnauthorized, nil, "Unauthorized")
		}
		return
	}

	metrics, err := h.metrics.GetMetrics(c.Request.Context(), userID, projectID)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrProjectNotAccessible), errors.Is(err, services.ErrNoRunningInstance):
			pgFail(c, http.StatusNotFound, err, "Project not found or database instance not ready")
			return
		default:
			pgFail(c, http.StatusInternalServerError, err, "Failed to retrieve metrics")
			return
		}
	}
	responses.Success(c, http.StatusOK, metrics, "Metrics retrieved successfully")
}
