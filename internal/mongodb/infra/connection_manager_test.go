package infra

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type mockDSNProvider struct {
	dsn        string
	instanceID uuid.UUID
	err        error
	callCount  int
}

func (m *mockDSNProvider) GetConnectionDSN(_ context.Context, _, _ uuid.UUID) (string, uuid.UUID, error) {
	m.callCount++
	return m.dsn, m.instanceID, m.err
}

func TestMongoConnectionManager_GetDatabase_dsnProviderError(t *testing.T) {
	provider := &mockDSNProvider{err: assert.AnError}
	mgr := NewMongoConnectionManager(provider)

	newProjectID := uuid.New()
	_, err := mgr.GetDatabase(context.Background(), uuid.New(), newProjectID)
	require.Error(t, err)
	assert.Equal(t, 1, provider.callCount)
}

func TestMongoConnectionManager_GetDatabase_dsnRotation(t *testing.T) {
	provider := &mockDSNProvider{
		dsn: "mongodb://host1:27017/mydb",
	}
	mgr := NewMongoConnectionManager(provider)
	mgr.connectFn = func(ctx context.Context, dsn string) (*mongo.Client, error) {
		return mongo.Connect(options.Client().ApplyURI(dsn))
	}

	projectID := uuid.New()

	_, _, err := mgr.acquireCachedClient(context.Background(), projectID, provider.dsn)
	require.NoError(t, err)

	provider.dsn = "mongodb://host2:27017/mydb"

	_, _, err = mgr.acquireCachedClient(context.Background(), projectID, provider.dsn)
	require.NoError(t, err)

	mgr.clientMu.Lock()
	entry := mgr.clients[projectID]
	mgr.clientMu.Unlock()
	assert.Equal(t, provider.dsn, entry.dsn)
}

func TestMongoConnectionManager_EvictProject(t *testing.T) {
	provider := &mockDSNProvider{
		dsn: "mongodb://host1:27017/mydb",
	}
	mgr := NewMongoConnectionManager(provider)
	mgr.connectFn = func(ctx context.Context, dsn string) (*mongo.Client, error) {
		return mongo.Connect(options.Client().ApplyURI(dsn))
	}

	unknownID := uuid.New()
	mgr.EvictProject(unknownID)

	projectID := uuid.New()
	_, err := mgr.GetDatabase(context.Background(), uuid.New(), projectID)
	require.NoError(t, err)
	assert.Equal(t, 1, provider.callCount)

	mgr.EvictProject(projectID)

	_, err = mgr.GetDatabase(context.Background(), uuid.New(), projectID)
	require.NoError(t, err)
	assert.Equal(t, 2, provider.callCount)
}

func TestMongoConnectionManager_CloseAll(t *testing.T) {
	provider := &mockDSNProvider{
		dsn: "mongodb://host1:27017/mydb",
	}
	mgr := NewMongoConnectionManager(provider)
	mgr.connectFn = func(ctx context.Context, dsn string) (*mongo.Client, error) {
		return mongo.Connect(options.Client().ApplyURI(dsn))
	}

	_, err := mgr.GetDatabase(context.Background(), uuid.New(), uuid.New())
	require.NoError(t, err)
	assert.Equal(t, 1, provider.callCount)

	mgr.CloseAll()

	mgr.EvictProject(uuid.New())

	_, err = mgr.GetDatabase(context.Background(), uuid.New(), uuid.New())
	require.NoError(t, err)
	assert.Equal(t, 2, provider.callCount)
}

func TestMongoConnectionManager_GetInstanceID(t *testing.T) {
	expectedID := uuid.New()

	t.Run("provider error", func(t *testing.T) {
		provider := &mockDSNProvider{err: assert.AnError}
		mgr := NewMongoConnectionManager(provider)
		_, err := mgr.GetInstanceID(context.Background(), uuid.New(), uuid.New())
		require.Error(t, err)
	})

	t.Run("provider success", func(t *testing.T) {
		provider := &mockDSNProvider{
			dsn:        "mongodb://host:27017/mydb",
			instanceID: expectedID,
		}
		mgr := NewMongoConnectionManager(provider)
		id, err := mgr.GetInstanceID(context.Background(), uuid.New(), uuid.New())
		require.NoError(t, err)
		assert.Equal(t, expectedID, id)
	})
}
