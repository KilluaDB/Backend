package service

import (
	"context"
	"errors"
	"testing"

	"backend/internal/mongodb/model"
	"backend/internal/mongodb/repository"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// stubConn is a fake infra.InstanceConnectionService. It returns a preconfigured
// database (or error) so CollectionService can be exercised without provisioning
// a real instance.
type stubConn struct {
	db  *mongo.Database
	err error
}

func (s stubConn) GetDatabase(ctx context.Context, userID, projectID uuid.UUID) (*mongo.Database, error) {
	return s.db, s.err
}

// lazyDB returns a *mongo.Database from a client that has NOT dialed anything.
// Combined with an already-cancelled context, repo operations fail immediately
// with a deterministic "server selection error: context canceled" — no network,
// no timing dependence.
func lazyDB(t *testing.T) *mongo.Database {
	t.Helper()
	client, err := mongo.Connect(options.Client().ApplyURI("mongodb://127.0.0.1:27019/testdb"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
	return client.Database("testdb")
}

// cancelledCtx returns a context that is already cancelled.
func cancelledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// newRepoForTest builds the real (stateless) collection repository.
func newRepoForTest() *repository.CollectionRepository {
	return repository.NewCollectionRepository()
}

func TestNewCollectionService(t *testing.T) {
	conn := stubConn{}
	repo := newRepoForTest()
	svc := NewCollectionService(conn, repo)
	require.NotNil(t, svc)
	assert.Equal(t, conn, svc.conn)
	assert.Same(t, repo, svc.repo)
}

func TestCollectionService_ListCollections(t *testing.T) {
	uid, pid := uuid.New(), uuid.New()

	t.Run("GetDatabase error propagates", func(t *testing.T) {
		wantErr := errors.New("no instance")
		svc := NewCollectionService(stubConn{err: wantErr}, newRepoForTest())
		got, err := svc.ListCollections(context.Background(), uid, pid)
		assert.Nil(t, got)
		assert.ErrorIs(t, err, wantErr)
	})

	t.Run("repo error propagates", func(t *testing.T) {
		svc := NewCollectionService(stubConn{db: lazyDB(t)}, newRepoForTest())
		got, err := svc.ListCollections(cancelledCtx(), uid, pid)
		assert.Nil(t, got)
		assert.Error(t, err)
	})
}

func TestCollectionService_CreateCollection(t *testing.T) {
	uid, pid := uuid.New(), uuid.New()

	t.Run("invalid name rejected before connecting", func(t *testing.T) {
		// conn.db is nil and repo is empty: reaching either would panic, proving
		// validation short-circuits first.
		svc := NewCollectionService(stubConn{}, newRepoForTest())
		err := svc.CreateCollection(context.Background(), uid, pid, "  ")
		assert.ErrorIs(t, err, ErrInvalidCollectionName)
	})

	t.Run("GetDatabase error propagates", func(t *testing.T) {
		wantErr := errors.New("no instance")
		svc := NewCollectionService(stubConn{err: wantErr}, newRepoForTest())
		err := svc.CreateCollection(context.Background(), uid, pid, "users")
		assert.ErrorIs(t, err, wantErr)
	})

	t.Run("repo error propagates (non-mongo-code)", func(t *testing.T) {
		svc := NewCollectionService(stubConn{db: lazyDB(t)}, newRepoForTest())
		err := svc.CreateCollection(cancelledCtx(), uid, pid, "users")
		assert.Error(t, err)
		assert.NotErrorIs(t, err, ErrCollectionAlreadyExists)
	})
}

func TestCollectionService_DeleteCollection(t *testing.T) {
	uid, pid := uuid.New(), uuid.New()

	t.Run("invalid name rejected", func(t *testing.T) {
		svc := NewCollectionService(stubConn{}, newRepoForTest())
		err := svc.DeleteCollection(context.Background(), uid, pid, "\x00")
		assert.ErrorIs(t, err, ErrInvalidCollectionName)
	})

	t.Run("GetDatabase error propagates", func(t *testing.T) {
		wantErr := errors.New("no instance")
		svc := NewCollectionService(stubConn{err: wantErr}, newRepoForTest())
		err := svc.DeleteCollection(context.Background(), uid, pid, "users")
		assert.ErrorIs(t, err, wantErr)
	})

	t.Run("repo error propagates (non-mongo-code)", func(t *testing.T) {
		svc := NewCollectionService(stubConn{db: lazyDB(t)}, newRepoForTest())
		err := svc.DeleteCollection(cancelledCtx(), uid, pid, "users")
		assert.Error(t, err)
		assert.NotErrorIs(t, err, ErrCollectionNotFound)
	})
}

func TestCollectionService_AddField(t *testing.T) {
	uid, pid := uuid.New(), uuid.New()
	truthy := true

	t.Run("invalid collection name rejected", func(t *testing.T) {
		svc := NewCollectionService(stubConn{}, newRepoForTest())
		_, err := svc.AddField(context.Background(), uid, pid, "  ", model.AddFieldRequest{Field: "x"})
		assert.ErrorIs(t, err, ErrInvalidCollectionName)
	})

	t.Run("invalid field name rejected", func(t *testing.T) {
		svc := NewCollectionService(stubConn{}, newRepoForTest())
		_, err := svc.AddField(context.Background(), uid, pid, "users", model.AddFieldRequest{Field: "$bad"})
		assert.ErrorIs(t, err, ErrInvalidFieldName)
	})

	t.Run("GetDatabase error propagates", func(t *testing.T) {
		wantErr := errors.New("no instance")
		svc := NewCollectionService(stubConn{err: wantErr}, newRepoForTest())
		_, err := svc.AddField(context.Background(), uid, pid, "users", model.AddFieldRequest{Field: "status"})
		assert.ErrorIs(t, err, wantErr)
	})

	t.Run("repo error propagates with default updateExisting", func(t *testing.T) {
		svc := NewCollectionService(stubConn{db: lazyDB(t)}, newRepoForTest())
		// UpdateExisting nil -> defaults to true branch.
		res, err := svc.AddField(cancelledCtx(), uid, pid, "users", model.AddFieldRequest{Field: "status", Default: "active"})
		assert.Nil(t, res)
		assert.Error(t, err)
	})

	t.Run("repo error propagates with explicit updateExisting", func(t *testing.T) {
		svc := NewCollectionService(stubConn{db: lazyDB(t)}, newRepoForTest())
		// UpdateExisting non-nil -> exercises the dereference branch.
		res, err := svc.AddField(cancelledCtx(), uid, pid, "users", model.AddFieldRequest{Field: "status", UpdateExisting: &truthy})
		assert.Nil(t, res)
		assert.Error(t, err)
	})
}

func TestCollectionService_RemoveField(t *testing.T) {
	uid, pid := uuid.New(), uuid.New()

	t.Run("invalid collection name rejected", func(t *testing.T) {
		svc := NewCollectionService(stubConn{}, newRepoForTest())
		_, err := svc.RemoveField(context.Background(), uid, pid, "", "field")
		assert.ErrorIs(t, err, ErrInvalidCollectionName)
	})

	t.Run("invalid field name rejected", func(t *testing.T) {
		svc := NewCollectionService(stubConn{}, newRepoForTest())
		_, err := svc.RemoveField(context.Background(), uid, pid, "users", "$bad")
		assert.ErrorIs(t, err, ErrInvalidFieldName)
	})

	t.Run("GetDatabase error propagates", func(t *testing.T) {
		wantErr := errors.New("no instance")
		svc := NewCollectionService(stubConn{err: wantErr}, newRepoForTest())
		_, err := svc.RemoveField(context.Background(), uid, pid, "users", "status")
		assert.ErrorIs(t, err, wantErr)
	})

	t.Run("repo error propagates (non-mongo-code)", func(t *testing.T) {
		svc := NewCollectionService(stubConn{db: lazyDB(t)}, newRepoForTest())
		res, err := svc.RemoveField(cancelledCtx(), uid, pid, "users", "status")
		assert.Nil(t, res)
		assert.Error(t, err)
	})
}
