package service

import (
	"context"
	"errors"
	"testing"

	"backend/internal/mocks"
	"backend/internal/model"

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

type getProjectFailStore struct {
	*mocks.ProjectStore
	err error
}

func (s *getProjectFailStore) GetByID(_ context.Context, _ uuid.UUID) (*model.Project, error) {
	return nil, s.err
}
func (s *getProjectFailStore) GetByIDAndUserID(_ context.Context, _, _ uuid.UUID) (*model.Project, error) {
	return nil, s.err
}
func (s *getProjectFailStore) GetByUserID(_ context.Context, _ uuid.UUID) ([]model.Project, error) {
	return nil, s.err
}
func (s *getProjectFailStore) Create(_ context.Context, _ *model.Project) error {
	return s.err
}

func TestProjectService_GetProjectByID_errors(t *testing.T) {
	ctx := context.Background()

	t.Run("repo error", func(t *testing.T) {
		store := &getProjectFailStore{ProjectStore: mocks.NewProjectStore(), err: errors.New("db error")}
		svc := NewProjectService(store, &mockInstanceProvisioner{}, nil, nil)
		_, err := svc.GetProjectByID(ctx, uuid.New().String())
		require.ErrorContains(t, err, "db error")
	})

	t.Run("not found returns nil", func(t *testing.T) {
		normal := mocks.NewProjectStore()
		svc := NewProjectService(normal, &mockInstanceProvisioner{}, nil, nil)
		p, err := svc.GetProjectByID(ctx, uuid.New().String())
		require.NoError(t, err)
		assert.Nil(t, p)
	})
}

func TestProjectService_GetProjectByIDAndUserID_repoError(t *testing.T) {
	store := &getProjectFailStore{ProjectStore: mocks.NewProjectStore(), err: errors.New("db error")}
	svc := NewProjectService(store, &mockInstanceProvisioner{}, nil, nil)
	_, err := svc.GetProjectByIDAndUserID(context.Background(), uuid.New().String(), uuid.New().String())
	require.ErrorContains(t, err, "db error")
}

func TestProjectService_GetProjectsByUserID_errors(t *testing.T) {
	ctx := context.Background()

	t.Run("repo error", func(t *testing.T) {
		store := &getProjectFailStore{ProjectStore: mocks.NewProjectStore(), err: errors.New("db error")}
		svc := NewProjectService(store, &mockInstanceProvisioner{}, nil, nil)
		_, err := svc.GetProjectsByUserID(ctx, uuid.New().String())
		require.ErrorContains(t, err, "db error")
	})

	t.Run("empty list", func(t *testing.T) {
		store := mocks.NewProjectStore()
		svc := NewProjectService(store, &mockInstanceProvisioner{}, nil, nil)
		list, err := svc.GetProjectsByUserID(ctx, uuid.New().String())
		require.NoError(t, err)
		assert.Empty(t, list)
	})
}

func TestProjectService_DeleteProjectByIDAndUserID_errors(t *testing.T) {
	ctx := context.Background()

	t.Run("repo error on get", func(t *testing.T) {
		store := &getProjectFailStore{ProjectStore: mocks.NewProjectStore(), err: errors.New("db error")}
		svc := NewProjectService(store, &mockInstanceProvisioner{}, nil, nil)
		err := svc.DeleteProjectByIDAndUserID(ctx, uuid.New().String(), uuid.New().String())
		require.ErrorContains(t, err, "db error")
	})

	t.Run("provisioner error is non-fatal", func(t *testing.T) {
		userID := uuid.New()
		projects := mocks.NewProjectStore()
		p := projects.SeedProject(userID, "postgresql")
		prov := &mockInstanceProvisioner{
			deleteFn: func(_ context.Context, _ uuid.UUID, _ string) error {
				return errors.New("k8s error")
			},
		}
		svc := NewProjectService(projects, prov, nil, nil)
		err := svc.DeleteProjectByIDAndUserID(ctx, p.ID.String(), userID.String())
		require.NoError(t, err)
	})

	t.Run("delete repo error", func(t *testing.T) {
		userID := uuid.New()
		projects := mocks.NewProjectStore()
		p := projects.SeedProject(userID, "postgresql")
		store := &deleteProjectFailStore{ProjectStore: projects, deleteErr: errors.New("delete failed")}
		svc := NewProjectService(store, &mockInstanceProvisioner{}, nil, nil)
		err := svc.DeleteProjectByIDAndUserID(ctx, p.ID.String(), userID.String())
		require.ErrorContains(t, err, "delete failed")
	})

	t.Run("not found", func(t *testing.T) {
		projects := mocks.NewProjectStore()
		svc := NewProjectService(projects, &mockInstanceProvisioner{}, nil, nil)
		err := svc.DeleteProjectByIDAndUserID(ctx, uuid.New().String(), uuid.New().String())
		assert.ErrorIs(t, err, ErrProjectNotFound)
	})
}

func TestProjectService_CreateProject_repoError(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()

	t.Run("create fails", func(t *testing.T) {
		store := &getProjectFailStore{ProjectStore: mocks.NewProjectStore(), err: errors.New("insert failed")}
		svc := NewProjectService(store, &mockInstanceProvisioner{}, nil, nil)
		_, err := svc.CreateProject(ctx, userID, CreateProjectRequest{
			Name: "p", DBType: "postgres", Password: "SecurePass123!",
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrProjectCreateDB)
	})

	t.Run("success sets creating status", func(t *testing.T) {
		store := mocks.NewProjectStore()
		svc := NewProjectService(store, &mockInstanceProvisioner{}, nil, nil)
		p, err := svc.CreateProject(ctx, userID, CreateProjectRequest{
			Name: "p2", DBType: "postgres", Password: "SecurePass123!",
		})
		require.NoError(t, err)
		assert.Equal(t, "creating", p.Status)
	})
}

func TestProjectService_GetExternalConnectionInfo_errors(t *testing.T) {
	svc := NewProjectService(mocks.NewProjectStore(), &mockInstanceProvisioner{}, nil, nil)
	ctx := context.Background()

	t.Run("invalid project id", func(t *testing.T) {
		_, err := svc.GetExternalConnectionInfo(ctx, "bad", uuid.New().String())
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidProjectID)
	})

	t.Run("invalid user id", func(t *testing.T) {
		_, err := svc.GetExternalConnectionInfo(ctx, uuid.New().String(), "bad")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidUserID)
	})
}

func TestProjectService_GetProjectByIDAndUserID_invalidUserID(t *testing.T) {
	svc := NewProjectService(mocks.NewProjectStore(), &mockInstanceProvisioner{}, nil, nil)
	_, err := svc.GetProjectByIDAndUserID(context.Background(), uuid.New().String(), "bad")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidUserID)
}

type mockEvicter struct {
	called bool
	id     uuid.UUID
}

func (m *mockEvicter) EvictProject(projectID uuid.UUID) { m.called = true; m.id = projectID }

func TestProjectService_DeleteProjectByIDAndUserID_withEvicter(t *testing.T) {
	userID := uuid.New()
	projects := mocks.NewProjectStore()
	p := projects.SeedProject(userID, "postgresql")
	evicter := &mockEvicter{}
	svc := NewProjectService(projects, &mockInstanceProvisioner{}, nil, evicter)
	err := svc.DeleteProjectByIDAndUserID(context.Background(), p.ID.String(), userID.String())
	require.NoError(t, err)
	assert.True(t, evicter.called)
	assert.Equal(t, p.ID, evicter.id)
}

type deleteProjectFailStore struct {
	*mocks.ProjectStore
	deleteErr error
}

func (s *deleteProjectFailStore) DeleteByIDAndUserID(_ context.Context, _, _ uuid.UUID) error {
	return s.deleteErr
}

func TestProjectService_ProvisionInstanceAsync(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		projects := mocks.NewProjectStore()
		userID := uuid.New()
		p := projects.SeedProject(userID, "postgresql")
		prov := &mockInstanceProvisioner{
			createFn: func(ctx context.Context, projectID uuid.UUID, dbType, resourceTier, password string) (*ProvisionResult, error) {
				return &ProvisionResult{}, nil
			},
		}
		svc := NewProjectService(projects, prov, nil, nil)
		
		// The original method runs asynchronously inside a goroutine, but we can call it synchronously for the test
		svc.provisionInstanceAsync(ctx, p.ID, p.DBType, p.ResourceTier, "pass")

		updated, err := projects.GetByID(ctx, p.ID)
		require.NoError(t, err)
		assert.Equal(t, "running", updated.Status)
	})

	t.Run("provisioning fails updates status to failed", func(t *testing.T) {
		projects := mocks.NewProjectStore()
		userID := uuid.New()
		p := projects.SeedProject(userID, "postgresql")
		prov := &mockInstanceProvisioner{
			createFn: func(ctx context.Context, projectID uuid.UUID, dbType, resourceTier, password string) (*ProvisionResult, error) {
				return nil, errors.New("provisioning failed")
			},
		}
		svc := NewProjectService(projects, prov, nil, nil)
		
		svc.provisionInstanceAsync(ctx, p.ID, p.DBType, p.ResourceTier, "pass")

		updated, err := projects.GetByID(ctx, p.ID)
		require.NoError(t, err)
		assert.Equal(t, "failed", updated.Status)
	})
}
