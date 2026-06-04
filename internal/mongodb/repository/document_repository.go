package repository

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type DocumentRepository struct{}

func NewDocumentRepository() *DocumentRepository {
	return &DocumentRepository{}
}

func (r *DocumentRepository) InsertDocuments(ctx context.Context, db *mongo.Database, collection string, docs []interface{}) (*mongo.InsertManyResult, error) {
	return db.Collection(collection).InsertMany(ctx, docs)
}

func (r *DocumentRepository) FindDocumentByID(ctx context.Context, db *mongo.Database, collection string, id bson.ObjectID) (map[string]interface{}, error) {
	filter := bson.M{"_id": id}

	result := db.Collection(collection).FindOne(ctx, filter)
	if result.Err() != nil {
		return nil, result.Err()
	} 

	var doc map[string]interface{}
	if err := result.Decode(&doc); err != nil {
		return nil, err
	}

	return doc, nil
}

func (r *DocumentRepository) FindDocuments(ctx context.Context, db *mongo.Database, collection string, filter bson.D, sort bson.D, skip, limit int64) ([]map[string]interface{}, error) {
	opts := options.Find().SetSkip(skip)

	if limit > 0 {
		opts.SetLimit(limit)
	}

	if len(sort) > 0 {
		opts.SetSort(sort)
	}

	cursor, err := db.Collection(collection).Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}

	defer cursor.Close(ctx)

	var docs []map[string]interface{}
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (r *DocumentRepository) CountDocuments(ctx context.Context, db *mongo.Database, collection string, filter bson.D) (int64, error) {
	return db.Collection(collection).CountDocuments(ctx, filter)
}

func (r *DocumentRepository) UpdateDocuments(ctx context.Context, db *mongo.Database, collection string, filter, update bson.D, upsert, updateOne bool) (*mongo.UpdateResult, error) {
	if updateOne {
		opts := options.UpdateOne().SetUpsert(upsert)
		return db.Collection(collection).UpdateOne(ctx, filter, update, opts)
	}
	opts := options.UpdateMany().SetUpsert(upsert)
	return db.Collection(collection).UpdateMany(ctx, filter, update, opts)
}

func (r *DocumentRepository) DeleteDocuments(ctx context.Context, db *mongo.Database, collection string, filter bson.D, deleteOne bool) (*mongo.DeleteResult, error) {
	if deleteOne {
		return db.Collection(collection).DeleteOne(ctx, filter)
	}
	return db.Collection(collection).DeleteMany(ctx, filter)
}