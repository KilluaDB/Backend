package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"

	"backend/internal/metrics"
	"backend/internal/mongodb/infra"
	"backend/internal/mongodb/model"
	"backend/internal/mongodb/repository"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

var allowedUpdateOperators = map[string]bool{
	"$set": true, "$unset": true, "$inc": true, "$push": true,
	"$pull": true, "$addToSet": true, "$rename": true, "$mul": true,
	"$min": true, "$max": true, "$currentDate": true,
}

var (
	ErrDocumentNotFound   = errors.New("document not found")
	ErrInvalidFilter      = errors.New("invalid filter")
	ErrInvalidUpdate      = errors.New("invalid update")
	ErrInvalidDocumentID  = errors.New("invalid document id")
	ErrInvalidFieldType   = errors.New("invalid field type")
	ErrTypeMismatch       = errors.New("value type does not match existing field type")
	ErrFieldAlreadyExists = errors.New("field already exists")
)

const defaultPageLimit int64 = 20
const maxPageLimit int64 = 100

type DocumentService struct {
	conn infra.InstanceConnectionService
	repo *repository.DocumentRepository
}

func NewDocumentService(conn infra.InstanceConnectionService, repo *repository.DocumentRepository) *DocumentService {
	return &DocumentService{
		conn: conn,
		repo: repo,
	}
}

func (s *DocumentService) InsertDocuments(ctx context.Context, userID, projectID uuid.UUID, collection string, req model.InsertDocumentsRequest) (*model.InsertDocumentResult, error) {
	start := time.Now()
	if err := validateCollectionName(collection); err != nil {
		return nil, err
	}

	db, err := s.conn.GetDatabase(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}

	docs := make([]interface{}, len(req.Documents))
	for i, d := range req.Documents {
		docs[i] = d
	}

	result, err := s.repo.InsertDocuments(ctx, db, collection, docs)
	metrics.MongoQueryDuration.WithLabelValues("insert").Observe(time.Since(start).Seconds())
	if err != nil {
		metrics.DbErrorsTotal.WithLabelValues("mongo", "insert").Inc()
		return nil, err
	}

	count := int64(len(result.InsertedIDs))

	if count > 0 {
		s.asyncIncrementCounter(db, "insert", count)
	}

	return &model.InsertDocumentResult{
		InsertedCount: int64(len(result.InsertedIDs)),
		InsertedIDs:   result.InsertedIDs,
	}, nil
}

func (s *DocumentService) GetDocumentByID(ctx context.Context, userID, projectID uuid.UUID, collection string, id string) (map[string]interface{}, error) {
	start := time.Now()
	if err := validateCollectionName(collection); err != nil {
		return nil, err
	}

	objectID := parseDocumentID(id)

	db, err := s.conn.GetDatabase(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}

	doc, err := s.repo.FindDocumentByID(ctx, db, collection, objectID)
	metrics.MongoQueryDuration.WithLabelValues("find").Observe(time.Since(start).Seconds())
	if err != nil {
		log.Printf("DEBUG find by string id err: %v", err)
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrDocumentNotFound
		}
		metrics.DbErrorsTotal.WithLabelValues("mongo", "find").Inc()
		return nil, err
	}

	return doc, nil
}

func (s *DocumentService) GetDocuments(ctx context.Context, userID, projectID uuid.UUID, collection string, req model.GetDocumentsRequest) (*model.GetDocumentsResult, error) {
	start := time.Now()
	if err := validateCollectionName(collection); err != nil {
		return nil, err
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultPageLimit
	} else if limit > maxPageLimit {
		limit = maxPageLimit
	}

	page := req.Page
	if page <= 0 {
		page = 1
	}
	skip := (page - 1) * limit

	filter := bson.D{}
	sort := bson.D{
		{Key: "_id", Value: -1},
	}

	db, err := s.conn.GetDatabase(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}

	total, err := s.repo.CountDocuments(ctx, db, collection, filter)
	if err != nil {
		return nil, err
	}

	docs, err := s.repo.FindDocuments(ctx, db, collection, filter, sort, skip, limit)
	metrics.MongoQueryDuration.WithLabelValues("find").Observe(time.Since(start).Seconds())
	if err != nil {
		metrics.DbErrorsTotal.WithLabelValues("mongo", "find").Inc()
		return nil, err
	}

	if docs == nil {
		docs = []map[string]interface{}{}
	}

	return &model.GetDocumentsResult{
		Documents: docs,
		Total:     total,
		Page:      page,
		Limit:     limit,
	}, nil
}

func (s *DocumentService) QueryDocuments(ctx context.Context, userID, projectID uuid.UUID, collection string, req model.QueryDocumentsRequest) (*model.QueryDocumentsResult, error) {
	start := time.Now()
	if err := validateCollectionName(collection); err != nil {
		return nil, err
	}

	filter, err := mapToBsonD(req.Filter)
	if err != nil {
		return nil, ErrInvalidFilter
	}

	sort, err := mapToBsonD(req.Sort)
	if err != nil {
		return nil, ErrInvalidFilter
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultPageLimit
	} else if limit > maxPageLimit {
		limit = maxPageLimit
	}

	page := req.Page
	if page <= 0 {
		page = 1
	}
	skip := (page - 1) * limit

	db, err := s.conn.GetDatabase(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}

	total, err := s.repo.CountDocuments(ctx, db, collection, filter)
	if err != nil {
		return nil, err
	}

	docs, err := s.repo.FindDocuments(ctx, db, collection, filter, sort, skip, limit)
	metrics.MongoQueryDuration.WithLabelValues("find").Observe(time.Since(start).Seconds())
	if err != nil {
		metrics.DbErrorsTotal.WithLabelValues("mongo", "find").Inc()
		return nil, err
	}

	if docs == nil {
		docs = []map[string]interface{}{}
	}

	return &model.QueryDocumentsResult{
		Documents: docs,
		Total:     total,
		Page:      page,
		Limit:     limit,
	}, nil
}

func (s *DocumentService) CountDocuments(ctx context.Context, userID, projectID uuid.UUID, collection string, req model.CountDocumentsRequest) (*model.CountDocumentsResult, error) {
	start := time.Now()
	if err := validateCollectionName(collection); err != nil {
		return nil, err
	}

	filter, err := mapToBsonD(req.Filter)
	if err != nil {
		return nil, ErrInvalidFilter
	}

	db, err := s.conn.GetDatabase(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}

	count, err := s.repo.CountDocuments(ctx, db, collection, filter)
	metrics.MongoQueryDuration.WithLabelValues("count").Observe(time.Since(start).Seconds())
	if err != nil {
		metrics.DbErrorsTotal.WithLabelValues("mongo", "count").Inc()
		return nil, err
	}

	return &model.CountDocumentsResult{Count: count}, nil
}

// Bulk
func (s *DocumentService) UpdateDocuments(ctx context.Context, userID, projectID uuid.UUID, collection string, req model.UpdateDocumentsRequest) (*model.UpdateDocumentsResult, error) {
	start := time.Now()
	if err := validateCollectionName(collection); err != nil {
		return nil, err
	}

	filter, err := mapToBsonD(req.Filter)
	if err != nil {
		return nil, ErrInvalidFilter
	}

	update, err := mapToBsonD(req.Update)
	if err != nil {
		return nil, ErrInvalidUpdate
	}

	if err := validateUpdateOperators(req.Update); err != nil {
		return nil, err
	}

	upsert := req.Upsert != nil && *req.Upsert
	db, err := s.conn.GetDatabase(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}

	result, err := s.repo.UpdateManyDocuments(ctx, db, collection, filter, update, upsert)
	metrics.MongoQueryDuration.WithLabelValues("update").Observe(time.Since(start).Seconds())
	if err != nil {
		metrics.DbErrorsTotal.WithLabelValues("mongo", "update").Inc()
		return nil, err
	}

	return &model.UpdateDocumentsResult{
		Matched:  result.MatchedCount,
		Modified: result.ModifiedCount,
		Upserted: result.UpsertedID,
	}, nil
}

// Bulk
func (s *DocumentService) DeleteDocuments(ctx context.Context, userID, projectID uuid.UUID, collection string, req model.DeleteDocumentsRequest) (*model.DeleteDocumentsResult, error) {
	start := time.Now()
	if err := validateCollectionName(collection); err != nil {
		return nil, err
	}

	filter, err := mapToBsonD(req.Filter)
	if err != nil {
		return nil, ErrInvalidFilter
	}

	db, err := s.conn.GetDatabase(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}

	result, err := s.repo.DeleteManyDocuments(ctx, db, collection, filter)
	metrics.MongoQueryDuration.WithLabelValues("delete").Observe(time.Since(start).Seconds())
	if err != nil {
		metrics.DbErrorsTotal.WithLabelValues("mongo", "delete").Inc()
		return nil, err
	}

	return &model.DeleteDocumentsResult{Deleted: result.DeletedCount}, nil
}

func (s *DocumentService) UpdateDocumentField(ctx context.Context, userID, projectID uuid.UUID, collection, id, field string, req model.UpdateFieldRequest) error {
	start := time.Now()
	if err := validateCollectionName(collection); err != nil {
		return err
	}

	if err := validateFieldName(field); err != nil {
		return ErrInvalidFieldName
	}

	objectID := parseDocumentID(id)

	db, err := s.conn.GetDatabase(ctx, userID, projectID)
	if err != nil {
		return err
	}

	existing, err := s.repo.FindDocumentByID(ctx, db, collection, objectID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return ErrDocumentNotFound
		}
		return err
	}

	var finalValue interface{}

	if req.Type != "" {
		// client explicitly wants a type change
		finalValue, err = castToType(req.Value, req.Type)
		if err != nil {
			return ErrInvalidFieldType
		}
	} else {
		// no type change requested — validate new value matches existing type
		if existingValue, exists := existing[field]; exists {
			if err := validateSameType(existingValue, req.Value); err != nil {
				return err
			}
		}
		finalValue = req.Value
	}

	filter := bson.D{{Key: "_id", Value: objectID}}
	update := bson.D{{Key: "$set", Value: bson.D{{Key: field, Value: finalValue}}}}

	result, err := s.repo.UpdateOneDocument(ctx, db, collection, filter, update, false)
	metrics.MongoQueryDuration.WithLabelValues("update").Observe(time.Since(start).Seconds())
	if err != nil {
		metrics.DbErrorsTotal.WithLabelValues("mongo", "update").Inc()
		return err
	}

	if result.MatchedCount == 0 {
		return ErrDocumentNotFound
	}

	s.asyncIncrementCounter(db, "update", result.MatchedCount)

	return nil
}

func (s *DocumentService) AddDocumentField(ctx context.Context, userID, projectID uuid.UUID, collection, id string, req model.AddDocumentFieldRequest) error {
	start := time.Now()
	if err := validateCollectionName(collection); err != nil {
		return err
	}

	if err := validateFieldName(req.Field); err != nil {
		return ErrInvalidFieldName
	}

	castedValue, err := castToType(req.Value, req.Type)
	if err != nil {
		return ErrInvalidFieldType
	}

	objectID := parseDocumentID(id)

	db, err := s.conn.GetDatabase(ctx, userID, projectID)
	if err != nil {
		return err
	}

	existing, err := s.repo.FindDocumentByID(ctx, db, collection, objectID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return ErrDocumentNotFound
		}
		return err
	}

	if !strings.Contains(req.Field, ".") {
		if _, exists := existing[req.Field]; exists {
			return ErrFieldAlreadyExists
		}
	}

	filter := bson.D{{Key: "_id", Value: objectID}}
	update := bson.D{{Key: "$set", Value: bson.D{{Key: req.Field, Value: castedValue}}}}

	result, err := s.repo.UpdateOneDocument(ctx, db, collection, filter, update, false)
	metrics.MongoQueryDuration.WithLabelValues("update").Observe(time.Since(start).Seconds())
	if err != nil {
		metrics.DbErrorsTotal.WithLabelValues("mongo", "update").Inc()
		return err
	}

	if result.MatchedCount == 0 {
		return ErrDocumentNotFound
	}

	s.asyncIncrementCounter(db, "update", result.MatchedCount)

	return nil
}

func (s *DocumentService) DeleteDocumentField(ctx context.Context, userID, projectID uuid.UUID, collection, id, field string) error {
	start := time.Now()
	if err := validateCollectionName(collection); err != nil {
		return err
	}

	if err := validateFieldName(field); err != nil {
		return ErrInvalidFieldName
	}

	objectID := parseDocumentID(id)

	db, err := s.conn.GetDatabase(ctx, userID, projectID)
	if err != nil {
		return err
	}

	filter := bson.D{{Key: "_id", Value: objectID}}
	update := bson.D{{Key: "$unset", Value: bson.D{{Key: field, Value: ""}}}}

	result, err := s.repo.UpdateOneDocument(ctx, db, collection, filter, update, false)
	metrics.MongoQueryDuration.WithLabelValues("update").Observe(time.Since(start).Seconds())
	if err != nil {
		metrics.DbErrorsTotal.WithLabelValues("mongo", "update").Inc()
		return err
	}

	if result.MatchedCount == 0 {
		return ErrDocumentNotFound
	}

	s.asyncIncrementCounter(db, "update", result.MatchedCount)

	return nil
}

func (s *DocumentService) DeleteDocument(ctx context.Context, userID, projectID uuid.UUID, collection, id string) error {
	start := time.Now()
	if err := validateCollectionName(collection); err != nil {
		return err
	}

	objectID := parseDocumentID(id)

	db, err := s.conn.GetDatabase(ctx, userID, projectID)
	if err != nil {
		return err
	}

	filter := bson.D{{Key: "_id", Value: objectID}}

	result, err := s.repo.DeleteOneDocument(ctx, db, collection, filter)
	metrics.MongoQueryDuration.WithLabelValues("delete").Observe(time.Since(start).Seconds())
	if err != nil {
		metrics.DbErrorsTotal.WithLabelValues("mongo", "delete").Inc()
		return err
	}

	if result.DeletedCount == 0 {
		return ErrDocumentNotFound
	}

	s.asyncIncrementCounter(db, "delete", result.DeletedCount)

	return nil
}

func (s *DocumentService) asyncIncrementCounter(db *mongo.Database, operation string, count int64) {
	if count <= 0 {
		return
	}
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.repo.IncrementCounter(bgCtx, db, operation, count)
	}()
}

func parseDocumentID(id string) interface{} {
	if oid, err := bson.ObjectIDFromHex(id); err == nil {
		log.Printf("parsed as ObjectID: %v", oid)
		return oid
	}
	log.Printf("parsed as string: %v", id)
	// return id // fall back to string _id
	// Try ObjectID first only if you know your app inserts with ObjectID
	// For string _ids, just return as-is
	return id
}

// converts a map from the client into a bson.D preserving key order.
func mapToBsonD(m map[string]interface{}) (bson.D, error) {
	if len(m) == 0 {
		return bson.D{}, nil
	}

	data, err := bson.Marshal(m)
	if err != nil {
		return nil, err
	}

	var doc bson.D
	if err := bson.Unmarshal(data, &doc); err != nil {
		return doc, nil
	}

	return doc, nil
}

// ensures the update document only uses MongoDB update operators.
func validateUpdateOperators(update map[string]interface{}) error {
	for key := range update {
		if !strings.HasPrefix(key, "$") {
			return errors.New("update must use update operators (e.g. $set, %unset)")
		}

		if !allowedUpdateOperators[key] {
			return errors.New("unsupported update operator: " + key)
		}
	}
	return nil
}

func validateSameType(existing, incoming interface{}) error {
	if existing == nil || incoming == nil {
		return nil
	}

	existingType := reflect.TypeOf(existing).Kind()
	incomingType := reflect.TypeOf(incoming).Kind()

	// JSON numbers all come in as float64 — treat int-like floats as compatible
	if (existingType == reflect.Int32 || existingType == reflect.Int64) && incomingType == reflect.Float64 {
		return nil
	}

	if existingType != incomingType {
		return ErrTypeMismatch
	}

	return nil
}

func castToType(value interface{}, targetType string) (interface{}, error) {
	switch strings.ToLower(targetType) {
	case "string":
		return fmt.Sprintf("%v", value), nil
	case "int32":
		return int32(toInt64(value)), nil
	case "int64":
		return toInt64(value), nil
	case "double":
		return toFloat64(value), nil
	case "boolean":
		b, ok := value.(bool)
		if !ok {
			return nil, errors.New("value must be true or false")
		}
		return b, nil
	case "date":
		s, ok := value.(string)
		if !ok {
			return nil, errors.New("date must be an RFC3339 string")
		}
		return time.Parse(time.RFC3339, s)
	case "null":
		return nil, nil
	case "object":
		m, ok := value.(map[string]interface{})
		if !ok {
			return nil, errors.New("value must be a JSON object")
		}
		return m, nil
	case "array":
		a, ok := value.([]interface{})
		if !ok {
			return nil, errors.New("value must be a JSON array")
		}
		return a, nil
	case "":
		return value, nil
	default:
		return nil, errors.New("unsupported type: " + targetType)
	}
}
