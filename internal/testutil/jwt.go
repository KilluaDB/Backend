package testutil

import (
	"backend/internal/utils"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	TestAccessSecret  = "test-access-secret-key-32bytes!!"
	TestRefreshSecret = "test-refresh-secret-key-32bytes!"
)

// SetupJWTSecrets configures package-level JWT secrets for tests.
func SetupJWTSecrets(t *testing.T) {
	t.Helper()
	utils.AccessTokenSecret = []byte(TestAccessSecret)
	utils.RefreshTokenSecret = []byte(TestRefreshSecret)
}

// BearerToken returns a valid access JWT for the given user.
func BearerToken(t *testing.T, userID uuid.UUID) string {
	t.Helper()
	SetupJWTSecrets(t)
	token, err := utils.GenerateJWT(userID, time.Hour, utils.AccessTokenSecret)
	if err != nil {
		t.Fatalf("generate access token: %v", err)
	}
	return "Bearer " + token
}

// ExpiredBearerToken returns an expired access JWT.
func ExpiredBearerToken(t *testing.T, userID uuid.UUID) string {
	t.Helper()
	SetupJWTSecrets(t)
	claims := &utils.Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(utils.AccessTokenSecret)
	if err != nil {
		t.Fatalf("sign expired token: %v", err)
	}
	return "Bearer " + signed
}
