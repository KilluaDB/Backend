//go:build integration

package service

import (
	"context"
	"testing"

	"backend/internal/mongodb/model"
	"backend/internal/mongodb/repository"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func setupTestMongoDBService(t *testing.T) (*mongo.Database, func()) {
	ctx := context.Background()

	mongodbContainer, err := mongodb.Run(ctx, "mongo:6")
	require.NoError(t, err)

	endpoint, err := mongodbContainer.ConnectionString(ctx)
	require.NoError(t, err)

	opts := options.Client().ApplyURI(endpoint)
	client, err := mongo.Connect(opts)
	require.NoError(t, err)

	db := client.Database("testdb")

	cleanup := func() {
		client.Disconnect(ctx)
		mongodbContainer.Terminate(ctx)
	}

	return db, cleanup
}

func TestDocumentService_Integration(t *testing.T) {
	db, cleanup := setupTestMongoDBService(t)
	defer cleanup()

	repo := repository.NewDocumentRepository()
	mockConn := &mockInstanceConn{db: db}
	svc := NewDocumentService(mockConn, repo)

	ctx := context.Background()
	userID := uuid.New()
	projectID := uuid.New()
	collection := "docs"

	// InsertDocuments
	t.Run("InsertDocuments", func(t *testing.T) {
		req := model.InsertDocumentsRequest{
			Documents: []map[string]interface{}{
				{"name": "test1", "val": 10},
				{"name": "test2", "val": 20},
			},
		}
		res, err := svc.InsertDocuments(ctx, userID, projectID, collection, req)
		assert.NoError(t, err)
		assert.Equal(t, int64(2), res.InsertedCount)
		assert.Len(t, res.InsertedIDs, 2)
	})

	// GetDocumentByID (Found)
	t.Run("GetDocumentByID - Found", func(t *testing.T) {
		// get id of first doc
		docs, _ := svc.GetDocuments(ctx, userID, projectID, collection, model.GetDocumentsRequest{Limit: 1})
		id := docs.Documents[0]["_id"].(bson.ObjectID).Hex()

		doc, err := svc.GetDocumentByID(ctx, userID, projectID, collection, id)
		assert.NoError(t, err)
		assert.NotNil(t, doc)
	})

	// GetDocumentByID (Not Found)
	t.Run("GetDocumentByID - Not Found", func(t *testing.T) {
		// random object id
		id := bson.NewObjectID().Hex()
		_, err := svc.GetDocumentByID(ctx, userID, projectID, collection, id)
		assert.ErrorIs(t, err, ErrDocumentNotFound)
	})

	// UpdateDocuments
	t.Run("UpdateDocuments", func(t *testing.T) {
		req := model.UpdateDocumentsRequest{
			Filter: map[string]interface{}{"name": "test1"},
			Update: map[string]interface{}{"$set": map[string]interface{}{"val": 100}},
		}
		res, err := svc.UpdateDocuments(ctx, userID, projectID, collection, req)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), res.Modified)
	})

	// DeleteDocuments
	t.Run("DeleteDocuments", func(t *testing.T) {
		req := model.DeleteDocumentsRequest{
			Filter: map[string]interface{}{"name": "test1"},
		}
		res, err := svc.DeleteDocuments(ctx, userID, projectID, collection, req)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), res.Deleted)
	})

	// QueryDocuments
	t.Run("QueryDocuments", func(t *testing.T) {
		req := model.QueryDocumentsRequest{
			Filter: map[string]interface{}{"name": "test2"},
			Limit:  10,
			Page:   1,
		}
		res, err := svc.QueryDocuments(ctx, userID, projectID, collection, req)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), res.Total)
		assert.Len(t, res.Documents, 1)
	})

	// GetDocuments
	t.Run("GetDocuments", func(t *testing.T) {
		req := model.GetDocumentsRequest{
			Limit: 10,
		}
		res, err := svc.GetDocuments(ctx, userID, projectID, collection, req)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, len(res.Documents), 1)
	})

	// CountDocuments
	t.Run("CountDocuments", func(t *testing.T) {
		req := model.CountDocumentsRequest{}
		res, err := svc.CountDocuments(ctx, userID, projectID, collection, req)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, res.Count, int64(1))
	})

	// AddDocumentField
	t.Run("AddDocumentField", func(t *testing.T) {
		docs, _ := svc.GetDocuments(ctx, userID, projectID, collection, model.GetDocumentsRequest{Limit: 1})
		id := docs.Documents[0]["_id"].(bson.ObjectID).Hex()

		req := model.AddDocumentFieldRequest{
			Field: "new_field",
			Value: "value",
			Type:  "string",
		}
		err := svc.AddDocumentField(ctx, userID, projectID, collection, id, req)
		assert.NoError(t, err)
	})

	// UpdateDocumentField
	t.Run("UpdateDocumentField", func(t *testing.T) {
		docs, _ := svc.GetDocuments(ctx, userID, projectID, collection, model.GetDocumentsRequest{Limit: 1})
		id := docs.Documents[0]["_id"].(bson.ObjectID).Hex()

		req := model.UpdateFieldRequest{
			Value: "new_value",
		}
		err := svc.UpdateDocumentField(ctx, userID, projectID, collection, id, "new_field", req)
		assert.NoError(t, err)

		// Test cast to type
		reqCast := model.UpdateFieldRequest{
			Value: 123,
			Type:  "int32",
		}
		err = svc.UpdateDocumentField(ctx, userID, projectID, collection, id, "cast_field", reqCast)
		assert.NoError(t, err)

		// Test invalid cast
		reqBadCast := model.UpdateFieldRequest{
			Value: "not a bool",
			Type:  "boolean",
		}
		err = svc.UpdateDocumentField(ctx, userID, projectID, collection, id, "bad_cast", reqBadCast)
		assert.ErrorIs(t, err, ErrInvalidFieldType)

		// Test type mismatch
		reqMismatch := model.UpdateFieldRequest{
			Value: "not an int",
		}
		err = svc.UpdateDocumentField(ctx, userID, projectID, collection, id, "cast_field", reqMismatch)
		assert.ErrorIs(t, err, ErrTypeMismatch)

		err = svc.UpdateDocumentField(ctx, userID, projectID, collection, bson.NewObjectID().Hex(), "f", req)
		assert.ErrorIs(t, err, ErrDocumentNotFound)
	})

	// AddDocumentField
	t.Run("AddDocumentField", func(t *testing.T) {
		docs, _ := svc.GetDocuments(ctx, userID, projectID, collection, model.GetDocumentsRequest{Limit: 1})
		id := docs.Documents[0]["_id"].(bson.ObjectID).Hex()

		req := model.AddDocumentFieldRequest{
			Field: "added_field",
			Value: "added_val",
		}
		err := svc.AddDocumentField(ctx, userID, projectID, collection, id, req)
		assert.NoError(t, err)

		// existing field
		err = svc.AddDocumentField(ctx, userID, projectID, collection, id, req)
		assert.ErrorIs(t, err, ErrFieldAlreadyExists)

		err = svc.AddDocumentField(ctx, userID, projectID, collection, bson.NewObjectID().Hex(), req)
		assert.ErrorIs(t, err, ErrDocumentNotFound)
	})

	// DeleteDocumentField
	t.Run("DeleteDocumentField", func(t *testing.T) {
		docs, _ := svc.GetDocuments(ctx, userID, projectID, collection, model.GetDocumentsRequest{Limit: 1})
		id := docs.Documents[0]["_id"].(bson.ObjectID).Hex()

		err := svc.DeleteDocumentField(ctx, userID, projectID, collection, id, "new_field")
		assert.NoError(t, err)

		err = svc.DeleteDocumentField(ctx, userID, projectID, collection, bson.NewObjectID().Hex(), "new_field")
		assert.ErrorIs(t, err, ErrDocumentNotFound)
	})

	// DeleteDocument
	t.Run("DeleteDocument", func(t *testing.T) {
		docs, _ := svc.GetDocuments(ctx, userID, projectID, collection, model.GetDocumentsRequest{Limit: 1})
		id := docs.Documents[0]["_id"].(bson.ObjectID).Hex()

		err := svc.DeleteDocument(ctx, userID, projectID, collection, id)
		assert.NoError(t, err)

		err = svc.DeleteDocument(ctx, userID, projectID, collection, bson.NewObjectID().Hex())
		assert.ErrorIs(t, err, ErrDocumentNotFound)
	})
}
