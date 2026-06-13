package utils

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateAndVerifyJWT(t *testing.T) {
	secret := []byte("test-secret-key-at-least-32-bytes-long")
	userID := uuid.New()

	token, err := GenerateJWT(userID, time.Hour, secret)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	claims, err := VerifyJWT(token, secret)
	require.NoError(t, err)
	assert.Equal(t, userID, claims.UserID)
}

func TestVerifyJWT_Errors(t *testing.T) {
	secret := []byte("test-secret-key-at-least-32-bytes-long")
	userID := uuid.New()
	valid, err := GenerateJWT(userID, time.Hour, secret)
	require.NoError(t, err)

	_, err = VerifyJWT("not.a.jwt", secret)
	assert.Error(t, err)

	_, err = VerifyJWT(valid, []byte("wrong-secret-key-32bytes-long!!!"))
	assert.Error(t, err)

	expiredClaims := &Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, expiredClaims)
	expired, err := tok.SignedString(secret)
	require.NoError(t, err)
	_, err = VerifyJWT(expired, secret)
	assert.Error(t, err)
}
