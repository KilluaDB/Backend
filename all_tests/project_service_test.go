package service

import (
	"context"
	"testing"

	"backend/internal/mocks"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeDBType(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"postgres", "postgresql", false},
		{"postgresql", "postgresql", false},
		{"mongodb", "mongodb", false},
		{"nosql", "mongodb", false},
		{"mysql", "", true},
	}
	for _, tt := range tests {
		got, err := normalizeDBType(tt.in)
		if tt.wantErr {
			assert.Error(t, err)
		} else {
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		}
	}
}

func TestProjectService_CreateProject(t *testing.T) {
	projects := mocks.NewProjectStore()
	prov := &mockInstanceProvisioner{}
	svc := NewProjectService(projects, prov, nil, nil)
	ctx := context.Background()
	userID := uuid.New().String()

	tests := []struct {
		name    string
		userID  string
		req     CreateProjectRequest
		wantErr error
	}{
		{"invalid user id", "bad", CreateProjectRequest{Name: "p", DBType: "postgres", Password: "SecurePass123!"}, ErrInvalidUserID},
		{"invalid db type", userID, CreateProjectRequest{Name: "p", DBType: "mysql", Password: "SecurePass123!"}, ErrInvalidDBType},
		{"invalid password", userID, CreateProjectRequest{Name: "p", DBType: "postgres", Password: "short"}, ErrInvalidDBPassword},
		{"invalid tier", userID, CreateProjectRequest{Name: "p", DBType: "postgres", Password: "SecurePass123!", ResourceTier: "enterprise"}, ErrInvalidResourceTier},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.CreateProject(ctx, tt.userID, tt.req)
			require.Error(t, err)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			}
		})
	}

	p, err := svc.CreateProject(ctx, userID, CreateProjectRequest{
		Name: "my-db", DBType: "postgres", Password: "SecurePass123!",
	})
	require.NoError(t, err)
	assert.Equal(t, "postgresql", p.DBType)
	assert.Equal(t, "creating", p.Status)
}

func TestProjectService_GetProjectsByUserID(t *testing.T) {
	projects := mocks.NewProjectStore()
	prov := &mockInstanceProvisioner{}
	svc := NewProjectService(projects, prov, nil, nil)
	ctx := context.Background()
	userID := uuid.New()
	projects.SeedProject(userID, "postgresql")
	projects.SeedProject(userID, "mongodb")

	list, err := svc.GetProjectsByUserID(ctx, userID.String())
	require.NoError(t, err)
	assert.Len(t, list, 2)
	assert.NotEmpty(t, list[0].Status)
}

func TestProjectService_GetAndDelete(t *testing.T) {
	projects := mocks.NewProjectStore()
	prov := &mockInstanceProvisioner{}
	svc := NewProjectService(projects, prov, nil, nil)
	ctx := context.Background()
	userID := uuid.New()
	p := projects.SeedProject(userID, "postgresql")

	got, err := svc.GetProjectByIDAndUserID(ctx, p.ID.String(), userID.String())
	require.NoError(t, err)
	assert.Equal(t, p.ID, got.ID)

	_, err = svc.GetProjectByIDAndUserID(ctx, uuid.New().String(), userID.String())
	assert.ErrorIs(t, err, ErrProjectNotFound)

	require.NoError(t, svc.DeleteProjectByIDAndUserID(ctx, p.ID.String(), userID.String()))
	_, err = svc.GetProjectByIDAndUserID(ctx, p.ID.String(), userID.String())
	assert.ErrorIs(t, err, ErrProjectNotFound)
}

func TestProjectService_GetExternalConnectionInfo(t *testing.T) {
	projects := mocks.NewProjectStore()
	userID := uuid.New()
	p := projects.SeedProject(userID, "postgresql")
	p.Status = "running"
	_ = projects.Update(context.Background(), p)

	prov := &mockInstanceProvisioner{
		external: true,
		getConnFn: func(ctx context.Context, projectID uuid.UUID, dbType string) (*ProvisionResult, error) {
			return &ProvisionResult{DSN: "postgresql://app:secret@internal:5432/app?sslmode=require"}, nil
		},
	}
	svc := NewProjectService(projects, prov, nil, nil)

	info, err := svc.GetExternalConnectionInfo(context.Background(), p.ID.String(), userID.String())
	require.NoError(t, err)
	assert.Contains(t, info.ConnectionString, "postgresql://")
	assert.Equal(t, "app", info.Username)

	prov.external = false
	_, err = svc.GetExternalConnectionInfo(context.Background(), p.ID.String(), userID.String())
	assert.ErrorIs(t, err, ErrExternalAccessNotConfigured)
}

func TestProjectService_InvalidIDs(t *testing.T) {
	svc := NewProjectService(mocks.NewProjectStore(), &mockInstanceProvisioner{}, nil, nil)
	_, err := svc.GetProjectByID(context.Background(), "not-uuid")
	assert.Error(t, err)
	_, err = svc.GetProjectsByUserID(context.Background(), "bad")
	assert.Error(t, err)
}
