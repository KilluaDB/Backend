package handler

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// parseUserID converts middleware "userId" (uuid.UUID or string) to uuid.UUID.
func parseUserID(v any) (uuid.UUID, error) {
	switch x := v.(type) {
	case uuid.UUID:
		return x, nil
	case string:
		return uuid.Parse(x)
	default:
		return uuid.Nil, fmt.Errorf("invalid user id type: %T", v)
	}
}

// userIDFromGin returns the authenticated user UUID from context (set by auth middleware).
func userIDFromGin(c *gin.Context) (uuid.UUID, bool) {
	raw, exists := c.Get("userId")
	if !exists {
		return uuid.Nil, false
	}
	u, err := parseUserID(raw)
	if err != nil {
		return uuid.Nil, false
	}
	return u, true
}

// projectIDFromGin parses path param "id" as project UUID.
func projectIDFromGin(c *gin.Context) (uuid.UUID, error) {
	id := c.Param("id")
	if id == "" {
		return uuid.Nil, fmt.Errorf("project id is required")
	}
	return uuid.Parse(id)
}

// userAndProjectFromGin returns user and project UUIDs from a route under /projects/:id/...
// When ok is false: projectErr is non-nil if path param "id" is missing or not a valid UUID;
// projectErr is nil if the authenticated user id is missing or invalid.
func userAndProjectFromGin(c *gin.Context) (userUUID, projectUUID uuid.UUID, ok bool, projectErr error) {
	u, userOk := userIDFromGin(c)
	if !userOk {
		return uuid.Nil, uuid.Nil, false, nil
	}
	p, err := projectIDFromGin(c)
	if err != nil {
		return u, uuid.Nil, false, err
	}
	return u, p, true, nil
}
