package service

import (
	"context"
	"errors"
	"strings"

	"backend/internal/mongodb/infra"
	"backend/internal/mongodb/model"
	"backend/internal/mongodb/repository"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

var (
	ErrInvalidCollectionName   = errors.New("invalid collection name")
	ErrCollectionAlreadyExists = errors.New("collection already exists")
	ErrInvalidFieldName        = errors.New("invalid field name")
	ErrCollectionNotFound      = errors.New("collection not found")
)

const (
	mongoNamespaceExistsCode   int = 48
	mongoNamespaceNotFoundCode int = 26
)

// CollectionService provides MongoDB collection operations with validation.
type CollectionService struct {
	conn infra.InstanceConnectionService
	repo *repository.CollectionRepository
}

func NewCollectionService(conn infra.InstanceConnectionService, repo *repository.CollectionRepository) *CollectionService {
	return &CollectionService{
		conn: conn,
		repo: repo,
	}
}

func (s *CollectionService) ListCollections(ctx context.Context, userID, projectID uuid.UUID) ([]string, error) {
	db, err := s.conn.GetDatabase(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}

	rawCollections, err := s.repo.ListCollections(ctx, db);
	if err != nil {
		return nil, err
	}

	filteredCollections := make([]string, 0)
	for _, col := range rawCollections {
		trimmed := strings.TrimSpace(col)

		if strings.HasPrefix(trimmed, "system.") || strings.Contains(trimmed, "$") {
			continue
		}

		filteredCollections = append(filteredCollections, col)
	}
	return filteredCollections, nil
}

func (s *CollectionService) CreateCollection(ctx context.Context, userID, projectID uuid.UUID, name string) error {
	if err := validateCollectionName(name); err != nil {
		return err
	}
	db, err := s.conn.GetDatabase(ctx, userID, projectID)
	if err != nil {
		return err
	}
	if err := s.repo.CreateCollection(ctx, db, name); err != nil {
		if isMongoCommandCode(err, mongoNamespaceExistsCode) {
			return ErrCollectionAlreadyExists
		}
		return err
	}
	return nil
}

func (s *CollectionService) DeleteCollection(ctx context.Context, userID, projectID uuid.UUID, name string) error {
	if err := validateCollectionName(name); err != nil {
		return err
	}
	db, err := s.conn.GetDatabase(ctx, userID, projectID)
	if err != nil {
		return err
	}
	if err := s.repo.DropCollection(ctx, db, name); err != nil {
		if isMongoCommandCode(err, mongoNamespaceNotFoundCode) {
			return ErrCollectionNotFound
		}
		return err
	}
	return nil
}

func (s *CollectionService) AddField(ctx context.Context, userID, projectID uuid.UUID, collection string, req model.AddFieldRequest) (*model.FieldUpdateResult, error) {
	if err := validateCollectionName(collection); err != nil {
		return nil, err
	}
	if err := validateFieldName(req.Field); err != nil {
		return nil, err
	}
	db, err := s.conn.GetDatabase(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	updateExisting := true
	if req.UpdateExisting != nil {
		updateExisting = *req.UpdateExisting
	}
	result, err := s.repo.AddFieldToDocuments(ctx, db, collection, req.Field, req.Default, updateExisting)
	if err != nil {
		if isMongoCommandCode(err, mongoNamespaceNotFoundCode) {
			return nil, ErrCollectionNotFound
		}
		return nil, err
	}
	return &model.FieldUpdateResult{Matched: result.MatchedCount, Modified: result.ModifiedCount}, nil
}

func (s *CollectionService) RemoveField(ctx context.Context, userID, projectID uuid.UUID, collection, field string) (*model.FieldUpdateResult, error) {
	if err := validateCollectionName(collection); err != nil {
		return nil, err
	}
	if err := validateFieldName(field); err != nil {
		return nil, err
	}
	db, err := s.conn.GetDatabase(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	result, err := s.repo.RemoveFieldFromDocuments(ctx, db, collection, field)
	if err != nil {
		if isMongoCommandCode(err, mongoNamespaceNotFoundCode) {
			return nil, ErrCollectionNotFound
		}
		return nil, err
	}
	return &model.FieldUpdateResult{Matched: result.MatchedCount, Modified: result.ModifiedCount}, nil
}

func validateCollectionName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || strings.ContainsRune(trimmed, '\x00') {
		return ErrInvalidCollectionName
	}
	return nil
}

func validateFieldName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || strings.HasPrefix(trimmed, "$") || strings.ContainsRune(trimmed, '\x00') {
		return ErrInvalidFieldName
	}
	if strings.HasPrefix(trimmed, "system.") || strings.Contains(trimmed, "$") {
		return ErrInvalidCollectionName
   }
	return nil
}

func isMongoCommandCode(err error, codes ...int) bool {
	var cmdErr mongo.CommandError
	if errors.As(err, &cmdErr) {
		for _, code := range codes {
			if int(cmdErr.Code) == code {
				return true
			}
		}
	}
	var writeErr mongo.WriteException
	if errors.As(err, &writeErr) {
		for _, we := range writeErr.WriteErrors {
			for _, code := range codes {
				if we.Code == code {
					return true
				}
			}
		}
	}
	var bulkErr mongo.BulkWriteException
	if errors.As(err, &bulkErr) {
		for _, we := range bulkErr.WriteErrors {
			for _, code := range codes {
				if we.Code == code {
					return true
				}
			}
		}
	}
	return false
}