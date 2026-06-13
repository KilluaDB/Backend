package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashAndVerifyPassword(t *testing.T) {
	password := "SecurePass123!"
	hashed, err := Hash(password)
	require.NoError(t, err)
	require.NotEmpty(t, hashed)

	assert.NoError(t, VerifyPassword(string(hashed), password))
	assert.Error(t, VerifyPassword(string(hashed), "wrong"))
	assert.Error(t, VerifyPassword("invalid-format", password))
}

func TestGenerateStateOauthCookie(t *testing.T) {
	state, err := GenerateStateOauthCookie()
	require.NoError(t, err)
	assert.NotEmpty(t, state)

	state2, err := GenerateStateOauthCookie()
	require.NoError(t, err)
	assert.NotEqual(t, state, state2)
}
