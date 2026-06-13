package service

import (
	"context"

	"github.com/google/uuid"
)

// InstanceProvisioner abstracts Kubernetes operator provisioning for tests.
type InstanceProvisioner interface {
	CreateInstance(ctx context.Context, projectID uuid.UUID, dbType string, tier string, password string) (*ProvisionResult, error)
	GetConnection(ctx context.Context, projectID uuid.UUID, dbType string) (*ProvisionResult, error)
	DeleteInstance(ctx context.Context, projectID uuid.UUID, dbType string) error
	ClusterNameForProject(projectID uuid.UUID) string
	HasExternalAccess() bool
	ExternalHostname(projectID uuid.UUID, dbType string) string
	ExternalPort(dbType string) int
	TierResources(tier string) (cpu float64, memoryMB float64, storageGB int, err error)
}

// TierResources exposes tier sizing for OperatorProvisioner.
func (p *OperatorProvisioner) TierResources(tier string) (float64, float64, int, error) {
	return p.tierResources(tier)
}
