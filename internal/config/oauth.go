package config

import (
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

func OAuthConfig() (*oauth2.Config, error) {
	scopes := []string{"openid", "email", "profile"}
	return &oauth2.Config{
		ClientID: os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL: os.Getenv("GOOGLE_REDIRECT_URL"),
		Scopes: scopes,
		Endpoint: google.Endpoint,
	}, nil
} 