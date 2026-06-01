package utils

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const UserIDContextKey = "userId"

func ParseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}

func Contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// ParseUserID converts middleware "userId" (uuid.UUID or string) to uuid.UUID.
func ParseUserID(v any) (uuid.UUID, error) {
	switch x := v.(type) {
	case uuid.UUID:
		return x, nil
	case string:
		return uuid.Parse(x)
	default:
		return uuid.Nil, fmt.Errorf("invalid user id type: %T", v)
	}
}

// UserIDFromGin returns the authenticated user UUID from context (set by auth middleware).
func UserIDFromGin(c *gin.Context) (uuid.UUID, bool) {
	raw, exists := c.Get(UserIDContextKey)
	if !exists {
		return uuid.Nil, false
	}
	u, err := ParseUserID(raw)
	if err != nil {
		return uuid.Nil, false
	}
	return u, true
}

// ProjectIDFromGin parses path param "id" as project UUID.
func ProjectIDFromGin(c *gin.Context) (uuid.UUID, error) {
	id := c.Param("id")
	if id == "" {
		return uuid.Nil, fmt.Errorf("project id is required")
	}
	return uuid.Parse(id)
}

// UserAndProjectFromGin returns user and project UUIDs from a route under /projects/:id/...
// When ok is false: projectErr is non-nil if path param "id" is missing or not a valid UUID;
// projectErr is nil if the authenticated user id is missing or invalid.
func UserAndProjectFromGin(c *gin.Context) (userUUID, projectUUID uuid.UUID, ok bool, projectErr error) {
	u, userOk := UserIDFromGin(c)
	if !userOk {
		return uuid.Nil, uuid.Nil, false, nil
	}
	p, err := ProjectIDFromGin(c)
	if err != nil {
		return u, uuid.Nil, false, err
	}
	return u, p, true, nil
}
