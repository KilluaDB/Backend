package service

import (
	"context"
	"errors"
	"testing"

	"backend/internal/mocks"
	"backend/internal/model"
	"backend/internal/testutil"
	"backend/internal/utils"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthService_RegisterLoginRefreshLogout(t *testing.T) {
	testutil.SetupJWTSecrets(t)
	users := mocks.NewUserStore()
	store, cleanup := testutil.NewRefreshTokenStore(t)
	defer cleanup()
	svc := NewAuthService(users, store)
	ctx := context.Background()

	t.Run("register success", func(t *testing.T) {
		uid, access, refresh, err := svc.Register(ctx, &model.User{Email: "new@example.com", Password: "password123"})
		require.NoError(t, err)
		assert.NotEqual(t, uid.String(), "00000000-0000-0000-0000-000000000000")
		assert.NotEmpty(t, access)
		assert.NotEmpty(t, refresh)
	})

	t.Run("register duplicate", func(t *testing.T) {
		_, _, _, err := svc.Register(ctx, &model.User{Email: "new@example.com", Password: "password123"})
		assert.ErrorIs(t, err, ErrUserAlreadyExists)
	})

	hash, err := utils.Hash("password123")
	require.NoError(t, err)
	users.SeedUser("login@example.com", string(hash), "user")

	t.Run("login success", func(t *testing.T) {
		_, access, refresh, err := svc.Login(ctx, "login@example.com", "password123")
		require.NoError(t, err)
		assert.NotEmpty(t, access)
		assert.NotEmpty(t, refresh)
	})

	t.Run("login invalid password", func(t *testing.T) {
		_, _, _, err := svc.Login(ctx, "login@example.com", "wrong")
		assert.ErrorIs(t, err, ErrInvalidPassword)
	})

	t.Run("login unknown user", func(t *testing.T) {
		_, _, _, err := svc.Login(ctx, "missing@example.com", "password123")
		assert.ErrorIs(t, err, ErrUserNotFound)
	})

	t.Run("refresh and logout", func(t *testing.T) {
		_, _, refresh, err := svc.Login(ctx, "login@example.com", "password123")
		require.NoError(t, err)

		uid, newAccess, newRefresh, err := svc.Refresh(ctx, refresh)
		require.NoError(t, err)
		assert.NotEmpty(t, newAccess)
		assert.NotEmpty(t, newRefresh)
		assert.NotEqual(t, refresh, newRefresh)

		require.NoError(t, svc.Logout(ctx, newRefresh))
		_, _, _, err = svc.Refresh(ctx, newRefresh)
		assert.Error(t, err)

		_ = uid
	})

	t.Run("Register: refreshStore.Set fails", func(t *testing.T) {
		failStore := &failingRefreshStore{setErr: errors.New("redis down")}
		localUsers := mocks.NewUserStore()
		localSvc := NewAuthService(localUsers, failStore)
		_, _, _, err := localSvc.Register(ctx, &model.User{Email: "fail-reg-set@example.com", Password: "password123"})
		require.ErrorContains(t, err, "redis down")
	})

	t.Run("Login: refreshStore.Set fails", func(t *testing.T) {
		failStore := &failingRefreshStore{setErr: errors.New("redis unavailable")}
		localUsers := mocks.NewUserStore()
		h, err := utils.Hash("secret99")
		require.NoError(t, err)
		localUsers.SeedUser("login-fail-set@example.com", string(h), "user")

		localSvc := NewAuthService(localUsers, failStore)
		_, _, _, err = localSvc.Login(ctx, "login-fail-set@example.com", "secret99")
		require.ErrorContains(t, err, "redis unavailable")
	})

	t.Run("Refresh: refreshStore.Get returns unknown error", func(t *testing.T) {
		failStore := &failingRefreshStore{getErr: errors.New("connection timeout")}
		localUsers := mocks.NewUserStore()
		localSvc := NewAuthService(localUsers, failStore)
		_, _, _, err := localSvc.Refresh(ctx, "some-refresh-token")
		require.ErrorContains(t, err, "connection timeout")
	})

	t.Run("Refresh: nil refreshStore", func(t *testing.T) {
		localUsers := mocks.NewUserStore()
		localSvc := NewAuthService(localUsers, nil)
		_, _, refreshToken, err := localSvc.Register(ctx, &model.User{Email: "nilstore@example.com", Password: "password123"})
		require.NoError(t, err)
		require.NotEmpty(t, refreshToken)

		uid, newAccess, newRefresh, err := localSvc.Refresh(ctx, refreshToken)
		require.NoError(t, err)
		assert.NotEmpty(t, newAccess)
		assert.NotEmpty(t, newRefresh)
		assert.NotEqual(t, refreshToken, newRefresh)
		_ = uid
	})

	t.Run("Register with soft-deleted user: HardDeleteSoftDeletedByEmail fails", func(t *testing.T) {
		email := "softdelete@example.com"
		h, err := utils.Hash("irrelevant")
		require.NoError(t, err)
		baseUsers := mocks.NewUserStore()
		u := baseUsers.SeedUser(email, string(h), "user")
		require.NoError(t, baseUsers.Delete(ctx, u.ID))

		hardDelErr := errors.New("db error on hard delete")
		wrapped := &failingHardDeleteStore{UserStore: baseUsers, hardDeleteErr: hardDelErr}
		localSvc := NewAuthService(wrapped, &failingRefreshStore{})
		_, _, _, err = localSvc.Register(ctx, &model.User{Email: email, Password: "newpassword"})
		require.ErrorContains(t, err, "db error on hard delete")
	})
}

type failingRefreshStore struct {
	setErr    error
	getErr    error
	deleteErr error
}

func (s *failingRefreshStore) Set(_ context.Context, _ string, _ uuid.UUID) error { return s.setErr }
func (s *failingRefreshStore) Get(_ context.Context, _ string) (uuid.UUID, error) {
	return uuid.Nil, s.getErr
}
func (s *failingRefreshStore) Delete(_ context.Context, _ string) error { return s.deleteErr }

type failingHardDeleteStore struct {
	*mocks.UserStore
	hardDeleteErr error
}

func (s *failingHardDeleteStore) HardDeleteSoftDeletedByEmail(_ context.Context, _ string) error {
	return s.hardDeleteErr
}


