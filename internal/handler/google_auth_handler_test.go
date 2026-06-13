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

	t.Run("missing state cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/callback?state=abc&code=def", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing code", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/callback?state=abc", nil)
		req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "abc"})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	// doCallback issues a callback request with matching state cookie + query param.
	doCallback := func(router *gin.Engine, state, code string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/callback?state="+state+"&code="+code, nil)
		req.AddCookie(&http.Cookie{Name: "oauth_state", Value: state})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	// callbackRouterWithToken builds a handler whose OAuth token endpoint is a local
	// httptest server, so the code->token Exchange is deterministic and offline.
	callbackRouterWithToken := func(authSvc *service.GoogleAuthService, tokenURL string) *gin.Engine {
		cfg := &oauth2.Config{
			ClientID:     "id",
			ClientSecret: "secret",
			Endpoint:     oauth2.Endpoint{AuthURL: "https://auth.example/oauth", TokenURL: tokenURL},
			RedirectURL:  "http://localhost/callback",
		}
		router := gin.New()
		router.GET("/callback", NewGoogleAuthHandler(authSvc, cfg).Callback)
		return router
	}

	t.Run("code exchange fails", func(t *testing.T) {
		tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer tokenSrv.Close()

		router := callbackRouterWithToken(svc, tokenSrv.URL)
		w := doCallback(router, "xyz", "abc")
		// Handler returns 500 when cfg.Exchange fails.
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("service callback error", func(t *testing.T) {
		tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"ya29.token","token_type":"Bearer","expires_in":3600}`))
		}))
		defer tokenSrv.Close()

		// Exchange succeeds, but the service fails because Google reports the email unverified.
		failSvc := service.NewGoogleAuthServiceWithClient(mocks.NewUserStore(), stubGoogleClient{email: "oauth@test.com", verified: false})
		router := callbackRouterWithToken(failSvc, tokenSrv.URL)
		w := doCallback(router, "xyz", "abc")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("success returns access token", func(t *testing.T) {
		tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"ya29.token","token_type":"Bearer","expires_in":3600}`))
		}))
		defer tokenSrv.Close()

		okSvc := service.NewGoogleAuthServiceWithClient(mocks.NewUserStore(), stubGoogleClient{email: "oauth@test.com", verified: true})
		router := callbackRouterWithToken(okSvc, tokenSrv.URL)
		w := doCallback(router, "xyz", "abc")
		// Handler returns 200 with the issued access token in the body (not a redirect).
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "access_token")
	})
}

type stubGoogleClient struct {
	email    string
	verified bool
}

func (s stubGoogleClient) FetchUserInfo(ctx context.Context, accessToken string) (string, bool, error) {
	return s.email, s.verified, nil
}
