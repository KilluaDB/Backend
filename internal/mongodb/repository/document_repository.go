package repository

import (
	"context"
	"strings"
	"time"

	"backend/internal/mongodb/model"

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

func (r *DocumentRepository) FindDocumentByID(ctx context.Context, db *mongo.Database, collection string, id interface{}) (map[string]interface{}, error) {
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

func (r *DocumentRepository) UpdateManyDocuments(ctx context.Context, db *mongo.Database, collection string, filter, update bson.D, upsert bool) (*mongo.UpdateResult, error) {	
	opts := options.UpdateMany().SetUpsert(upsert)
	return db.Collection(collection).UpdateMany(ctx, filter, update, opts)
}

func (r *DocumentRepository) UpdateOneDocument(ctx context.Context, db *mongo.Database, collection string, filter, update bson.D, upsert bool) (*mongo.UpdateResult, error) {
	opts := options.UpdateOne().SetUpsert(upsert)
	return db.Collection(collection).UpdateOne(ctx, filter, update, opts)
}

func (r *DocumentRepository) DeleteManyDocuments(ctx context.Context, db *mongo.Database, collection string, filter bson.D) (*mongo.DeleteResult, error) {
	return db.Collection(collection).DeleteMany(ctx, filter)
}

func (r *DocumentRepository) DeleteOneDocument(ctx context.Context, db *mongo.Database, collection string, filter bson.D) (*mongo.DeleteResult, error) {
	return db.Collection(collection).DeleteOne(ctx, filter)
}

func (r *DocumentRepository) IncrementCounter(ctx context.Context, db *mongo.Database, operation string, count int64) error {
	if count <= 0 {
		return nil
	}

	today := time.Now().UTC().Format("2006-01-02")
	lowercasedOp := strings.ToLower(strings.TrimSpace(operation))

	filter := bson.D{{Key: "_id", Value: today}}
	update := bson.D{{Key: "$inc", Value: bson.D{{Key: lowercasedOp, Value: count}}}}
	opts := options.UpdateOne().SetUpsert(true)

	_, err := db.Collection("system_metrics").UpdateOne(ctx, filter, update, opts)

	return err
}

func (r *DocumentRepository) GetLast30DaysStats(ctx context.Context, db *mongo.Database) (*model.OperationStats, error) {
	since := time.Now().UTC().AddDate(0, 0, -30).Format("2006-01-02")

	filter := bson.D{{Key: "_id", Value: bson.D{{Key: "$gte", Value: since}}}}
	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: 1}})

	cursor, err := db.Collection("system_metrics").Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	stats := &model.OperationStats{}
	for cursor.Next(ctx) {
		var doc bson.D
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		stats.Inserts += toInt64(bsonGet(doc, "insert"))
		stats.Updates += toInt64(bsonGet(doc, "update"))
		stats.Deletes += toInt64(bsonGet(doc, "delete"))
	}

	return stats, nil
}

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

func bsonGet(d bson.D, key string) interface{} {
	for _, e := range d {
		if e.Key == key {
			return e.Value
		}
	}
	return nil
}
