package service

import (
	"context"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type mockInstanceConn struct {
	db  *mongo.Database
	err error
}

func (m *mockInstanceConn) GetDatabase(ctx context.Context, userID, projectID uuid.UUID) (*mongo.Database, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.db, nil
}

func (m *mockInstanceConn) GetClient(ctx context.Context, userID, projectID uuid.UUID) (*mongo.Client, error) {
	if m.db == nil {
		return nil, m.err
	}
	return m.db.Client(), m.err
}
