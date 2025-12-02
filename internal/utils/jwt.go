package utils

import (
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	// These should normally come from environment variables for security.
	AccessTokenSecret  = []byte(os.Getenv("ACCESS_TOKEN_SECRET"))
	RefreshTokenSecret = []byte(os.Getenv("REFRESH_TOKEN_SECRET"))
)

// Claims represents JWT claims.
type Claims struct {
	jwt.RegisteredClaims
}

// GenerateJWT creates a signed JWT with expiration.
func GenerateTokens(userID uuid.UUID) (string, string, string, error) {
	jti := uuid.NewString()
	
	accessClaims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
		},
	}
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessString, err := accessToken.SignedString(AccessTokenSecret)
	if err != nil {
		return "", "", "", err
	}

	refreshClaims := &Claims{
		// UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(30*24*time.Hour)),
		},
	}
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshString, err := refreshToken.SignedString(RefreshTokenSecret)
	if err != nil {
		return "", "", "", err
	}

	return accessString, refreshString, jti, nil
}

// VerifyJWT parses and validates a JWT string.
func VerifyJWT(tokenStr string, secret []byte) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		return secret, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, jwt.ErrSignatureInvalid
}
