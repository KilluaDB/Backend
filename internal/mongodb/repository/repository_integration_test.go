//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func setupTestMongoDB(t *testing.T) (*mongo.Database, func()) {
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

func TestDocumentRepository_Integration(t *testing.T) {
	db, cleanup := setupTestMongoDB(t)
	defer cleanup()

	repo := NewDocumentRepository()
	ctx := context.Background()
	collName := "test_docs"

	// InsertDocuments
	docs := []interface{}{
		bson.D{{Key: "name", Value: "doc1"}, {Key: "value", Value: 10}},
		bson.D{{Key: "name", Value: "doc2"}, {Key: "value", Value: 20}},
	}
	insertRes, err := repo.InsertDocuments(ctx, db, collName, docs)
	assert.NoError(t, err)
	assert.Len(t, insertRes.InsertedIDs, 2)

	// CountDocuments
	count, err := repo.CountDocuments(ctx, db, collName, bson.D{})
	assert.NoError(t, err)
	assert.Equal(t, int64(2), count)

	// FindDocumentByID
	id := insertRes.InsertedIDs[0]
	doc, err := repo.FindDocumentByID(ctx, db, collName, id)
	assert.NoError(t, err)
	assert.Equal(t, "doc1", doc["name"])

	// FindDocuments
	findDocs, err := repo.FindDocuments(ctx, db, collName, bson.D{{Key: "name", Value: "doc2"}}, bson.D{}, 0, 10)
	assert.NoError(t, err)
	assert.Len(t, findDocs, 1)
	assert.Equal(t, "doc2", findDocs[0]["name"])

	// UpdateDocuments
	updateFilter := bson.D{{Key: "name", Value: "doc2"}}
	updateDoc := bson.D{{Key: "$set", Value: bson.D{{Key: "value", Value: 25}}}}
	updateRes, err := repo.UpdateDocuments(ctx, db, collName, updateFilter, updateDoc, false, true)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), updateRes.ModifiedCount)

	// DeleteDocuments
	deleteRes, err := repo.DeleteDocuments(ctx, db, collName, updateFilter, true)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), deleteRes.DeletedCount)
}

func TestDocumentRepository_Metrics_Integration(t *testing.T) {
	db, cleanup := setupTestMongoDB(t)
	defer cleanup()

	repo := NewDocumentRepository()
	ctx := context.Background()

	// IncrementCounter (insert)
	err := repo.IncrementCounter(ctx, db, "insert", 5)
	assert.NoError(t, err)
	err = repo.IncrementCounter(ctx, db, "insert", 3)
	assert.NoError(t, err)

	// IncrementCounter (update)
	err = repo.IncrementCounter(ctx, db, "update", 2)
	assert.NoError(t, err)

	// Test zero or negative
	err = repo.IncrementCounter(ctx, db, "delete", 0)
	assert.NoError(t, err)

	// GetLast30DaysStats
	stats, err := repo.GetLast30DaysStats(ctx, db)
	assert.NoError(t, err)
	assert.Equal(t, int64(8), stats.Inserts)
	assert.Equal(t, int64(2), stats.Updates)
	assert.Equal(t, int64(0), stats.Deletes)
}

func TestCollectionRepository_Integration(t *testing.T) {
	db, cleanup := setupTestMongoDB(t)
	defer cleanup()

	repo := NewCollectionRepository()
	ctx := context.Background()

	// CreateCollection
	err := repo.CreateCollection(ctx, db, "test_coll")
	assert.NoError(t, err)

	// ListCollections
	collections, err := repo.ListCollections(ctx, db)
	assert.NoError(t, err)
	assert.Contains(t, collections, "test_coll")
	assert.NotContains(t, collections, "system.indexes")

	// AddFieldToDocuments
	// Insert dummy doc first
	_, err = db.Collection("test_coll").InsertOne(ctx, bson.D{{Key: "a", Value: 1}})
	assert.NoError(t, err)

	updateRes, err := repo.AddFieldToDocuments(ctx, db, "test_coll", "new_field", "default", false)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), updateRes.ModifiedCount)

	// RemoveFieldFromDocuments
	updateRes, err = repo.RemoveFieldFromDocuments(ctx, db, "test_coll", "new_field")
	assert.NoError(t, err)
	assert.Equal(t, int64(1), updateRes.ModifiedCount)

	// DropCollection
	err = repo.DropCollection(ctx, db, "test_coll")
	assert.NoError(t, err)
	
	collections, err = repo.ListCollections(ctx, db)
	assert.NoError(t, err)
	assert.NotContains(t, collections, "test_coll")
}
