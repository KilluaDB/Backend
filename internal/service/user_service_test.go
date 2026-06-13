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

func TestUserService_GetUser(t *testing.T) {
	users := mocks.NewUserStore()
	u := users.SeedUser("u@test.com", "hash", "user")
	svc := NewUserService(users, mocks.NewProjectStore(), nil)

	got, err := svc.GetUser(context.Background(), u.ID)
	require.NoError(t, err)
	assert.Equal(t, u.Email, got.Email)
	assert.Empty(t, got.PasswordHash)

	_, err = svc.GetUser(context.Background(), uuid.New())
	assert.EqualError(t, err, "user not found")
}

func TestUserService_GetAllUsers(t *testing.T) {
	users := mocks.NewUserStore()
	users.SeedUser("a@test.com", "hash", "user")
	users.SeedUser("b@test.com", "hash", "user")
	svc := NewUserService(users, mocks.NewProjectStore(), nil)

	all, err := svc.GetAllUsers(context.Background())
	require.NoError(t, err)
	assert.Len(t, all, 2)
	for _, u := range all {
		assert.Empty(t, u.PasswordHash)
	}
}

func TestUserService_UpdateUser(t *testing.T) {
	users := mocks.NewUserStore()
	admin := users.SeedUser("admin@test.com", "hash", "admin")
	target := users.SeedUser("target@test.com", "hash", "user")
	svc := NewUserService(users, mocks.NewProjectStore(), nil)
	ctx := context.Background()

	tests := []struct {
		name      string
		targetID  uuid.UUID
		actorID   uuid.UUID
		req       UpdateUserRequest
		wantErr   string
		wantEmail string
	}{
		{
			name:     "non-admin cannot change role",
			targetID: target.ID,
			actorID:  target.ID,
			req:      UpdateUserRequest{Role: strPtr("admin")},
			wantErr:  "only admins can change user roles",
		},
		{
			name:      "admin promotes user",
			targetID:  target.ID,
			actorID:   admin.ID,
			req:       UpdateUserRequest{Role: strPtr("admin")},
			wantEmail: target.Email,
		},
		{
			name:      "update email",
			targetID:  target.ID,
			actorID:   target.ID,
			req:       UpdateUserRequest{Email: strPtr("updated@test.com")},
			wantEmail: "updated@test.com",
		},
		{
			name:     "admin cannot demote self",
			targetID: admin.ID,
			actorID:  admin.ID,
			req:      UpdateUserRequest{Role: strPtr("user")},
			wantErr:  "admin cannot demote themselves",
		},
		{
			name:     "target not found",
			targetID: uuid.New(),
			actorID:  admin.ID,
			req:      UpdateUserRequest{},
			wantErr:  "user not found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.UpdateUser(ctx, tt.targetID, tt.actorID, tt.req)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			if tt.wantEmail != "" {
				assert.Equal(t, tt.wantEmail, got.Email)
			}
		})
	}
}

func TestUserService_DeleteUser_policy(t *testing.T) {
	users := mocks.NewUserStore()
	admin1 := users.SeedUser("a1@test.com", "hash", "admin")
	admin2 := users.SeedUser("a2@test.com", "hash", "admin")
	svc := NewUserService(users, mocks.NewProjectStore(), nil)
	ctx := context.Background()

	err := svc.DeleteUser(ctx, admin2.ID, admin1.ID)
	require.Error(t, err)
	assert.EqualError(t, err, "admins cannot delete other admins")

	usersSolo := mocks.NewUserStore()
	solo := usersSolo.SeedUser("solo@test.com", "hash", "admin")
	svcSolo := NewUserService(usersSolo, mocks.NewProjectStore(), nil)
	err = svcSolo.DeleteUser(ctx, solo.ID, solo.ID)
	require.Error(t, err)
	assert.EqualError(t, err, "cannot delete the last admin")

	err = svc.DeleteUser(ctx, uuid.New(), admin1.ID)
	require.Error(t, err)
	assert.EqualError(t, err, "user not found")
}

func strPtr(s string) *string { return &s }

type findUserFailStore struct {
	*mocks.UserStore
	err error
}

func (s *findUserFailStore) FindUserByID(_ context.Context, _ uuid.UUID) (*model.User, error) {
	return nil, s.err
}
func (s *findUserFailStore) FindAll(_ context.Context) ([]model.User, error) {
	return nil, s.err
}
func (s *findUserFailStore) Update(_ context.Context, _ *model.User) error {
	return s.err
}

func TestUserService_GetUser_repoError(t *testing.T) {
	store := &findUserFailStore{UserStore: mocks.NewUserStore(), err: errors.New("db error")}
	svc := NewUserService(store, mocks.NewProjectStore(), nil)
	_, err := svc.GetUser(context.Background(), uuid.New())
	require.ErrorContains(t, err, "db error")
}

func TestUserService_GetAllUsers_repoError(t *testing.T) {
	store := &findUserFailStore{UserStore: mocks.NewUserStore(), err: errors.New("db error")}
	svc := NewUserService(store, mocks.NewProjectStore(), nil)
	_, err := svc.GetAllUsers(context.Background())
	require.ErrorContains(t, err, "db error")
}

func TestUserService_UpdateUser_repoErrors(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	actorID := uuid.New()

	t.Run("find target fails", func(t *testing.T) {
		store := &findUserFailStore{UserStore: mocks.NewUserStore(), err: errors.New("target lookup failed")}
		svc := NewUserService(store, mocks.NewProjectStore(), nil)
		_, err := svc.UpdateUser(ctx, userID, actorID, UpdateUserRequest{})
		require.ErrorContains(t, err, "target lookup failed")
	})

	t.Run("find authenticated user fails", func(t *testing.T) {
		store := mocks.NewUserStore()
		target := store.SeedUser("target@test.com", "hash", "user")
		failStore := &findUserFailStore{UserStore: store, err: errors.New("auth lookup failed")}
		svc := NewUserService(failStore, mocks.NewProjectStore(), nil)
		_, err := svc.UpdateUser(ctx, target.ID, uuid.New(), UpdateUserRequest{})
		require.ErrorContains(t, err, "auth lookup failed")
	})
}

type updateUserFailStore struct {
	*mocks.UserStore
}

func (s *updateUserFailStore) Update(_ context.Context, _ *model.User) error {
	return errors.New("update failed")
}

func TestUserService_UpdateUser_updateFails(t *testing.T) {
	users := mocks.NewUserStore()
	target := users.SeedUser("target@test.com", "hash", "user")
	admin := users.SeedUser("admin@test.com", "hash", "admin")
	store := &updateUserFailStore{UserStore: users}

	svc := NewUserService(store, mocks.NewProjectStore(), nil)
	_, err := svc.UpdateUser(context.Background(), target.ID, admin.ID, UpdateUserRequest{Email: strPtr("new@test.com")})
	require.ErrorContains(t, err, "update failed")
}
