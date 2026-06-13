package response

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"backend/internal/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSuccess(t *testing.T) {
	c, w := testutil.NewGinContext(http.MethodGet, "/", nil, nil)
	Success(c, http.StatusOK, map[string]string{"k": "v"}, "ok")
	assert.Equal(t, http.StatusOK, w.Code)

	var resp APIResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "success", resp.Status)
	assert.Equal(t, "ok", resp.Message)
}

func TestFail(t *testing.T) {
	c, w := testutil.NewGinContext(http.MethodGet, "/", nil, nil)
	Fail(c, http.StatusBadRequest, errors.New("detail"), "bad")
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp APIResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "error", resp.Status)
	assert.Equal(t, "bad", resp.Message)
}

func TestJSON(t *testing.T) {
	c, w := testutil.NewGinContext(http.MethodGet, "/", nil, nil)
	JSON(c, http.StatusCreated, "success", map[string]int{"n": 1}, "created", nil)
	assert.Equal(t, http.StatusCreated, w.Code)
}
