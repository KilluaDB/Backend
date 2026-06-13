package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// mockInstanceProvisioner is a test double for InstanceProvisioner (service package tests only).
type mockInstanceProvisioner struct {
	createFn  func(ctx context.Context, projectID uuid.UUID, dbType, tier, password string) (*ProvisionResult, error)
	getConnFn func(ctx context.Context, projectID uuid.UUID, dbType string) (*ProvisionResult, error)
	deleteFn  func(ctx context.Context, projectID uuid.UUID, dbType string) error
	external  bool
}

func (m *mockInstanceProvisioner) CreateInstance(ctx context.Context, projectID uuid.UUID, dbType, tier, password string) (*ProvisionResult, error) {
	if m.createFn != nil {
		return m.createFn(ctx, projectID, dbType, tier, password)
	}
	return &ProvisionResult{DSN: "postgresql://u:p@host:5432/app", ResourceRef: "ns/name"}, nil
}

func (m *mockInstanceProvisioner) GetConnection(ctx context.Context, projectID uuid.UUID, dbType string) (*ProvisionResult, error) {
	if m.getConnFn != nil {
		return m.getConnFn(ctx, projectID, dbType)
	}
	return &ProvisionResult{DSN: "postgresql://u:p@host:5432/app"}, nil
}

func (m *mockInstanceProvisioner) DeleteInstance(ctx context.Context, projectID uuid.UUID, dbType string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, projectID, dbType)
	}
	return nil
}

func (m *mockInstanceProvisioner) ClusterNameForProject(projectID uuid.UUID) string {
	return fmt.Sprintf("db-%s", projectID.String())
}

func (m *mockInstanceProvisioner) HasExternalAccess() bool { return m.external }

func (m *mockInstanceProvisioner) ExternalHostname(projectID uuid.UUID, dbType string) string {
	return "db.example.com"
}

func (m *mockInstanceProvisioner) ExternalPort(dbType string) int {
	if dbType == "mongodb" {
		return 27017
	}
	return 5432
}

func (m *mockInstanceProvisioner) TierResources(tier string) (float64, float64, int, error) {
	switch tier {
	case "free", "basic", "premium":
		return 1, 512, 5, nil
	default:
		return 0, 0, 0, fmt.Errorf("unknown tier: %s", tier)
	}
}
