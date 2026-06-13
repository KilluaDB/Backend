package infra

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// These tests exercise only the pure / error-handling branches of the
// connection manager that are reachable without a live MongoDB server. No real
// connection is ever opened: connectFn is stubbed, or we rely on mongo.Connect
// failing synchronously on an invalid URI. The happy-path cache-hit return
// (a successful Ping against a live server) is not reachable here and is left
// to an integration/mtest harness.

// TestMongoConnectionManager_GetDatabase_connectError verifies that an error
// from connectFn is propagated by acquireCachedClient -> GetDatabase and that
// nothing is cached.
func TestMongoConnectionManager_GetDatabase_connectError(t *testing.T) {
	provider := &mockDSNProvider{dsn: "mongodb://host:27017/mydb"}
	mgr := NewMongoConnectionManager(provider)

	sentinel := errors.New("dial refused")
	mgr.connectFn = func(ctx context.Context, dsn string) (*mongo.Client, error) {
		return nil, sentinel
	}

	projectID := uuid.New()
	_, err := mgr.GetDatabase(context.Background(), uuid.New(), projectID)
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)

	// Failed connect must not populate the cache.
	mgr.clientMu.Lock()
	_, cached := mgr.clients[projectID]
	mgr.clientMu.Unlock()
	assert.False(t, cached, "failed connection must not be cached")
}

// TestMongoConnectionManager_GetDatabase_cacheHitPingFails covers the cache-hit
// path where the cached entry has the same DSN but its Ping fails (no live
// server), forcing eviction and a reconnect. The second call must therefore
// invoke connectFn again.
func TestMongoConnectionManager_GetDatabase_cacheHitPingFails(t *testing.T) {
	provider := &mockDSNProvider{dsn: "mongodb://host1:27017/mydb"}
	mgr := NewMongoConnectionManager(provider)

	var connectCalls int
	mgr.connectFn = func(ctx context.Context, dsn string) (*mongo.Client, error) {
		connectCalls++
		// Build a client without dialing; Ping later fails (no server), which is
		// exactly the branch we want to cover.
		return mongo.Connect(options.Client().ApplyURI(dsn))
	}

	userID := uuid.New()
	projectID := uuid.New()

	_, err := mgr.GetDatabase(context.Background(), userID, projectID)
	require.NoError(t, err)
	require.Equal(t, 1, connectCalls)

	// Same DSN -> cache hit -> Ping (fails against no server) -> reconnect.
	_, err = mgr.GetDatabase(context.Background(), userID, projectID)
	require.NoError(t, err)
	assert.Equal(t, 2, connectCalls, "stale ping should force a reconnect")
}

// TestMongoConnectionManager_connectAndCacheClient_raceSameDSN covers the
// concurrent-writer re-check in connectAndCacheClient: while this goroutine is
// "dialing", another writer populates the cache with the SAME dsn. The freshly
// dialed client must be discarded and the already-cached one returned. We
// simulate the concurrent writer by seeding the cache from inside connectFn,
// which runs without the lock held.
func TestMongoConnectionManager_connectAndCacheClient_raceSameDSN(t *testing.T) {
	const dsn = "mongodb://host1:27017/mydb"
	provider := &mockDSNProvider{dsn: dsn}
	mgr := NewMongoConnectionManager(provider)

	projectID := uuid.New()
	racer, err := mongo.Connect(options.Client().ApplyURI(dsn))
	require.NoError(t, err)

	mgr.connectFn = func(ctx context.Context, d string) (*mongo.Client, error) {
		// Simulate a concurrent goroutine that already cached a client for the
		// same project + dsn before we finished dialing.
		mgr.clientMu.Lock()
		mgr.clients[projectID] = cachedMongoClient{dsn: dsn, dbName: "mydb", client: racer}
		mgr.clientMu.Unlock()
		return mongo.Connect(options.Client().ApplyURI(d))
	}

	db, err := mgr.GetDatabase(context.Background(), uuid.New(), projectID)
	require.NoError(t, err)
	require.NotNil(t, db)

	// The cached (racer) client must have won.
	mgr.clientMu.Lock()
	entry := mgr.clients[projectID]
	mgr.clientMu.Unlock()
	assert.Same(t, racer, entry.client, "the already-cached client must be retained")
}

// TestMongoConnectionManager_connectAndCacheClient_raceDifferentDSN covers the
// re-check branch where a concurrent writer cached a client under a DIFFERENT
// dsn: that stale entry is disconnected and overwritten by the newly dialed
// client.
func TestMongoConnectionManager_connectAndCacheClient_raceDifferentDSN(t *testing.T) {
	const dsn = "mongodb://host2:27017/mydb"
	provider := &mockDSNProvider{dsn: dsn}
	mgr := NewMongoConnectionManager(provider)

	projectID := uuid.New()
	stale, err := mongo.Connect(options.Client().ApplyURI("mongodb://old:27017/olddb"))
	require.NoError(t, err)

	var dialed *mongo.Client
	mgr.connectFn = func(ctx context.Context, d string) (*mongo.Client, error) {
		mgr.clientMu.Lock()
		mgr.clients[projectID] = cachedMongoClient{dsn: "mongodb://old:27017/olddb", dbName: "olddb", client: stale}
		mgr.clientMu.Unlock()
		dialed, err = mongo.Connect(options.Client().ApplyURI(d))
		return dialed, err
	}

	_, err = mgr.GetDatabase(context.Background(), uuid.New(), projectID)
	require.NoError(t, err)

	// The newly dialed client for the current dsn must have replaced the stale one.
	mgr.clientMu.Lock()
	entry := mgr.clients[projectID]
	mgr.clientMu.Unlock()
	assert.Equal(t, dsn, entry.dsn)
	assert.Same(t, dialed, entry.client, "stale-dsn entry must be overwritten")
}

// TestDefaultConnectClient_invalidURI covers the error-wrapping branch of
// defaultConnectClient: an invalid URI makes mongo.Connect fail synchronously,
// before any network I/O.
func TestDefaultConnectClient_invalidURI(t *testing.T) {
	client, err := defaultConnectClient(context.Background(), "not-a-valid-uri")
	require.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "connect instance mongo")
}

// TestDatabaseNameFromDSN exercises the DSN -> database-name parsing, including
// the fallbacks to "admin".
func TestDatabaseNameFromDSN(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{name: "explicit db", dsn: "mongodb://host:27017/mydb", want: "mydb"},
		{name: "explicit db with options", dsn: "mongodb://host:27017/mydb?replicaSet=rs0", want: "mydb"},
		{name: "no db path defaults to admin", dsn: "mongodb://host:27017", want: "admin"},
		{name: "trailing slash defaults to admin", dsn: "mongodb://host:27017/", want: "admin"},
		{name: "extra path segment trimmed", dsn: "mongodb://host:27017/mydb/extra", want: "mydb"},
		{name: "credentials and db", dsn: "mongodb://user:pass@host:27017/appdb", want: "appdb"},
		{name: "unparseable defaults to admin", dsn: "mongodb://host:27017/%zz", want: "admin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, databaseNameFromDSN(tt.dsn))
		})
	}
}
