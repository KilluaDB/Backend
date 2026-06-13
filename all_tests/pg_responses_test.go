package handler

import (
	"errors"
	"net/http"
	"testing"

	"backend/internal/testutil"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizePgErrorString(t *testing.T) {
	raw := "connection failed password=secret user=admin host=10.0.0.5 port=5432"
	out := sanitizePgErrorString(raw)
	assert.NotContains(t, out, "secret")
	assert.Contains(t, out, "[REDACTED]")
}

func TestPgFail(t *testing.T) {
	c, w := testutil.NewGinContext(http.MethodGet, "/", nil, nil)
	pgErr := &pgconn.PgError{Code: "23505", Message: "duplicate key password=secret"}
	pgFail(c, http.StatusConflict, pgErr, "conflict")
	assert.Equal(t, http.StatusConflict, w.Code)

	var resp struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	require.NoError(t, testutil.ParseJSONResponse(w, &resp))
	assert.Equal(t, "23505", resp.Code)
}

func TestPgFailWithData(t *testing.T) {
	c, w := testutil.NewGinContext(http.MethodGet, "/", nil, nil)
	pgFailWithData(c, http.StatusBadRequest, errors.New("bad"), "msg", map[string]int{"rows": 0})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
