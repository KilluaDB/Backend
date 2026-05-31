package backup

import (
	"backend/internal/responses"
	"backend/internal/services"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// importMaxDuration bounds a single import. Backups much larger than this should
// stage the file and run the restore out-of-band; we don't support that here.
const importMaxDuration = 30 * time.Minute

// Handler is the Gin handler for /api/v1/projects/:id/{export,import}.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Export streams a full database backup for the project. The Postgres `format`
// query param selects plain SQL (default) or pg_dump custom format. MongoDB
// always emits a gzipped mongodump archive.
func (h *Handler) Export(c *gin.Context) {
	userID, ok := userUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectID, err := projectUUID(c)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid projectId format")
		return
	}

	opts := ExportOptions{PostgresFormat: c.Query("format")}

	meta, err := h.svc.PrepareExport(c.Request.Context(), userID, projectID, opts)
	if err != nil {
		writeServiceError(c, err, "Failed to prepare export")
		return
	}

	projectName := h.svc.ProjectName(c.Request.Context(), userID, projectID)
	filename := buildFilename(projectName, meta.FilenameExt)

	c.Status(http.StatusOK)
	c.Header("Content-Type", meta.ContentType)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filename))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "no-store")

	if err := h.svc.Export(c.Request.Context(), userID, projectID, opts, c.Writer); err != nil {
		// Response has already been started (status 200 + headers). The client
		// sees a truncated download. Record the error for the logger so the
		// operator can diagnose.
		_ = c.Error(err)
	}
}

// Import accepts a backup file (multipart `file` field or raw body) and restores
// it into the project's database. The Postgres `format` query param can be used
// to force pg_restore (custom) or psql (sql); empty triggers auto-detect by
// magic bytes.
func (h *Handler) Import(c *gin.Context) {
	userID, ok := userUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectID, err := projectUUID(c)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid projectId format")
		return
	}

	// Imports can take a while; extend the read deadline so the http.Server's
	// ReadTimeout doesn't cut us off mid-upload. CloseNotify on context cancel
	// remains in effect, so a client disconnect still aborts the work.
	if rc := http.NewResponseController(c.Writer); rc != nil {
		_ = rc.SetReadDeadline(time.Now().Add(importMaxDuration))
		_ = rc.SetWriteDeadline(time.Now().Add(importMaxDuration))
	}

	body, closeBody, err := importBodyReader(c.Request)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Failed to read upload body")
		return
	}
	defer closeBody()

	opts := ImportOptions{PostgresFormat: c.Query("format")}

	if err := h.svc.Import(c.Request.Context(), userID, projectID, opts, body); err != nil {
		writeServiceError(c, err, "Import failed")
		return
	}

	responses.Success(c, http.StatusOK, nil, "Import completed successfully")
}

// importBodyReader returns a streaming reader for the import body, supporting
// both multipart/form-data (field "file") and raw bodies.
func importBodyReader(r *http.Request) (io.Reader, func(), error) {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))

	if strings.HasPrefix(mediaType, "multipart/") {
		// Use a streaming MultipartReader rather than ParseMultipartForm so we
		// don't buffer the whole upload to memory or a temp file.
		mr, err := r.MultipartReader()
		if err != nil {
			return nil, func() {}, fmt.Errorf("parse multipart: %w", err)
		}
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				return nil, func() {}, errors.New("multipart upload missing 'file' field")
			}
			if err != nil {
				return nil, func() {}, fmt.Errorf("read multipart part: %w", err)
			}
			if part.FormName() == "file" {
				return part, func() { _ = part.Close() }, nil
			}
			_ = part.Close()
		}
	}

	// Raw body (application/octet-stream, application/sql, etc.)
	return r.Body, func() {}, nil
}

// writeServiceError maps service-layer errors to HTTP status codes.
func writeServiceError(c *gin.Context, err error, fallback string) {
	switch {
	case errors.Is(err, services.ErrProjectNotAccessible):
		responses.Fail(c, http.StatusNotFound, err, "Project not found or access denied")
	case errors.Is(err, services.ErrNoRunningInstance):
		responses.Fail(c, http.StatusConflict, err, "Database instance is not running")
	case errors.Is(err, ErrUnsupportedDBType):
		responses.Fail(c, http.StatusBadRequest, err, "Project database type does not support backup/restore")
	default:
		// Surface tool-level errors as 500 with a sanitized message.
		responses.JSON(c, http.StatusInternalServerError, "error", nil, fallback, err)
	}
}

func userUUID(c *gin.Context) (uuid.UUID, bool) {
	raw, exists := c.Get("userId")
	if !exists {
		return uuid.Nil, false
	}
	switch v := raw.(type) {
	case uuid.UUID:
		return v, true
	case string:
		u, err := uuid.Parse(v)
		if err != nil {
			return uuid.Nil, false
		}
		return u, true
	default:
		return uuid.Nil, false
	}
}

func projectUUID(c *gin.Context) (uuid.UUID, error) {
	id := c.Param("id")
	if id == "" {
		return uuid.Nil, errors.New("project id is required")
	}
	return uuid.Parse(id)
}

var filenameUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// buildFilename produces an attachment filename safe for Content-Disposition.
// Format: <sanitized-project-name>-<YYYYMMDD-HHMMSS>.<ext>
func buildFilename(projectName, ext string) string {
	safe := filenameUnsafe.ReplaceAllString(projectName, "_")
	safe = strings.Trim(safe, "._-")
	if safe == "" {
		safe = "backup"
	}
	ts := time.Now().UTC().Format("20060102-150405")
	return fmt.Sprintf("%s-%s.%s", safe, ts, ext)
}
