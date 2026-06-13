// Package servicetest provides test doubles for service-layer dependencies.
// It lives under service/ so it can reference service types without an import cycle
// (service tests must not import internal/mocks when mocks also import service).
package servicetest

import (
	"context"
	"fmt"

	"backend/internal/service"

	"github.com/google/uuid"
)

// Provisioner is a test double for service.InstanceProvisioner.
type Provisioner struct {
	CreateFn  func(ctx context.Context, projectID uuid.UUID, dbType, tier, password string) (*service.ProvisionResult, error)
	GetConnFn func(ctx context.Context, projectID uuid.UUID, dbType string) (*service.ProvisionResult, error)
	DeleteFn  func(ctx context.Context, projectID uuid.UUID, dbType string) error
	External  bool
}

func (m *Provisioner) CreateInstance(ctx context.Context, projectID uuid.UUID, dbType, tier, password string) (*service.ProvisionResult, error) {
	if m.CreateFn != nil {
		return m.CreateFn(ctx, projectID, dbType, tier, password)
	}
	return &service.ProvisionResult{DSN: "postgresql://u:p@host:5432/app", ResourceRef: "ns/name"}, nil
}

func (m *Provisioner) GetConnection(ctx context.Context, projectID uuid.UUID, dbType string) (*service.ProvisionResult, error) {
	if m.GetConnFn != nil {
		return m.GetConnFn(ctx, projectID, dbType)
	}
	return &service.ProvisionResult{DSN: "postgresql://u:p@host:5432/app"}, nil
}

func (m *Provisioner) DeleteInstance(ctx context.Context, projectID uuid.UUID, dbType string) error {
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, projectID, dbType)
	}
	return nil
}

func (m *Provisioner) ClusterNameForProject(projectID uuid.UUID) string {
	return fmt.Sprintf("db-%s", projectID.String())
}

func (m *Provisioner) HasExternalAccess() bool { return m.External }

func (m *Provisioner) ExternalHostname(projectID uuid.UUID, dbType string) string {
	return "db.example.com"
}

func (m *Provisioner) ExternalPort(dbType string) int {
	if dbType == "mongodb" {
		return 27017
	}
	return 5432
}

func (m *Provisioner) TierResources(tier string) (float64, float64, int, error) {
	switch tier {
	case "free", "basic", "premium":
		return 1, 512, 5, nil
	default:
		return 0, 0, 0, fmt.Errorf("unknown tier: %s", tier)
	}
}

func (m *Provisioner) PostgRESTURL(projectID uuid.UUID) string {
	return "http://postgrest.example.com"
}

func (m *Provisioner) GetPostgRESTCredentials(ctx context.Context, projectID uuid.UUID) (string, string, error) {
	return "secret", "apikey", nil
}
