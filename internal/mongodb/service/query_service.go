package service

import (
	"backend/internal/database"
	"backend/internal/models"
	"backend/internal/mongodb/repository"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MongoQueryRequest describes a MongoDB query operation for /mongodb/query/execute endpoint.
type MongoQueryRequest struct {
	Collection string                 `json:"collection" binding:"required"`
	Operation  string                 `json:"operation" binding:"required"` // find | insertOne | updateMany | deleteMany
	Filter     map[string]interface{} `json:"filter,omitempty"`
	Data       map[string]interface{} `json:"data,omitempty"`  // for insertOne/updateMany
	Limit      *int                   `json:"limit,omitempty"` // used for find
}

type InstanceConnectionService interface {
	GetInstanceID(ctx context.Context, userID, projectID uuid.UUID) (uuid.UUID, error)
}

type QueryService struct {
	instanceConn InstanceConnectionService
	driver       database.DatabaseDriver
	execRepo     *repository.QueryHistoryRepository
}

func NewQueryService(
	instanceConn InstanceConnectionService,
	driver database.DatabaseDriver,
	execRepo *repository.QueryHistoryRepository,
) *QueryService {
	return &QueryService{
		instanceConn: instanceConn,
		driver:       driver,
		execRepo:     execRepo,
	}
}

func (s *QueryService) ExecuteMongoQuery(userID uuid.UUID, req *MongoQueryRequest, projectId uuid.UUID) (interface{}, *models.QueryHistory, error) {
	startTime := time.Now()
	ctx := context.Background()

	instanceID, _ := s.instanceConn.GetInstanceID(ctx, userID, projectId)

	operation := strings.ToLower(strings.TrimSpace(req.Operation))
	switch operation {
	case "find", "insertone", "updatemany", "deletemany":
	default:
		execTime := time.Since(startTime).Milliseconds()
		success := false
		execTimeInt := int(execTime)
		exec := &models.QueryHistory{
			DBInstanceID:    instanceID,
			UserID:          userID,
			QueryText:       mustJSON(req),
			ExecutedAt:      time.Now(),
			Success:         &success,
			ExecutionTimeMs: &execTimeInt,
		}
		if instanceID != uuid.Nil {
			_ = s.execRepo.Create(exec)
		}
		return map[string]any{"error": "unsupported mongo operation"}, exec, nil
	}

	if req.Collection == "" {
		execTime := time.Since(startTime).Milliseconds()
		success := false
		execTimeInt := int(execTime)
		exec := &models.QueryHistory{
			DBInstanceID:    instanceID,
			UserID:          userID,
			QueryText:       mustJSON(req),
			ExecutedAt:      time.Now(),
			Success:         &success,
			ExecutionTimeMs: &execTimeInt,
		}
		if instanceID != uuid.Nil {
			_ = s.execRepo.Create(exec)
		}
		return map[string]any{"error": "collection is required"}, exec, nil
	}

	if (operation == "insertone" || operation == "updatemany") && len(req.Data) == 0 {
		execTime := time.Since(startTime).Milliseconds()
		success := false
		execTimeInt := int(execTime)
		exec := &models.QueryHistory{
			DBInstanceID:    instanceID,
			UserID:          userID,
			QueryText:       mustJSON(req),
			ExecutedAt:      time.Now(),
			Success:         &success,
			ExecutionTimeMs: &execTimeInt,
		}
		if instanceID != uuid.Nil {
			_ = s.execRepo.Create(exec)
		}
		return map[string]any{"error": "data is required for this operation"}, exec, nil
	}

	payload := map[string]any{
		"collection": req.Collection,
		"operation":  operation,
		"filter":     req.Filter,
		"data":       req.Data,
	}
	if req.Limit != nil {
		payload["limit"] = *req.Limit
	}

	result, execErr := s.driver.ExecuteQuery(ctx, projectId.String(), payload)
	execTime := time.Since(startTime).Milliseconds()

	success := execErr == nil
	execTimeInt := int(execTime)
	exec := &models.QueryHistory{
		DBInstanceID:    instanceID,
		UserID:          userID,
		QueryText:       mustJSON(req),
		ExecutedAt:      time.Now(),
		Success:         &success,
		ExecutionTimeMs: &execTimeInt,
	}
	if instanceID != uuid.Nil {
		_ = s.execRepo.Create(exec)
	}

	if execErr != nil {
		return map[string]any{"error": execErr.Error()}, exec, nil
	}
	return result, exec, nil
}

func (s *QueryService) GetQueryHistory(userID uuid.UUID, limit int) ([]models.QueryHistory, error) {
	return s.execRepo.GetByUserID(userID, limit)
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
