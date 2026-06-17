package service

import (
	"context"

	"backend/internal/mongodb/infra"
	"backend/internal/mongodb/model"
	"backend/internal/mongodb/repository"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type MongoDashboardMetricsService struct {
	conn           infra.InstanceConnectionService
	collectionRepo *repository.CollectionRepository
	documentRepo   *repository.DocumentRepository
}

func NewMongoDashboardMetricsService(conn infra.InstanceConnectionService, collectionRepo *repository.CollectionRepository, documentRepo *repository.DocumentRepository) *MongoDashboardMetricsService {
	return &MongoDashboardMetricsService{
		conn:           conn,
		collectionRepo: collectionRepo,
		documentRepo:   documentRepo,
	}
}

func (s *MongoDashboardMetricsService) GetMetrics(ctx context.Context, userID, projectID uuid.UUID) (*model.MongoDashboardMetrics, error) {
	db, err := s.conn.GetDatabase(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}

	// db.stats() — size, collections, documents
	var dbStats bson.M
	if err := db.RunCommand(ctx, bson.D{{Key: "dbStats", Value: 1}, {Key: "scale", Value: 1}}).Decode(&dbStats); err != nil {
		return nil, err
	}

	collections, err := s.collectionRepo.ListCollections(ctx, db)
	if err != nil {
		return nil, err
	}

	var totalDocs int64
	for _, col := range collections {
		count, err := s.documentRepo.EstimatedDocumentCount(ctx, db, col)
		if err != nil {
			continue
		}
		totalDocs += count
	}

	operationStats, err := s.documentRepo.GetLast30DaysStats(ctx, db)
	if err != nil {
		return nil, err
	}

	metrics := &model.MongoDashboardMetrics{}
	metrics.Database = db.Name()
	metrics.DBSizeBytes = toInt64(dbStats["dataSize"])
	metrics.TotalDocuments = totalDocs
	metrics.Collections = int64(len(collections))
	metrics.Last30Days = *operationStats

	return &model.MongoDashboardMetrics{
		Database:       db.Name(),
		DBSizeBytes:    toInt64(dbStats["dataSize"]),
		TotalDocuments: totalDocs,
		Collections:    int64(len(collections)),
		Last30Days:     *operationStats,
	}, nil
}

// helpers to safely cast bson numeric types
func toInt64(v interface{}) int64 {
	switch val := v.(type) {
	case int32:
		return int64(val)
	case int64:
		return val
	case float64:
		return int64(val)
	case int:
		return int64(val)
	}
	return 0
}

func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int32:
		return float64(val)
	case int64:
		return float64(val)
	case int:
		return float64(val)
	}
	return 0
}

func bsonGet(d bson.D, key string) interface{} {
	for _, e := range d {
		if e.Key == key {
			return e.Value
		}
	}
	return nil
}
