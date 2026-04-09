package repository

//
//import (
//	"context"
//	"fmt"
//	"time"
//
//	"go.mongodb.org/mongo-driver/mongo"
//	"go.mongodb.org/mongo-driver/mongo/options"
//)
//
//// ConnectToMongoProject creates a MongoDB client for a project's database instance.
//// Uses SCRAM-SHA-256 explicitly so the client does not try SCRAM-SHA-1 (which many
//// MongoDB servers have disabled). Caller is responsible for calling client.Disconnect(ctx) when done.
//func ConnectToMongoProject(endpoint string, port int, username, password, database string) (*mongo.Client, error) {
//	// URI without credentials; auth is set via Credential to force SCRAM-SHA-256.
//	uri := fmt.Sprintf("mongodb://%s:%d/%s", endpoint, port, database)
//
//	credential := options.Credential{
//		AuthMechanism: "SCRAM-SHA-256",
//		AuthSource:    "admin",
//		Username:      username,
//		Password:      password,
//	}
//
//	clientOpts := options.Client().ApplyURI(uri).SetAuth(credential)
//
//	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
//	defer cancel()
//
//	client, err := mongo.Connect(ctx, clientOpts)
//	if err != nil {
//		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
//	}
//
//	// Verify the connection
//	if err := client.Ping(ctx, nil); err != nil {
//		_ = client.Disconnect(ctx)
//		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
//	}
//
//	return client, nil
//}
