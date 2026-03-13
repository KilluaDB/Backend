package driver

import (
	"backend/internal/database"
	"backend/internal/mongodb/repository"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.mongodb.org/mongo-driver/bson"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
)

// Driver is a MongoDB implementation of database.DatabaseDriver.
// It uses the repository layer to resolve per-project instances and
// opens a MongoDB client to the project's database for each operation.
type Driver struct {
	resolver *repository.ConnectionResolver
}

var _ database.DatabaseDriver = (*Driver)(nil)

func NewDriver(metaPool *pgxpool.Pool) *Driver {
	return &Driver{resolver: repository.NewConnectionResolver(metaPool)}
}

func (d *Driver) getProjectClient(ctx context.Context, projectID string) (*mongodriver.Client, *mongodriver.Database, error) {
	return d.resolver.GetClient(ctx, projectID)
}

func (d *Driver) CreateContainer(ctx context.Context, projectID string, name string) error {
	client, db, err := d.getProjectClient(ctx, projectID)
	if err != nil {
		return err
	}
	defer client.Disconnect(ctx)

	return db.CreateCollection(ctx, name)
}

func (d *Driver) DeleteContainer(ctx context.Context, projectID string, name string) error {
	client, db, err := d.getProjectClient(ctx, projectID)
	if err != nil {
		return err
	}
	defer client.Disconnect(ctx)

	return db.Collection(name).Drop(ctx)
}

func (d *Driver) ListContainers(ctx context.Context, projectID string) ([]string, error) {
	client, db, err := d.getProjectClient(ctx, projectID)
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(ctx)

	names, err := db.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	return names, nil
}

func (d *Driver) InsertRecord(ctx context.Context, projectID string, container string, data map[string]interface{}) error {
	client, db, err := d.getProjectClient(ctx, projectID)
	if err != nil {
		return err
	}
	defer client.Disconnect(ctx)

	_, err = db.Collection(container).InsertOne(ctx, data)
	return err
}

func (d *Driver) GetRecords(ctx context.Context, projectID string, container string, filter map[string]interface{}) ([]map[string]interface{}, error) {
	client, db, err := d.getProjectClient(ctx, projectID)
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(ctx)

	bsonFilter := bson.M{}
	for k, v := range filter {
		bsonFilter[k] = v
	}

	cur, err := db.Collection(container).Find(ctx, bsonFilter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var out []map[string]interface{}
	for cur.Next(ctx) {
		var m map[string]interface{}
		if err := cur.Decode(&m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, cur.Err()
}

func (d *Driver) UpdateRecords(ctx context.Context, projectID string, container string, filter map[string]interface{}, update map[string]interface{}) error {
	client, db, err := d.getProjectClient(ctx, projectID)
	if err != nil {
		return err
	}
	defer client.Disconnect(ctx)

	if len(update) == 0 {
		return errors.New("update cannot be empty")
	}

	bsonFilter := bson.M{}
	for k, v := range filter {
		bsonFilter[k] = v
	}
	bsonUpdate := bson.M{"$set": bson.M(update)}

	_, err = db.Collection(container).UpdateMany(ctx, bsonFilter, bsonUpdate)
	return err
}

func (d *Driver) DeleteRecords(ctx context.Context, projectID string, container string, filter map[string]interface{}) error {
	client, db, err := d.getProjectClient(ctx, projectID)
	if err != nil {
		return err
	}
	defer client.Disconnect(ctx)

	bsonFilter := bson.M{}
	for k, v := range filter {
		bsonFilter[k] = v
	}

	_, err = db.Collection(container).DeleteMany(ctx, bsonFilter)
	return err
}

// AddField in MongoDB is implemented as setting the field on all documents
// that don't already have it, with a nil value.
func (d *Driver) AddField(ctx context.Context, projectID string, container string, field string, fieldType string) error {
	client, db, err := d.getProjectClient(ctx, projectID)
	if err != nil {
		return err
	}
	defer client.Disconnect(ctx)

	update := bson.M{
		"$set": bson.M{
			field: nil,
		},
	}
	_, err = db.Collection(container).UpdateMany(ctx, bson.M{}, update)
	return err
}

// RemoveField unsets the given field from all documents.
func (d *Driver) RemoveField(ctx context.Context, projectID string, container string, field string) error {
	client, db, err := d.getProjectClient(ctx, projectID)
	if err != nil {
		return err
	}
	defer client.Disconnect(ctx)

	update := bson.M{
		"$unset": bson.M{
			field: "",
		},
	}
	_, err = db.Collection(container).UpdateMany(ctx, bson.M{}, update)
	return err
}

// ExecuteQuery executes a basic MongoDB operation described as map[string]interface{}.
func (d *Driver) ExecuteQuery(ctx context.Context, projectID string, query interface{}) (interface{}, error) {
	q, ok := query.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("Mongo driver expects query as map[string]interface{}")
	}

	collection, _ := q["collection"].(string)
	if collection == "" {
		return nil, errors.New("collection is required for Mongo query")
	}
	operation, _ := q["operation"].(string)
	operation = strings.ToLower(strings.TrimSpace(operation))
	if operation == "" {
		operation = "find"
	}

	filterMap, _ := q["filter"].(map[string]interface{})
	dataMap, _ := q["data"].(map[string]interface{})
	limitVal, _ := q["limit"].(int)

	client, db, err := d.getProjectClient(ctx, projectID)
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(ctx)

	bsonFilter := bson.M{}
	for k, v := range filterMap {
		bsonFilter[k] = v
	}

	switch operation {
	case "find":
		cur, err := db.Collection(collection).Find(ctx, bsonFilter)
		if err != nil {
			return nil, err
		}
		defer cur.Close(ctx)

		var out []map[string]interface{}
		count := 0
		for cur.Next(ctx) {
			if limitVal > 0 && count >= limitVal {
				break
			}
			var m map[string]interface{}
			if err := cur.Decode(&m); err != nil {
				return nil, err
			}
			out = append(out, m)
			count++
		}
		if err := cur.Err(); err != nil {
			return nil, err
		}
		return out, nil

	case "insertone":
		if len(dataMap) == 0 {
			return nil, errors.New("data is required for insertOne")
		}
		res, err := db.Collection(collection).InsertOne(ctx, dataMap)
		if err != nil {
			return nil, err
		}
		return map[string]any{"inserted_id": res.InsertedID}, nil

	case "updatemany":
		if len(dataMap) == 0 {
			return nil, errors.New("data is required for updateMany")
		}
		update := bson.M{}
		hasOperator := false
		for k := range dataMap {
			if strings.HasPrefix(k, "$") {
				hasOperator = true
				break
			}
		}
		if hasOperator {
			for k, v := range dataMap {
				update[k] = v
			}
		} else {
			update["$set"] = bson.M(dataMap)
		}
		res, err := db.Collection(collection).UpdateMany(ctx, bsonFilter, update)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"matched_count":  res.MatchedCount,
			"modified_count": res.ModifiedCount,
		}, nil

	case "deletemany":
		res, err := db.Collection(collection).DeleteMany(ctx, bsonFilter)
		if err != nil {
			return nil, err
		}
		return map[string]any{"deleted_count": res.DeletedCount}, nil

	default:
		return nil, fmt.Errorf("unsupported mongo operation: %s", operation)
	}
}
