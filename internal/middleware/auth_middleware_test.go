package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"backend/internal/testutil"
	"backend/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestAuthenticate(t *testing.T) {
	testutil.SetupJWTSecrets(t)
	userID := uuid.New()

	r := gin.New()
	r.GET("/protected", Authenticate, func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{"missing header", "", http.StatusUnauthorized},
		{"invalid format", "Token xyz", http.StatusUnauthorized},
		{"invalid jwt", "Bearer invalid.token.here", http.StatusUnauthorized},
		{"expired jwt", testutil.ExpiredBearerToken(t, userID), http.StatusUnauthorized},
		{"valid jwt", testutil.BearerToken(t, userID), http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.wantStatus == http.StatusOK {
				// middleware sets context on gin.Context during request; verified via status only
				_ = utils.UserIDContextKey
			}
		})
	}
}

func TestAuthenticate_SetsUserIDInContext(t *testing.T) {
	userID := uuid.New()

	r := gin.New()
	r.GET("/check", Authenticate, func(c *gin.Context) {
		uid, _ := utils.UserIDFromGin(c)
		c.String(http.StatusOK, uid.String())
	})

	req := httptest.NewRequest(http.MethodGet, "/check", nil)
	req.Header.Set("Authorization", testutil.BearerToken(t, userID))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, userID.String(), w.Body.String())
}
