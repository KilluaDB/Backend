package repository

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// CollectionRepository encapsulates low-level MongoDB collection operations.
type CollectionRepository struct{}

func NewCollectionRepository() *CollectionRepository {
	return &CollectionRepository{}
}

func (r *CollectionRepository) ListCollections(ctx context.Context, db *mongo.Database) ([]string, error) {
	return db.ListCollectionNames(ctx, bson.D{})
}

func (r *CollectionRepository) CreateCollection(ctx context.Context, db *mongo.Database, name string) error {
	return db.CreateCollection(ctx, name)
}

func (r *CollectionRepository) DropCollection(ctx context.Context, db *mongo.Database, name string) error {
	return db.Collection(name).Drop(ctx)
}

func (r *CollectionRepository) AddFieldToDocuments(ctx context.Context, db *mongo.Database, collection, field string, defaultValue interface{}, updateExisting bool) (*mongo.UpdateResult, error) {
	filter := bson.M{}
	if !updateExisting {
		filter = bson.M{field: bson.M{"$exists": false}}
	}
	update := bson.M{"$set": bson.M{field: defaultValue}}
	return db.Collection(collection).UpdateMany(ctx, filter, update, options.UpdateMany().SetUpsert(false))
}

func (r *CollectionRepository) RemoveFieldFromDocuments(ctx context.Context, db *mongo.Database, collection, field string) (*mongo.UpdateResult, error) {
	filter := bson.M{field: bson.M{"$exists": true}}
	update := bson.M{"$unset": bson.M{field: ""}}
	return db.Collection(collection).UpdateMany(ctx, filter, update, options.UpdateMany().SetUpsert(false))
}
