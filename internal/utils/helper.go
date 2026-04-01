package utils

import "github.com/google/uuid"

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
