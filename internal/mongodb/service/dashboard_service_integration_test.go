//go:build integration

package service

import (
	"context"
	"testing"

	"backend/internal/mongodb/repository"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMongoDashboardMetricsService_GetMetrics_Integration(t *testing.T) {
	db, cleanup := setupTestMongoDBService(t)
	defer cleanup()

	colRepo := repository.NewCollectionRepository()
	docRepo := repository.NewDocumentRepository()
	mockConn := &mockInstanceConn{db: db}
	
	svc := NewMongoDashboardMetricsService(mockConn, colRepo, docRepo)

	ctx := context.Background()
	userID := uuid.New()
	projectID := uuid.New()

	metrics, err := svc.GetMetrics(ctx, userID, projectID)
	require.NoError(t, err)
	assert.NotNil(t, metrics)
	assert.Equal(t, "testdb", metrics.Database)
	assert.GreaterOrEqual(t, metrics.DBSizeBytes, int64(0))
	assert.Equal(t, int64(0), metrics.TotalDocuments)
	assert.GreaterOrEqual(t, metrics.Collections, int64(0))
}
