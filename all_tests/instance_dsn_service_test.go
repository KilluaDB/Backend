package service

import (
	"context"
	"sync"
	"testing"

	"backend/internal/mocks"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstanceDSNService_GetConnectionDSN(t *testing.T) {
	projects := mocks.NewProjectStore()
	userID := uuid.New()
	p := projects.SeedProject(userID, "postgresql")
	p.Status = "running"
	_ = projects.Update(context.Background(), p)

	prov := &mockInstanceProvisioner{
		getConnFn: func(ctx context.Context, projectID uuid.UUID, dbType string) (*ProvisionResult, error) {
			return &ProvisionResult{DSN: "postgresql://u:p@host:5432/app"}, nil
		},
	}
	svc := NewInstanceDSNService(projects, prov)
	ctx := context.Background()

	dsn, _, err := svc.GetConnectionDSN(ctx, userID, p.ID)
	require.NoError(t, err)
	assert.Contains(t, dsn, "postgresql://")

	p.Status = "creating"
	_ = projects.Update(context.Background(), p)
	prov.getConnFn = func(ctx context.Context, projectID uuid.UUID, dbType string) (*ProvisionResult, error) {
		return nil, assert.AnError
	}
	_, _, err = svc.GetConnectionDSN(ctx, userID, p.ID)
	assert.ErrorIs(t, err, ErrNoRunningInstance)
}

func TestInstanceDSNService_GetExternalConnectionInfo(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	t.Run("external access not configured", func(t *testing.T) {
		projects := mocks.NewProjectStore()
		prov := &mockInstanceProvisioner{external: false}
		svc := NewInstanceDSNService(projects, prov)
		_, err := svc.GetExternalConnectionInfo(ctx, userID, uuid.New())
		assert.ErrorIs(t, err, ErrExternalAccessNotConfigured)
	})

	t.Run("project not found", func(t *testing.T) {
		projects := mocks.NewProjectStore()
		prov := &mockInstanceProvisioner{external: true}
		svc := NewInstanceDSNService(projects, prov)
		_, err := svc.GetExternalConnectionInfo(ctx, userID, uuid.New())
		assert.ErrorIs(t, err, ErrProjectNotAccessible)
	})

	t.Run("project not running", func(t *testing.T) {
		projects := mocks.NewProjectStore()
		p := projects.SeedProject(userID, "postgresql")
		p.Status = "creating"
		require.NoError(t, projects.Update(ctx, p))
		prov := &mockInstanceProvisioner{external: true}
		svc := NewInstanceDSNService(projects, prov)
		_, err := svc.GetExternalConnectionInfo(ctx, userID, p.ID)
		assert.ErrorIs(t, err, ErrNoRunningInstance)
	})

	t.Run("GetConnection fails", func(t *testing.T) {
		projects := mocks.NewProjectStore()
		p := projects.SeedProject(userID, "postgresql")
		p.Status = "running"
		require.NoError(t, projects.Update(ctx, p))
		prov := &mockInstanceProvisioner{
			external: true,
			getConnFn: func(_ context.Context, _ uuid.UUID, _ string) (*ProvisionResult, error) {
				return nil, assert.AnError
			},
		}
		svc := NewInstanceDSNService(projects, prov)
		_, err := svc.GetExternalConnectionInfo(ctx, userID, p.ID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "get connection from K8s")
	})

	t.Run("DSN parse fails", func(t *testing.T) {
		projects := mocks.NewProjectStore()
		p := projects.SeedProject(userID, "postgresql")
		p.Status = "running"
		require.NoError(t, projects.Update(ctx, p))
		prov := &mockInstanceProvisioner{
			external: true,
			getConnFn: func(_ context.Context, _ uuid.UUID, _ string) (*ProvisionResult, error) {
				return &ProvisionResult{DSN: "postgresql://user:pass@host:5432/db%GG"}, nil
			},
		}
		svc := NewInstanceDSNService(projects, prov)
		_, err := svc.GetExternalConnectionInfo(ctx, userID, p.ID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse internal DSN")
	})

	t.Run("postgresql success", func(t *testing.T) {
		projects := mocks.NewProjectStore()
		p := projects.SeedProject(userID, "postgresql")
		p.Status = "running"
		require.NoError(t, projects.Update(ctx, p))
		prov := &mockInstanceProvisioner{
			external: true,
			getConnFn: func(_ context.Context, _ uuid.UUID, _ string) (*ProvisionResult, error) {
				return &ProvisionResult{DSN: "postgresql://app:mysecret@host:5432/app"}, nil
			},
		}
		svc := NewInstanceDSNService(projects, prov)
		info, err := svc.GetExternalConnectionInfo(ctx, userID, p.ID)
		require.NoError(t, err)
		assert.Contains(t, info.ConnectionString, "postgresql://")
		assert.Equal(t, "db.example.com", info.Host)
		assert.Equal(t, 5432, info.Port)
		assert.Equal(t, "app", info.Database)
		assert.Equal(t, "app", info.Username)
		assert.Equal(t, "mysecret", info.Password)
	})

	t.Run("mongodb success", func(t *testing.T) {
		projects := mocks.NewProjectStore()
		p := projects.SeedProject(userID, "mongodb")
		p.Status = "running"
		require.NoError(t, projects.Update(ctx, p))
		prov := &mockInstanceProvisioner{
			external: true,
			getConnFn: func(_ context.Context, _ uuid.UUID, _ string) (*ProvisionResult, error) {
				return &ProvisionResult{DSN: "mongodb://admin:mysecret@host:27017/app"}, nil
			},
		}
		svc := NewInstanceDSNService(projects, prov)
		info, err := svc.GetExternalConnectionInfo(ctx, userID, p.ID)
		require.NoError(t, err)
		assert.Contains(t, info.ConnectionString, "mongodb://")
		assert.Equal(t, "db.example.com", info.Host)
		assert.Equal(t, 27017, info.Port)
		assert.Equal(t, "app", info.Database)
		assert.Equal(t, "admin", info.Username)
		assert.Equal(t, "mysecret", info.Password)
	})
}

func TestInstanceDSNService_GetConnectionDSN_statusAutoCorrect(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	projects := mocks.NewProjectStore()
	spy := &spyProjectStore{ProjectStore: projects}

	p := projects.SeedProject(userID, "postgresql")
	p.Status = "creating"
	require.NoError(t, projects.Update(ctx, p))

	prov := &mockInstanceProvisioner{
		getConnFn: func(_ context.Context, _ uuid.UUID, _ string) (*ProvisionResult, error) {
			return &ProvisionResult{DSN: "postgresql://u:p@host:5432/app"}, nil
		},
	}

	svc := NewInstanceDSNService(spy, prov)
	dsn, _, err := svc.GetConnectionDSN(ctx, userID, p.ID)
	require.NoError(t, err)
	assert.Contains(t, dsn, "postgresql://")

	spy.mu.Lock()
	require.Len(t, spy.updateCalls, 1)
	assert.Equal(t, p.ID, spy.updateCalls[0].id)
	assert.Equal(t, "running", spy.updateCalls[0].status)
	spy.mu.Unlock()
}

type spyProjectStore struct {
	*mocks.ProjectStore
	mu          sync.Mutex
	updateCalls []struct {
		id     uuid.UUID
		status string
	}
}

func (s *spyProjectStore) UpdateRuntimeStatus(ctx context.Context, id uuid.UUID, status string) error {
	s.mu.Lock()
	s.updateCalls = append(s.updateCalls, struct {
		id     uuid.UUID
		status string
	}{id, status})
	s.mu.Unlock()
	return s.ProjectStore.UpdateRuntimeStatus(ctx, id, status)
}
