package service

import (
	"context"
	"errors"
	"testing"

	"backend/internal/mocks"
	rep "backend/internal/repository"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserService_DeleteUser_success(t *testing.T) {
	users := mocks.NewUserStore()
	projects := mocks.NewProjectStore()
	admin := users.SeedUser("admin@test.com", "hash", "admin")
	users.SeedUser("other-admin@test.com", "hash", "admin")
	target := users.SeedUser("user@test.com", "hash", "user")
	projects.SeedProject(target.ID, "postgresql")

	mock := newTxMock(t)
	mock.ExpectBegin()
	mock.ExpectCommit()

	svc := NewUserService(users, projects, nil)
	svc.txPool = mock

	err := svc.DeleteUser(context.Background(), target.ID, admin.ID)
	require.NoError(t, err)

	got, err := users.FindUserByID(context.Background(), target.ID)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestUserService_DeleteUser_authenticatedNotFound(t *testing.T) {
	users := mocks.NewUserStore()
	target := users.SeedUser("user@test.com", "hash", "user")
	svc := NewUserService(users, mocks.NewProjectStore(), nil)

	err := svc.DeleteUser(context.Background(), target.ID, uuid.New())
	require.Error(t, err)
	assert.EqualError(t, err, "authenticated user not found")
}

func TestUserService_DeleteUser_beginFails(t *testing.T) {
	users := mocks.NewUserStore()
	admin := users.SeedUser("admin@test.com", "hash", "admin")
	users.SeedUser("other@test.com", "hash", "admin")
	target := users.SeedUser("user@test.com", "hash", "user")

	mock := newTxMock(t)
	mock.ExpectBegin().WillReturnError(errors.New("begin failed"))

	svc := NewUserService(users, mocks.NewProjectStore(), nil)
	svc.txPool = mock

	err := svc.DeleteUser(context.Background(), target.ID, admin.ID)
	require.Error(t, err)
	assert.EqualError(t, err, "begin failed")
}

func TestUserService_DeleteUser_projectDeleteFails(t *testing.T) {
	users := mocks.NewUserStore()
	admin := users.SeedUser("admin@test.com", "hash", "admin")
	users.SeedUser("other@test.com", "hash", "admin")
	target := users.SeedUser("user@test.com", "hash", "user")

	mock := newTxMock(t)
	mock.ExpectBegin()
	mock.ExpectRollback()

	projects := &deleteProjectsFailStore{ProjectStore: mocks.NewProjectStore(), err: errors.New("project delete failed")}
	svc := NewUserService(users, projects, nil)
	svc.txPool = mock

	err := svc.DeleteUser(context.Background(), target.ID, admin.ID)
	require.Error(t, err)
	assert.EqualError(t, err, "project delete failed")
}

func TestUserService_DeleteUser_userDeleteFails(t *testing.T) {
	users := &deleteUserFailStore{UserStore: mocks.NewUserStore()}
	admin := users.SeedUser("admin@test.com", "hash", "admin")
	users.SeedUser("other@test.com", "hash", "admin")
	target := users.SeedUser("user@test.com", "hash", "user")

	mock := newTxMock(t)
	mock.ExpectBegin()
	mock.ExpectRollback()

	svc := NewUserService(users, mocks.NewProjectStore(), nil)
	svc.txPool = mock

	err := svc.DeleteUser(context.Background(), target.ID, admin.ID)
	require.Error(t, err)
	assert.EqualError(t, err, "user delete failed")
}

func TestUserService_DeleteUser_commitFails(t *testing.T) {
	users := mocks.NewUserStore()
	admin := users.SeedUser("admin@test.com", "hash", "admin")
	users.SeedUser("other@test.com", "hash", "admin")
	target := users.SeedUser("user@test.com", "hash", "user")

	mock := newTxMock(t)
	mock.ExpectBegin()
	mock.ExpectCommit().WillReturnError(errors.New("commit failed"))
	mock.ExpectRollback()

	svc := NewUserService(users, mocks.NewProjectStore(), nil)
	svc.txPool = mock

	err := svc.DeleteUser(context.Background(), target.ID, admin.ID)
	require.Error(t, err)
	assert.EqualError(t, err, "commit failed")
}

func TestUserService_DeleteUser_countAdminsFails(t *testing.T) {
	users := &countAdminsFailStore{UserStore: mocks.NewUserStore()}
	admin := users.SeedUser("solo-admin@test.com", "hash", "admin")
	users.SeedUser("other-admin@test.com", "hash", "admin")

	svc := NewUserService(users, mocks.NewProjectStore(), nil)
	err := svc.DeleteUser(context.Background(), admin.ID, admin.ID)
	require.Error(t, err)
	assert.EqualError(t, err, "count failed")
}

func newTxMock(t *testing.T) pgxmock.PgxPoolIface {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, mock.ExpectationsWereMet())
		mock.Close()
	})
	return mock
}

type deleteProjectsFailStore struct {
	*mocks.ProjectStore
	err error
}

func (s *deleteProjectsFailStore) DeleteByUserIDTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	return s.err
}

var _ rep.ProjectStore = (*deleteProjectsFailStore)(nil)

type deleteUserFailStore struct {
	*mocks.UserStore
}

func (s *deleteUserFailStore) DeleteTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	return errors.New("user delete failed")
}

var _ rep.UserStore = (*deleteUserFailStore)(nil)

type countAdminsFailStore struct {
	*mocks.UserStore
}

func (s *countAdminsFailStore) CountAdmins(ctx context.Context) (int, error) {
	return 0, errors.New("count failed")
}

var _ rep.UserStore = (*countAdminsFailStore)(nil)
