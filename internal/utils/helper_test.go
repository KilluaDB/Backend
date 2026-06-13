package utils

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseUUID(t *testing.T) {
	id := uuid.New()
	parsed, err := ParseUUID(id.String())
	require.NoError(t, err)
	assert.Equal(t, id, parsed)
	_, err = ParseUUID("bad")
	assert.Error(t, err)
}

func TestContains(t *testing.T) {
	assert.True(t, Contains([]string{"a", "b"}, "b"))
	assert.False(t, Contains([]string{"a"}, "c"))
}

func TestParseUserID(t *testing.T) {
	id := uuid.New()
	got, err := ParseUserID(id)
	require.NoError(t, err)
	assert.Equal(t, id, got)

	got2, err := ParseUserID(id.String())
	require.NoError(t, err)
	assert.Equal(t, id, got2)

	_, err = ParseUserID(123)
	assert.Error(t, err)
}

func TestUserIDFromGin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	id := uuid.New()
	c.Set(UserIDContextKey, id)

	got, ok := UserIDFromGin(c)
	assert.True(t, ok)
	assert.Equal(t, id, got)
}

func TestUserIDFromGin_Errors(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Key not present in context.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	_, ok := UserIDFromGin(c)
	assert.False(t, ok)

	// Key present but of an unparseable type.
	c2, _ := gin.CreateTestContext(w)
	c2.Set(UserIDContextKey, 123)
	_, ok = UserIDFromGin(c2)
	assert.False(t, ok)
}

func TestProjectIDFromGin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	_, err := ProjectIDFromGin(c)
	assert.NoError(t, err)

	c.Params = nil
	_, err = ProjectIDFromGin(c)
	assert.Error(t, err)
}

func TestUserAndProjectFromGin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	uid := uuid.New()
	pid := uuid.New()
	c.Set(UserIDContextKey, uid)
	c.Params = gin.Params{{Key: "id", Value: pid.String()}}

	u, p, ok, err := UserAndProjectFromGin(c)
	require.True(t, ok)
	require.NoError(t, err)
	assert.Equal(t, uid, u)
	assert.Equal(t, pid, p)
}

func TestUserAndProjectFromGin_Errors(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Missing user: ok is false and projectErr is nil.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	_, _, ok, projErr := UserAndProjectFromGin(c)
	assert.False(t, ok)
	assert.NoError(t, projErr)

	// User present but project id missing: ok is false and projectErr is set.
	c2, _ := gin.CreateTestContext(w)
	c2.Set(UserIDContextKey, uuid.New())
	_, _, ok, projErr = UserAndProjectFromGin(c2)
	assert.False(t, ok)
	assert.Error(t, projErr)
}
