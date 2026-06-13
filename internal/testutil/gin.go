package testutil

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// NewGinContext builds a Gin context backed by httptest for handler/middleware tests.
func NewGinContext(method, path string, body any, headers map[string]string) (*gin.Context, *httptest.ResponseRecorder) {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	return c, w
}

// ParseJSONResponse decodes the recorder body into dest.
func ParseJSONResponse(w *httptest.ResponseRecorder, dest any) error {
	return json.Unmarshal(w.Body.Bytes(), dest)
}
