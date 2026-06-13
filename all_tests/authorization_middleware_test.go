package middleware

import (
	"net/http"
	"testing"

	"backend/internal/mocks"
	"backend/internal/testutil"
	"backend/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestRequireAdmin(t *testing.T) {
	store := mocks.NewUserStore()
	admin := store.SeedUser("admin@test.com", "hash", "admin")
	user := store.SeedUser("user@test.com", "hash", "user")

	tests := []struct {
		name       string
		userID     uuid.UUID
		wantStatus int
	}{
		{"no user in context", uuid.Nil, http.StatusUnauthorized},
		{"regular user", user.ID, http.StatusForbidden},
		{"admin user", admin.ID, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.GET("/admin", func(c *gin.Context) {
				if tt.userID != uuid.Nil {
					c.Set(utils.UserIDContextKey, tt.userID)
				}
				RequireAdmin(store)(c)
			}, func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			c, w := testutil.NewGinContext(http.MethodGet, "/admin", nil, nil)
			r.HandleContext(c)
			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}
