package handler

import (
	"context"
	"net/http"
	"testing"

	"backend/internal/mongodb/model"
	"backend/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

type mockDashboardMetricsService struct {
	result *model.MongoDashboardMetrics
	err    error
}

func (m *mockDashboardMetricsService) GetMetrics(ctx context.Context, userID, projectID uuid.UUID) (*model.MongoDashboardMetrics, error) {
	return m.result, m.err
}

func TestMongoDashboardHandler_GetMetrics(t *testing.T) {
	tests := []struct {
		name       string
		svc        *mockDashboardMetricsService
		wantStatus int
	}{
		{
			name:       "success",
			svc:        &mockDashboardMetricsService{result: &model.MongoDashboardMetrics{}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "service error - not accessible",
			svc:        &mockDashboardMetricsService{err: service.ErrProjectNotAccessible},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "service error - internal",
			svc:        &mockDashboardMetricsService{err: assert.AnError},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := uuid.New()
			projectID := uuid.New().String()
			h := NewMongoDashboardHandler(tt.svc)

			c, w := mongoCollectionContext(http.MethodGet, "/mongodb/dashboard", userID, projectID, nil,
				gin.Params{{Key: "id", Value: projectID}})

			h.GetMetrics(c)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}
