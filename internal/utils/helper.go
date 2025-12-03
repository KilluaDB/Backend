package utils

import "github.com/google/uuid"

func ParseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}
