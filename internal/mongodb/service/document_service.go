package service

import (
	"context"
	"errors"
	"log"
	"strings"

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
	ErrDocumentNotFound    = errors.New("document not found")
	ErrInvalidFilter       = errors.New("invalid filter")
	ErrInvalidUpdate       = errors.New("invalid update")
	ErrInvalidDocumentID   = errors.New("invalid document id")
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
	if err != nil {
		return nil, err
	}

	count := int64(len(result.InsertedIDs))
	
	if count > 0 {
		_ = s.repo.IncrementCounter(ctx, db, "insert", count)
	}

	return &model.InsertDocumentResult{
		InsertedCount: int64(len(result.InsertedIDs)),
		InsertedIDs: result.InsertedIDs,
	}, nil
}

func (s *DocumentService) GetDocumentByID(ctx context.Context, userID, projectID uuid.UUID, collection string, id string) (map[string]interface{}, error) {
	if err := validateCollectionName(collection); err != nil {
		return nil, err
	}

	objectID := parseDocumentID(id)

	db, err := s.conn.GetDatabase(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}

	doc, err := s.repo.FindDocumentByID(ctx, db, collection, objectID)
	if err != nil {
		log.Printf("DEBUG find by string id err: %v", err)
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrDocumentNotFound
		}
		return nil, err
	}
	log.Printf("DEBUG existing doc: %+v", doc)
	log.Printf("DEBUG _id type: %T value: %v", doc["_id"], doc["_id"])

	return doc, nil
}

func (s *DocumentService) GetDocuments(ctx context.Context, userID, projectID uuid.UUID, collection string, req model.GetDocumentsRequest) (*model.GetDocumentsResult, error) {
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
	if err != nil {
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
	if err != nil {
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
	if err != nil {
		return nil, err
	}

	return &model.CountDocumentsResult{Count: count}, nil
}

// Bulk
func (s *DocumentService) UpdateDocuments(ctx context.Context, userID, projectID uuid.UUID, collection string, req model.UpdateDocumentsRequest) (*model.UpdateDocumentsResult, error) {
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
	updateOne := req.UpdateOne != nil && *req.UpdateOne
	db, err := s.conn.GetDatabase(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}

	result, err := s.repo.UpdateDocuments(ctx, db, collection, filter, update, upsert, updateOne)
	if err != nil {
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
	if err := validateCollectionName(collection); err != nil {
		return nil, err
	}

	filter, err := mapToBsonD(req.Filter)
	if err != nil {
		return nil, ErrInvalidFilter
	}

	deleteOne := req.DeleteOne != nil && *req.DeleteOne
	db, err := s.conn.GetDatabase(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}

	result, err := s.repo.DeleteDocuments(ctx, db, collection, filter, deleteOne)
	if err != nil {
		return nil, err
	}

	return &model.DeleteDocumentsResult{Deleted: result.DeletedCount}, nil
}

func (s *DocumentService) UpdateDocumentField(ctx context.Context, userID, projectID uuid.UUID, collection, id, field string, value interface{}) error {
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

	log.Printf("DEBUG db: %s collection: %s", db.Name(), collection)

	filter := bson.D{{Key: "_id", Value: objectID}}
	update := bson.D{{Key: "$set", Value: bson.D{{Key: field, Value: value}}}}

	result, err := s.repo.UpdateDocuments(ctx, db, collection, filter, update, false, true)
	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrDocumentNotFound
	}

	_ = s.repo.IncrementCounter(ctx, db, "update", result.MatchedCount)

	return nil
}

func (s *DocumentService) DeleteDocumentField(ctx context.Context, userID, projectID uuid.UUID, collection, id, field string) error {
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

	result, err := s.repo.UpdateDocuments(ctx, db, collection, filter, update, false, true)
	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrDocumentNotFound
	}

	_ = s.repo.IncrementCounter(ctx, db, "update", result.MatchedCount)

	return nil
}

func (s *DocumentService) DeleteDocument(ctx context.Context, userID, projectID uuid.UUID, collection, id string) error {
	if err := validateCollectionName(collection); err != nil {
		return err
	}

	objectID := parseDocumentID(id)

	db, err := s.conn.GetDatabase(ctx, userID, projectID)
	if err != nil {
		return err
	}

	filter := bson.D{{Key: "_id", Value: objectID}}

	result, err := s.repo.DeleteDocuments(ctx, db, collection, filter, true)
	if err != nil {
		return err
	}

	if result.DeletedCount == 0 {
		return ErrDocumentNotFound
	}

	_ = s.repo.IncrementCounter(ctx, db, "delete", result.DeletedCount)

	return nil
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