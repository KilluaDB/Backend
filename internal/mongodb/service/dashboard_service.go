package service

import (
	"context"
	"log"

	"backend/internal/mongodb/infra"
	"backend/internal/mongodb/model"
	"backend/internal/mongodb/repository"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type MongoDashboardMetricsService struct {
	conn 				infra.InstanceConnectionService
	collectionRepo	*repository.CollectionRepository
	documentRepo 	*repository.DocumentRepository
}

func NewMongoDashboardMetricsService(conn infra.InstanceConnectionService, collectionRepo	*repository.CollectionRepository, documentRepo *repository.DocumentRepository) *MongoDashboardMetricsService {
	return &MongoDashboardMetricsService{
		conn: conn, 
		collectionRepo: collectionRepo,
		documentRepo: documentRepo,
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

	// serverStatus — connections, cache, opcounters, deadlocks, active ops
	var serverStatus bson.D
	if err := db.RunCommand(ctx, bson.D{{Key: "serverStatus", Value: 1}}).Decode(&serverStatus); err != nil {
		return nil, err
	}

	// currentOp — active operations
	var currentOp bson.M
	if err := db.RunCommand(ctx, bson.D{ {Key: "currentOp", Value: 1}, {Key: "active", Value: true},}).Decode(&currentOp); err != nil {
		return nil, err
	}

	metrics := &model.MongoDashboardMetrics{}

	collections, err := s.collectionRepo.ListCollections(ctx, db)
	if err != nil {
		return nil, err
	}

	var totalDocs int64
	for _, col := range collections {		
		count, err := s.documentRepo.CountDocuments(ctx, db, col, bson.D{})
		if err != nil {
			continue
		}
		totalDocs += count
	}

	metrics.Database = db.Name()
	metrics.DBSizeBytes = toInt64(dbStats["dataSize"])
	metrics.TotalDocuments = totalDocs
	metrics.Collections = toInt64(len(collections))

	// connections
	if conns, ok := bsonGet(serverStatus, "connections").(bson.D); ok {
		metrics.ActiveConns = toInt64(bsonGet(conns, "current"))
		metrics.AvailableConns = toInt64(bsonGet(conns, "available"))
	}	

	// opcounters — inserts/reads/updates/deletes since startup
	if ops, ok := bsonGet(serverStatus, "opcounters").(bson.D); ok {
		metrics.TotalInserts = toInt64(bsonGet(ops, "insert"))
		metrics.TotalUpdates = toInt64(bsonGet(ops, "update"))
		metrics.TotalDeletes = toInt64(bsonGet(ops, "delete"))
	}

	// currentOp — active operations count
	if inprog, ok := currentOp["inprog"].(bson.A); ok {
		metrics.ActiveOps = int64(len(inprog))
	}

	return metrics, nil
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
	log.Printf("DEBUG toInt64 unhandled type: %T value: %v", v, v)
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