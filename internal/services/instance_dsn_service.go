package services

import (
	"backend/internal/repositories"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

var (
	ErrProjectNotAccessible = errors.New("project not found or not accessible")
	ErrNoRunningInstance    = errors.New("no running database instance for this project")
)

// InstanceDsnService resolves credentials from K8s.
type InstanceDsnService struct {
	projectRepo  repositories.ProjectRepository
	instanceRepo *repositories.DatabaseInstanceRepository
	provisioner  *OperatorProvisioner
}

func NewInstanceDsnService(
	projectRepo repositories.ProjectRepository,
	instanceRepo *repositories.DatabaseInstanceRepository,
	provisioner *OperatorProvisioner,
) *InstanceDsnService {
	return &InstanceDsnService{
		projectRepo:  projectRepo,
		instanceRepo: instanceRepo,
		provisioner:  provisioner,
	}
}

// GetConnectionDSN validates project ownership, checks the instance is running,
// and fetches a fresh DSN from K8s.
func (s *InstanceDsnService) GetConnectionDSN(ctx context.Context, userID, projectID uuid.UUID) (dsn string, instanceID uuid.UUID, err error) {
	project, err := s.projectRepo.GetByIDAndUserID(ctx, projectID, userID)
	if err != nil {
		return "", uuid.Nil, err
	}
	if project == nil {
		return "", uuid.Nil, ErrProjectNotAccessible
	}

	inst, err := s.instanceRepo.GetByProjectID(projectID)
	if err != nil {
		return "", uuid.Nil, err
	}
	if inst == nil {
		return "", uuid.Nil, ErrNoRunningInstance
	}

	result, err := s.provisioner.GetConnection(ctx, projectID, project.DBType)
	if err != nil {
		if project.Status != "running" {
			return "", uuid.Nil, ErrNoRunningInstance
		}
		return "", uuid.Nil, fmt.Errorf("get connection from K8s: %w", err)
	}

	if project.Status != "running" {
		_ = s.projectRepo.UpdateRuntimeStatus(ctx, projectID, "running")
	}

	return result.DSN, inst.ID, nil
}
