package database

import (
	"context"
	"fmt"
)

// DatabaseDriver defines a unified interface for per-project database
// operations (tables/collections, rows/documents, fields, and queries).
type DatabaseDriver interface {
	CreateContainer(ctx context.Context, projectID string, name string) error
	DeleteContainer(ctx context.Context, projectID string, name string) error
	ListContainers(ctx context.Context, projectID string) ([]string, error)

	InsertRecord(ctx context.Context, projectID string, container string, data map[string]interface{}) error
	GetRecords(ctx context.Context, projectID string, container string, filter map[string]interface{}) ([]map[string]interface{}, error)
	UpdateRecords(ctx context.Context, projectID string, container string, filter map[string]interface{}, update map[string]interface{}) error
	DeleteRecords(ctx context.Context, projectID string, container string, filter map[string]interface{}) error

	AddField(ctx context.Context, projectID string, container string, field string, fieldType string) error
	RemoveField(ctx context.Context, projectID string, container string, field string) error

	ExecuteQuery(ctx context.Context, projectID string, query interface{}) (interface{}, error)
}

// DriverRegistry resolves a DatabaseDriver implementation for a given dbType
// (e.g. "postgresql", "mongodb").
type DriverRegistry interface {
	GetDriver(dbType string) (DatabaseDriver, error)
}

// InMemoryDriverRegistry is a simple map-backed registry.
type InMemoryDriverRegistry struct {
	drivers map[string]DatabaseDriver
}

func NewInMemoryDriverRegistry(drivers map[string]DatabaseDriver) *InMemoryDriverRegistry {
	return &InMemoryDriverRegistry{drivers: drivers}
}

func (r *InMemoryDriverRegistry) GetDriver(dbType string) (DatabaseDriver, error) {
	if d, ok := r.drivers[dbType]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("no driver registered for db_type %q", dbType)
}

