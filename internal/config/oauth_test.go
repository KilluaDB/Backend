package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOAuthConfig(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_ID", "my-client-id")
	t.Setenv("GOOGLE_CLIENT_SECRET", "my-client-secret")
	t.Setenv("GOOGLE_REDIRECT_URL", "https://example.com/callback")

	cfg, err := OAuthConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "my-client-id", cfg.ClientID)
	assert.Equal(t, "https://example.com/callback", cfg.RedirectURL)
	assert.ElementsMatch(t, []string{"openid", "email", "profile"}, cfg.Scopes)
	assert.Contains(t, cfg.Scopes, "email")
}
