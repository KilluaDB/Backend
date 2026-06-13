package service

import (
	"context"
	"testing"

	"backend/internal/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGoogleAuthService(t *testing.T) {
	svc := NewGoogleAuthService(mocks.NewUserStore())
	require.NotNil(t, svc)
	// The production constructor wires the real Google userinfo client.
	_, ok := svc.userInfo.(defaultGoogleUserInfoClient)
	assert.True(t, ok)
}

// FetchUserInfo's HTTP call fails fast and deterministically with an
// already-cancelled context (the transport short-circuits before dialing),
// covering the request-build and Do-error branches without any network.
func TestDefaultGoogleUserInfoClient_FetchUserInfo_doError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := defaultGoogleUserInfoClient{}.FetchUserInfo(ctx, "some-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get user info")
}
