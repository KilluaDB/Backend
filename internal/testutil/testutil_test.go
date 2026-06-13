package testutil

import (
	"backend/internal/utils"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestGinContext(t *testing.T) {
	c, w := NewGinContext("GET", "/test", map[string]interface{}{"key": "value"}, map[string]string{"X-Test": "test"})
	assert.NotNil(t, c)
	assert.NotNil(t, w)

	var res map[string]interface{}
	w.WriteString(`{"success": true}`)
	err := ParseJSONResponse(w, &res)
	assert.NoError(t, err)
	assert.Equal(t, true, res["success"])
}

func TestJWT(t *testing.T) {
	userID := uuid.New()

	// Test SetupJWTSecrets implicitly called by BearerToken
	token := BearerToken(t, userID)
	assert.Contains(t, token, "Bearer ")

	expiredToken := ExpiredBearerToken(t, userID)
	assert.Contains(t, expiredToken, "Bearer ")

	assert.Equal(t, []byte(TestAccessSecret), utils.AccessTokenSecret)
	assert.Equal(t, []byte(TestRefreshSecret), utils.RefreshTokenSecret)
}

func TestRedis(t *testing.T) {
	client, cleanup := NewRedisClient(t)
	assert.NotNil(t, client)
	cleanup()

	store, cleanup2 := NewRefreshTokenStore(t)
	assert.NotNil(t, store)
	cleanup2()
}
