package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"backend/internal/mocks"
	"backend/internal/service"
	"backend/internal/testutil"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"golang.org/x/oauth2"
)

func TestGoogleAuthHandler_Login(t *testing.T) {
	testutil.SetupJWTSecrets(t)
	users := mocks.NewUserStore()
	svc := service.NewGoogleAuthServiceWithClient(users, stubGoogleClient{email: "oauth@test.com", verified: true})
	cfg := &oauth2.Config{
		ClientID:     "id",
		ClientSecret: "secret",
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://auth.example/oauth",
			TokenURL: "https://token.example/oauth",
		},
		RedirectURL: "http://localhost/callback",
	}
	h := NewGoogleAuthHandler(svc, cfg)

	t.Run("success", func(t *testing.T) {
		c, w := testutil.NewGinContext(http.MethodGet, "/login", nil, nil)
		h.Login(c)
		assert.Equal(t, http.StatusTemporaryRedirect, w.Code)
		assert.Contains(t, w.Header().Get("Location"), "https://auth.example/oauth")
		setCookie := w.Header().Get("Set-Cookie")
		assert.Contains(t, setCookie, "oauth_state=")
		assert.Contains(t, setCookie, "HttpOnly")
	})

	t.Run("state generation", func(t *testing.T) {
		c1, w1 := testutil.NewGinContext(http.MethodGet, "/login", nil, nil)
		h.Login(c1)
		c1val := cookieValue(w1.Header().Get("Set-Cookie"), "oauth_state")

		c2, w2 := testutil.NewGinContext(http.MethodGet, "/login", nil, nil)
		h.Login(c2)
		c2val := cookieValue(w2.Header().Get("Set-Cookie"), "oauth_state")

		assert.NotEmpty(t, c1val)
		assert.NotEmpty(t, c2val)
		assert.NotEqual(t, c1val, c2val)
	})
}

func cookieValue(setCookie, name string) string {
	for _, c := range (&http.Response{Header: http.Header{"Set-Cookie": {setCookie}}}).Cookies() {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func TestGoogleAuthHandler_Callback(t *testing.T) {
	testutil.SetupJWTSecrets(t)
	users := mocks.NewUserStore()
	svc := service.NewGoogleAuthServiceWithClient(users, stubGoogleClient{email: "oauth@test.com", verified: true})
	cfg := &oauth2.Config{
		ClientID:     "id",
		ClientSecret: "secret",
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://auth.example/oauth",
			TokenURL: "https://token.example/oauth",
		},
		RedirectURL: "http://localhost/callback",
	}
	h := NewGoogleAuthHandler(svc, cfg)

	r := gin.New()
	r.GET("/callback", h.Callback)

	t.Run("missing state", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/callback?code=abc", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("state mismatch", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/callback?state=a&code=abc", nil)
		req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "b"})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

type stubGoogleClient struct {
	email    string
	verified bool
}

func (s stubGoogleClient) FetchUserInfo(ctx context.Context, accessToken string) (string, bool, error) {
	return s.email, s.verified, nil
}
