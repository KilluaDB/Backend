package mocks

import "context"

// GoogleUserInfo is a test double for service.GoogleUserInfoClient.
type GoogleUserInfo struct {
	Email    string
	Verified bool
	Err      error
}

func (g GoogleUserInfo) FetchUserInfo(ctx context.Context, accessToken string) (string, bool, error) {
	if g.Err != nil {
		return "", false, g.Err
	}
	return g.Email, g.Verified, nil
}
