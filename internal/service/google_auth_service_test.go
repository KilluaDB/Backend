package service

import (
	"context"
	"errors"
	"testing"

	"backend/internal/mocks"
	"backend/internal/model"
	"backend/internal/testutil"

	"golang.org/x/oauth2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubGoogleClient struct {
	email    string
	verified bool
	err      error
}

func (s stubGoogleClient) FetchUserInfo(ctx context.Context, accessToken string) (string, bool, error) {
	if s.err != nil {
		return "", false, s.err
	}
	return s.email, s.verified, nil
}

func TestGoogleAuthService_Callback(t *testing.T) {
	testutil.SetupJWTSecrets(t)
	users := mocks.NewUserStore()
	ctx := context.Background()

	t.Run("creates user and returns token", func(t *testing.T) {
		svc := NewGoogleAuthServiceWithClient(users, stubGoogleClient{email: "g@example.com", verified: true})
		token, err := svc.Callback(ctx, &oauth2.Token{AccessToken: "at"})
		require.NoError(t, err)
		assert.NotEmpty(t, token)
	})

	t.Run("existing user", func(t *testing.T) {
		users.SeedUser("existing@example.com", "", "user")
		svc := NewGoogleAuthServiceWithClient(users, stubGoogleClient{email: "existing@example.com", verified: true})
		token, err := svc.Callback(ctx, &oauth2.Token{AccessToken: "at"})
		require.NoError(t, err)
		assert.NotEmpty(t, token)
	})

	t.Run("unverified email", func(t *testing.T) {
		svc := NewGoogleAuthServiceWithClient(users, stubGoogleClient{email: "x@example.com", verified: false})
		_, err := svc.Callback(ctx, &oauth2.Token{AccessToken: "at"})
		assert.Error(t, err)
	})
}

type failingCreateStore struct {
	*mocks.UserStore
	createErr error
}

func (s *failingCreateStore) Create(_ context.Context, _ *model.User) error {
	return s.createErr
}

func TestGoogleAuthService_Callback_fetchUserInfoError(t *testing.T) {
	testutil.SetupJWTSecrets(t)
	svc := NewGoogleAuthServiceWithClient(mocks.NewUserStore(), stubGoogleClient{err: errors.New("network error")})
	_, err := svc.Callback(context.Background(), &oauth2.Token{AccessToken: "at"})
	require.ErrorContains(t, err, "network error")
}

func TestGoogleAuthService_Callback_createUserFails(t *testing.T) {
	testutil.SetupJWTSecrets(t)
	store := &failingCreateStore{UserStore: mocks.NewUserStore(), createErr: errors.New("db insert failed")}
	svc := NewGoogleAuthServiceWithClient(store, stubGoogleClient{email: "new@example.com", verified: true})
	_, err := svc.Callback(context.Background(), &oauth2.Token{AccessToken: "at"})
	require.ErrorContains(t, err, "failed to create user")
	require.ErrorContains(t, err, "db insert failed")
}
