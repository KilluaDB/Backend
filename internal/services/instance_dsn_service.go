package services

import (
	"backend/internal/repositories"
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/google/uuid"
)

var (
	ErrProjectNotAccessible        = errors.New("project not found or not accessible")
	ErrNoRunningInstance           = errors.New("no running database instance for this project")
	ErrExternalAccessNotConfigured = errors.New("external database access is not configured on this server")
)

// ExternalConnectionInfo holds everything a user needs to connect directly to their DB from outside.
type ExternalConnectionInfo struct {
	ConnectionString string
	Host             string
	Port             int
	Database         string
	Username         string
	Password         string
}

// InstanceDsnService resolves credentials from K8s.
type InstanceDsnService struct {
	projectRepo repositories.ProjectRepository
	provisioner *OperatorProvisioner
}

func NewInstanceDsnService(
	projectRepo repositories.ProjectRepository,
	provisioner *OperatorProvisioner,
) *InstanceDsnService {
	return &InstanceDsnService{
		projectRepo: projectRepo,
		provisioner: provisioner,
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

	return result.DSN, project.ID, nil
}

// GetExternalConnectionInfo returns the external connection string for a user's database.
// It validates ownership, fetches credentials from K8s, and builds the external DSN using
// the Traefik TCP SNI hostname derived from the project ID.
func (s *InstanceDsnService) GetExternalConnectionInfo(ctx context.Context, userID, projectID uuid.UUID) (*ExternalConnectionInfo, error) {
	if !s.provisioner.HasExternalAccess() {
		return nil, ErrExternalAccessNotConfigured
	}

	project, err := s.projectRepo.GetByIDAndUserID(ctx, projectID, userID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, ErrProjectNotAccessible
	}
	if project.Status != "running" {
		return nil, ErrNoRunningInstance
	}

	// Reuse existing credential fetch from K8s (internal DSN has user/pass/db encoded).
	result, err := s.provisioner.GetConnection(ctx, projectID, project.DBType)
	if err != nil {
		return nil, fmt.Errorf("get connection from K8s: %w", err)
	}

	parsed, err := url.Parse(result.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse internal DSN: %w", err)
	}

	username := parsed.User.Username()
	password, _ := parsed.User.Password()
	database := ""
	if len(parsed.Path) > 1 {
		database = parsed.Path[1:] // strip leading "/"
	}

	host := s.provisioner.ExternalHostname(projectID, project.DBType)
	port := s.provisioner.ExternalPort(project.DBType)

	var connStr string
	switch project.DBType {
	case "mongodb":
		userInfo := url.UserPassword(username, password)
		connStr = fmt.Sprintf("mongodb://%s@%s:%d/%s?authSource=admin", userInfo.String(), host, port, database)
	default:
		userInfo := url.UserPassword(username, password)
		connStr = fmt.Sprintf("postgresql://%s@%s:%d/%s?sslmode=require", userInfo.String(), host, port, database)
	}

	return &ExternalConnectionInfo{
		ConnectionString: connStr,
		Host:             host,
		Port:             port,
		Database:         database,
		Username:         username,
		Password:         password,
	}, nil
}
