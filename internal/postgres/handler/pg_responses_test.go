package handler

import (
	"errors"
	"net/http"
	"testing"

	"backend/internal/response"
	"backend/internal/testutil"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPgFail(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		c, w := testutil.NewGinContext(http.MethodGet, "/", nil, nil)
		pgFail(c, http.StatusNotFound, nil, "not found")
		assert.Equal(t, http.StatusNotFound, w.Code)

		var resp response.APIResponse
		require.NoError(t, testutil.ParseJSONResponse(w, &resp))
		assert.Equal(t, "error", resp.Status)
		assert.Equal(t, "not found", resp.Message)
		assert.Empty(t, resp.Error)
		assert.Empty(t, resp.Code)
	})

	t.Run("with error", func(t *testing.T) {
		c, w := testutil.NewGinContext(http.MethodGet, "/", nil, nil)
		pgFail(c, http.StatusInternalServerError, errors.New("disk full"), "internal error")
		assert.Equal(t, http.StatusInternalServerError, w.Code)

		var resp response.APIResponse
		require.NoError(t, testutil.ParseJSONResponse(w, &resp))
		assert.Equal(t, "error", resp.Status)
		assert.Contains(t, resp.Error, "disk full")
		assert.Empty(t, resp.Code)
	})

	t.Run("with PgError", func(t *testing.T) {
		c, w := testutil.NewGinContext(http.MethodGet, "/", nil, nil)
		pgErr := &pgconn.PgError{Code: "23505", Message: "duplicate key"}
		pgFail(c, http.StatusConflict, pgErr, "conflict")
		assert.Equal(t, http.StatusConflict, w.Code)

		var resp response.APIResponse
		require.NoError(t, testutil.ParseJSONResponse(w, &resp))
		assert.Equal(t, "error", resp.Status)
		assert.Contains(t, resp.Error, "duplicate key")
		assert.Equal(t, "23505", resp.Code)
	})
}

func TestPgFailWithData(t *testing.T) {
	t.Run("with error and data", func(t *testing.T) {
		c, w := testutil.NewGinContext(http.MethodGet, "/", nil, nil)
		data := map[string]any{"partial": "results"}
		pgFailWithData(c, http.StatusOK, errors.New("some rows failed"), "partial failure", data)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp response.APIResponse
		require.NoError(t, testutil.ParseJSONResponse(w, &resp))
		assert.Equal(t, "error", resp.Status)
		assert.Contains(t, resp.Error, "some rows failed")
		require.NotNil(t, resp.Data)
		d := resp.Data.(map[string]any)
		assert.Equal(t, "results", d["partial"])
	})

	t.Run("nil error", func(t *testing.T) {
		c, w := testutil.NewGinContext(http.MethodGet, "/", nil, nil)
		pgFailWithData(c, http.StatusBadRequest, nil, "bad request", "just-data")
		assert.Equal(t, http.StatusBadRequest, w.Code)

		var resp response.APIResponse
		require.NoError(t, testutil.ParseJSONResponse(w, &resp))
		assert.Equal(t, "error", resp.Status)
		assert.Empty(t, resp.Error)
		assert.Equal(t, "just-data", resp.Data)
	})
}

func TestRedactPrivateIPs(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "10.x.x.x redacted",
			input: "10.0.0.5",
			want:  "[REDACTED_IP]",
		},
		{
			name:  "192.168.x.x redacted",
			input: "192.168.1.1",
			want:  "[REDACTED_IP]",
		},
		{
			name:  "172.16.x.x redacted",
			input: "172.16.0.1",
			want:  "[REDACTED_IP]",
		},
		{
			name:  "172.30.x.x redacted",
			input: "172.30.0.1",
			want:  "[REDACTED_IP]",
		},
		{
			name:  "172.20.x.x redacted via catch-all 172.2",
			input: "172.20.0.1",
			want:  "[REDACTED_IP]",
		},
		{
			name:  "public IP unchanged",
			input: "8.8.8.8",
			want:  "8.8.8.8",
		},
		{
			name:  "localhost unchanged",
			input: "localhost",
			want:  "localhost",
		},
		{
			name:  "private IP in token with punctuation",
			input: "(10.0.0.1)",
			want:  "([REDACTED_IP])",
		},
		{
			name:  "non-lead private IP unchanged",
			input: "host=10.0.0.5",
			want:  "host=10.0.0.5",
		},
		{
			name:  "multiple IPs in one line",
			input: "10.0.0.1 192.168.1.1 8.8.8.8",
			want:  "[REDACTED_IP] [REDACTED_IP] 8.8.8.8",
		},
		{
			name:  "IP with port separator",
			input: "10.0.0.1:5432",
			want:  "[REDACTED_IP]",
		},
		{
			name:  "quoted private IP",
			input: `"10.0.0.1"`,
			want:  `"[REDACTED_IP]"`,
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactPrivateIPs(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSanitizePgErrorString(t *testing.T) {
	t.Run("empty string", func(t *testing.T) {
		assert.Equal(t, "", sanitizePgErrorString(""))
	})

	t.Run("redacts password kv", func(t *testing.T) {
		in := "password=mysecret port=5432"
		out := sanitizePgErrorString(in)
		assert.Contains(t, out, "password=[REDACTED]")
		assert.NotContains(t, out, "mysecret")
	})

	t.Run("redacts user kv", func(t *testing.T) {
		in := "user=admin"
		out := sanitizePgErrorString(in)
		assert.Contains(t, out, "user=[REDACTED]")
		assert.NotContains(t, out, "admin")
	})

	t.Run("redacts host kv", func(t *testing.T) {
		in := "host=10.0.0.1"
		out := sanitizePgErrorString(in)
		assert.Contains(t, out, "host=[REDACTED]")
	})

	t.Run("redacts port kv", func(t *testing.T) {
		in := "port=5432"
		out := sanitizePgErrorString(in)
		assert.Contains(t, out, "port=[REDACTED]")
	})

	t.Run("redacts dbname kv", func(t *testing.T) {
		in := "dbname=mydb"
		out := sanitizePgErrorString(in)
		assert.Contains(t, out, "dbname=[REDACTED]")
		assert.NotContains(t, out, "mydb")
	})

	t.Run("redacts database kv", func(t *testing.T) {
		in := "database=mydb"
		out := sanitizePgErrorString(in)
		assert.Contains(t, out, "database=[REDACTED]")
		assert.NotContains(t, out, "mydb")
	})

	t.Run("redacts pwd alias", func(t *testing.T) {
		in := "pwd=secret"
		out := sanitizePgErrorString(in)
		assert.Contains(t, out, "pwd=[REDACTED]")
		assert.NotContains(t, out, "secret")
	})

	t.Run("redacts passwd alias", func(t *testing.T) {
		in := "passwd=secret"
		out := sanitizePgErrorString(in)
		assert.Contains(t, out, "passwd=[REDACTED]")
		assert.NotContains(t, out, "secret")
	})

	t.Run("redacts URL credentials", func(t *testing.T) {
		in := "postgres://user:pass@host:5432/db"
		out := sanitizePgErrorString(in)
		assert.Contains(t, out, "[REDACTED]:[REDACTED]")
		assert.NotContains(t, out, "user")
		assert.NotContains(t, out, "pass")
	})
}
