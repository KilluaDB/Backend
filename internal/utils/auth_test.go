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

func TestVerifyPassword_MalformedHash(t *testing.T) {
	tests := []struct {
		name        string
		encodedHash string
	}{
		// Five "$"-separated parts so the format check passes, but the inner
		// fields are individually corrupt.
		{"bad parameters", "argon2id$v=19$not-params$c2FsdA$aGFzaA"},
		{"bad salt encoding", "argon2id$v=19$m=65536,t=1,p=4$!!!notbase64!!!$aGFzaA"},
		{"bad hash encoding", "argon2id$v=19$m=65536,t=1,p=4$c2FsdA$!!!notbase64!!!"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Error(t, VerifyPassword(tt.encodedHash, "password"))
		})
	}
}

func TestGenerateStateOauthCookie(t *testing.T) {
	state, err := GenerateStateOauthCookie()
	require.NoError(t, err)
	assert.NotEmpty(t, state)

	state2, err := GenerateStateOauthCookie()
	require.NoError(t, err)
	assert.NotEqual(t, state, state2)
}
