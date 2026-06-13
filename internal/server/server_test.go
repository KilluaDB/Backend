package server

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateRequiredEnvVars(t *testing.T) {
	required := []string{
		"PORT",
		"DB_HOST",
		"DB_PORT",
		"DB_USERNAME",
		"DB_PASSWORD",
		"DB_DATABASE",
		"REDIS_ADDR",
		"ACCESS_TOKEN_SECRET",
		"REFRESH_TOKEN_SECRET",
		"GOOGLE_CLIENT_ID",
		"GOOGLE_CLIENT_SECRET",
		"GOOGLE_REDIRECT_URL",
	}

	// Backup original env vars
	originalEnv := make(map[string]string)
	for _, env := range required {
		originalEnv[env] = os.Getenv(env)
	}
	defer func() {
		for k, v := range originalEnv {
			os.Setenv(k, v)
		}
	}()

	t.Run("success all variables set", func(t *testing.T) {
		for _, env := range required {
			os.Setenv(env, "dummy-value")
		}

		err := validateRequiredEnvVars()
		assert.NoError(t, err)
	})

	t.Run("missing variable", func(t *testing.T) {
		for _, missingEnv := range required {
			// Set all to dummy
			for _, env := range required {
				os.Setenv(env, "dummy-value")
			}

			// Unset one
			os.Unsetenv(missingEnv)

			err := validateRequiredEnvVars()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), missingEnv+" is required")
		}
	})
}

func TestCloseResources_NilManagers(t *testing.T) {
	// Ensure that calling CloseResources with nil managers does not panic
	// Note: We avoid calling database.Close() if database is not initialized,
	// but the function unconditionally calls it. We will just test it doesn't crash
	// for the manager variables. To prevent database.Close from panicking,
	// database package usually handles nil pool safely.

	pgInstanceManager = nil
	mongoInstanceManager = nil

	assert.NotPanics(t, func() {
		CloseResources()
	})
}
