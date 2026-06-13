package backup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"backend/internal/mocks"
	"backend/internal/service"
	"backend/internal/testutil"
	"backend/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_ExportUnauthorized(t *testing.T) {
	projects := mocks.NewProjectStore()
	svc := NewService(projects, stubDSN{})
	h := NewHandler(svc)
	r := gin.New()
	r.GET("/projects/:id/export", h.Export)

	c, w := testutil.NewGinContext(http.MethodGet, "/projects/"+uuid.New().String()+"/export", nil, nil)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	r.HandleContext(c)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_ExportPrepareError(t *testing.T) {
	projects := mocks.NewProjectStore()
	svc := NewService(projects, stubDSN{})
	h := NewHandler(svc)
	userID := uuid.New()
	r := gin.New()
	r.GET("/projects/:id/export", func(c *gin.Context) {
		c.Set(utils.UserIDContextKey, userID)
		h.Export(c)
	})

	pid := uuid.New().String()
	c, w := testutil.NewGinContext(http.MethodGet, "/projects/"+pid+"/export", nil, nil)
	c.Params = gin.Params{{Key: "id", Value: pid}}
	r.HandleContext(c)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandler_ExportSuccess(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/sh", "-c", `printf 'SELECT 1;\n'`)
	})

	projects := mocks.NewProjectStore()
	userID := uuid.New()
	proj := projects.SeedProject(userID, "postgresql")
	svc := NewService(projects, stubDSN{})
	h := NewHandler(svc)

	r := gin.New()
	r.GET("/projects/:id/export", func(c *gin.Context) {
		c.Set(utils.UserIDContextKey, userID)
		h.Export(c)
	})

	pid := proj.ID.String()
	c, w := testutil.NewGinContext(http.MethodGet, "/projects/"+pid+"/export", nil, nil)
	c.Params = gin.Params{{Key: "id", Value: pid}}
	r.HandleContext(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
	assert.Equal(t, "application/sql", w.Header().Get("Content-Type"))
}

func TestHandler_ExportMongoSuccess(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/sh", "-c", `printf 'mongo-archive'`)
	})

	projects := mocks.NewProjectStore()
	userID := uuid.New()
	proj := projects.SeedProject(userID, "mongodb")
	svc := NewService(projects, stubDSN{})
	h := NewHandler(svc)

	r := gin.New()
	r.GET("/projects/:id/export", func(c *gin.Context) {
		c.Set(utils.UserIDContextKey, userID)
		h.Export(c)
	})

	pid := proj.ID.String()
	c, w := testutil.NewGinContext(http.MethodGet, "/projects/"+pid+"/export", nil, nil)
	c.Params = gin.Params{{Key: "id", Value: pid}}
	r.HandleContext(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
	assert.Equal(t, "application/gzip", w.Header().Get("Content-Type"))
}

func TestHandler_ExportCustomFormat(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/sh", "-c", `printf 'PGDMP\x01\x02\x03'`)
	})

	projects := mocks.NewProjectStore()
	userID := uuid.New()
	proj := projects.SeedProject(userID, "postgresql")
	svc := NewService(projects, stubDSN{})
	h := NewHandler(svc)

	r := gin.New()
	r.GET("/projects/:id/export", func(c *gin.Context) {
		c.Set(utils.UserIDContextKey, userID)
		h.Export(c)
	})

	pid := proj.ID.String()
	c, w := testutil.NewGinContext(http.MethodGet, "/projects/"+pid+"/export?format=custom", nil, nil)
	c.Params = gin.Params{{Key: "id", Value: pid}}
	r.HandleContext(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Disposition"), ".dump")
	assert.Equal(t, "application/octet-stream", w.Header().Get("Content-Type"))
}

// When the export stream fails after headers are written, the handler records
// the error via c.Error rather than rewriting the (already 200) status.
func TestHandler_Export_streamError(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/sh", "-c", "exit 1")
	})

	projects := mocks.NewProjectStore()
	userID := uuid.New()
	proj := projects.SeedProject(userID, "postgresql")
	svc := NewService(projects, stubDSN{})
	h := NewHandler(svc)

	var captured *gin.Context
	r := gin.New()
	r.GET("/projects/:id/export", func(c *gin.Context) {
		c.Set(utils.UserIDContextKey, userID)
		h.Export(c)
		captured = c
	})

	pid := proj.ID.String()
	c, w := testutil.NewGinContext(http.MethodGet, "/projects/"+pid+"/export", nil, nil)
	c.Params = gin.Params{{Key: "id", Value: pid}}
	r.HandleContext(c)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, captured)
	assert.NotEmpty(t, captured.Errors)
}

func TestHandler_Export_unsupportedDBType(t *testing.T) {
	projects := mocks.NewProjectStore()
	userID := uuid.New()
	proj := projects.SeedProject(userID, "redis")
	svc := NewService(projects, stubDSN{})
	h := NewHandler(svc)

	r := gin.New()
	r.GET("/projects/:id/export", func(c *gin.Context) {
		c.Set(utils.UserIDContextKey, userID)
		h.Export(c)
	})

	pid := proj.ID.String()
	c, w := testutil.NewGinContext(http.MethodGet, "/projects/"+pid+"/export", nil, nil)
	c.Params = gin.Params{{Key: "id", Value: pid}}
	r.HandleContext(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Export_invalidPostgresFormat(t *testing.T) {
	projects := mocks.NewProjectStore()
	userID := uuid.New()
	proj := projects.SeedProject(userID, "postgresql")
	svc := NewService(projects, stubDSN{})
	h := NewHandler(svc)

	r := gin.New()
	r.GET("/projects/:id/export", func(c *gin.Context) {
		c.Set(utils.UserIDContextKey, userID)
		h.Export(c)
	})

	pid := proj.ID.String()
	c, w := testutil.NewGinContext(http.MethodGet, "/projects/"+pid+"/export?format=bad", nil, nil)
	c.Params = gin.Params{{Key: "id", Value: pid}}
	r.HandleContext(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestBuildFilename(t *testing.T) {
	sqlName := buildFilename("My Project!", "sql")
	assert.Contains(t, sqlName, "My_Project")
	assert.True(t, strings.HasSuffix(sqlName, ".sql"))

	dumpName := buildFilename("  spaced  ", "dump")
	assert.Contains(t, dumpName, "spaced")
	assert.True(t, strings.HasSuffix(dumpName, ".dump"))

	assert.Contains(t, buildFilename("", "sql"), "backup-")
}

func TestImportBodyReader_rawBody(t *testing.T) {
	c, _ := testutil.NewGinContext(http.MethodPost, "/import", "SELECT 1;", nil)
	reader, closeFn, err := importBodyReader(c.Request)
	require.NoError(t, err)
	defer closeFn()
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Contains(t, string(data), "SELECT 1")
}

func TestHandler_ImportUnauthorized(t *testing.T) {
	svc := NewService(mocks.NewProjectStore(), stubDSN{})
	h := NewHandler(svc)
	c, w := testutil.NewGinContext(http.MethodPost, "/import", nil, nil)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	h.Import(c)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_ImportInvalidProjectID(t *testing.T) {
	svc := NewService(mocks.NewProjectStore(), stubDSN{})
	h := NewHandler(svc)
	c, w := testutil.NewGinContext(http.MethodPost, "/import", nil, nil)
	c.Set(utils.UserIDContextKey, uuid.New())
	c.Params = gin.Params{{Key: "id", Value: "not-a-uuid"}}
	h.Import(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Import_rawBody(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/cat")
	})

	projects := mocks.NewProjectStore()
	userID := uuid.New()
	proj := projects.SeedProject(userID, "postgresql")
	svc := NewService(projects, stubDSN{})
	h := NewHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/import", strings.NewReader("SELECT 1;"))
	req.Header.Set("Content-Type", "application/sql")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set(utils.UserIDContextKey, userID)
	c.Params = gin.Params{{Key: "id", Value: proj.ID.String()}}

	h.Import(c)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandler_Import_multipartWithFile(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/cat")
	})

	projects := mocks.NewProjectStore()
	userID := uuid.New()
	proj := projects.SeedProject(userID, "postgresql")
	svc := NewService(projects, stubDSN{})
	h := NewHandler(svc)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "dump.sql")
	fw.Write([]byte("SELECT 1;"))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/import", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set(utils.UserIDContextKey, userID)
	c.Params = gin.Params{{Key: "id", Value: proj.ID.String()}}

	h.Import(c)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandler_Import_multipartMissingFile(t *testing.T) {
	projects := mocks.NewProjectStore()
	svc := NewService(projects, stubDSN{})
	h := NewHandler(svc)
	userID := uuid.New()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("notfile", "SELECT 1;")
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/import", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set(utils.UserIDContextKey, userID)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}

	h.Import(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Import_unsupportedDBType(t *testing.T) {
	projects := mocks.NewProjectStore()
	userID := uuid.New()
	proj := projects.SeedProject(userID, "redis")
	svc := NewService(projects, stubDSN{})
	h := NewHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/import", strings.NewReader("SELECT 1;"))
	req.Header.Set("Content-Type", "application/sql")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set(utils.UserIDContextKey, userID)
	c.Params = gin.Params{{Key: "id", Value: proj.ID.String()}}

	h.Import(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Import_projectNotFound(t *testing.T) {
	svc := NewService(mocks.NewProjectStore(), stubDSN{})
	h := NewHandler(svc)
	userID := uuid.New()

	req := httptest.NewRequest(http.MethodPost, "/import", strings.NewReader("SELECT 1;"))
	req.Header.Set("Content-Type", "application/sql")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set(utils.UserIDContextKey, userID)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}

	h.Import(c)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandler_Import_instanceNotRunning(t *testing.T) {
	projects := mocks.NewProjectStore()
	userID := uuid.New()
	proj := projects.SeedProject(userID, "postgresql")
	svc := NewService(projects, stubDSN{err: service.ErrNoRunningInstance})
	h := NewHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/import", strings.NewReader("SELECT 1;"))
	req.Header.Set("Content-Type", "application/sql")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set(utils.UserIDContextKey, userID)
	c.Params = gin.Params{{Key: "id", Value: proj.ID.String()}}

	h.Import(c)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestHandler_Import_invalidFormat(t *testing.T) {
	projects := mocks.NewProjectStore()
	userID := uuid.New()
	proj := projects.SeedProject(userID, "postgresql")
	svc := NewService(projects, stubDSN{})
	h := NewHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/import?format=bad", strings.NewReader("SELECT 1;"))
	req.Header.Set("Content-Type", "application/sql")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set(utils.UserIDContextKey, userID)
	c.Params = gin.Params{{Key: "id", Value: proj.ID.String()}}

	h.Import(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestWriteServiceError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"project not accessible", service.ErrProjectNotAccessible, http.StatusNotFound},
		{"no running instance", service.ErrNoRunningInstance, http.StatusConflict},
		{"unsupported db type", ErrUnsupportedDBType, http.StatusBadRequest},
		{"invalid postgres format", ErrInvalidPostgresFormat, http.StatusBadRequest},
		{"generic error", errors.New("some error"), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			writeServiceError(c, tt.err, "fallback")
			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}
