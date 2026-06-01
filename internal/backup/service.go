// Package backup implements full-database export (backup) and import (restore)
// for a project's provisioned database instance. PostgreSQL projects use
// pg_dump / pg_restore / psql; MongoDB projects use mongodump / mongorestore.
//
// All I/O is streamed: process stdout pipes directly to the HTTP response writer
// (export) and the HTTP request body pipes directly to process stdin (import).
// Nothing is buffered to disk or memory beyond pipe buffers.
package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"backend/internal/repository"
	"backend/internal/service"

	"github.com/google/uuid"
)

// DSNResolver returns a usable connection string for a project's database instance.
// Implemented by service.InstanceDSNService.
type DSNResolver interface {
	GetConnectionDSN(ctx context.Context, userID, projectID uuid.UUID) (dsn string, instanceID uuid.UUID, err error)
}

// Service is the cross-DB backup orchestrator.
type Service struct {
	projectRepo *repository.ProjectRepository
	dsn         DSNResolver
}

func NewService(projectRepo *repository.ProjectRepository, dsn DSNResolver) *Service {
	return &Service{
		projectRepo: projectRepo,
		dsn:         dsn,
	}
}

// DBKind identifies the underlying database engine for routing.
type DBKind string

const (
	DBPostgres DBKind = "postgresql"
	DBMongo    DBKind = "mongodb"
)

// ExportResult is metadata returned alongside a streamed export so the handler
// can set Content-Type / Content-Disposition correctly.
type ExportResult struct {
	Kind        DBKind
	FilenameExt string // e.g. "sql", "dump", "archive.gz"
	ContentType string
}

// ExportOptions controls export behavior. Format is consumed for Postgres only.
type ExportOptions struct {
	PostgresFormat string // "sql" (default) or "custom"
}

// ImportOptions controls import behavior. PostgresFormat optionally forces
// pg_restore vs psql; empty means auto-detect from the input stream.
type ImportOptions struct {
	PostgresFormat string // "", "sql", or "custom"
}

// ErrUnsupportedDBType is returned when project.DBType is neither postgres nor mongodb.
var ErrUnsupportedDBType = errors.New("unsupported database type for backup")

// PrepareExport resolves the project, verifies access, and returns the metadata
// the handler needs to write response headers BEFORE Export streams any bytes.
// It does not start the export — call Export with the same projectID afterward.
func (s *Service) PrepareExport(ctx context.Context, userID, projectID uuid.UUID, opts ExportOptions) (*ExportResult, error) {
	kind, err := s.resolveKind(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	switch kind {
	case DBPostgres:
		format, err := parsePostgresFormat(opts.PostgresFormat)
		if err != nil {
			return nil, err
		}
		ext := "sql"
		ctype := "application/sql"
		if format == PgFormatCustom {
			ext = "dump"
			ctype = "application/octet-stream"
		}
		return &ExportResult{Kind: DBPostgres, FilenameExt: ext, ContentType: ctype}, nil
	case DBMongo:
		return &ExportResult{Kind: DBMongo, FilenameExt: "archive.gz", ContentType: "application/gzip"}, nil
	default:
		return nil, ErrUnsupportedDBType
	}
}

// Export streams a full database backup of the project to dst.
// The handler must have already called PrepareExport and written response headers.
func (s *Service) Export(ctx context.Context, userID, projectID uuid.UUID, opts ExportOptions, dst io.Writer) error {
	kind, err := s.resolveKind(ctx, userID, projectID)
	if err != nil {
		return err
	}
	dsn, _, err := s.dsn.GetConnectionDSN(ctx, userID, projectID)
	if err != nil {
		return err
	}
	switch kind {
	case DBPostgres:
		format, err := parsePostgresFormat(opts.PostgresFormat)
		if err != nil {
			return err
		}
		return exportPostgres(ctx, dsn, format, dst)
	case DBMongo:
		return exportMongo(ctx, dsn, dst)
	default:
		return ErrUnsupportedDBType
	}
}

// Import streams src into the project's database via the appropriate restore tool.
func (s *Service) Import(ctx context.Context, userID, projectID uuid.UUID, opts ImportOptions, src io.Reader) error {
	kind, err := s.resolveKind(ctx, userID, projectID)
	if err != nil {
		return err
	}
	dsn, _, err := s.dsn.GetConnectionDSN(ctx, userID, projectID)
	if err != nil {
		return err
	}
	switch kind {
	case DBPostgres:
		var forced PostgresFormat
		if opts.PostgresFormat != "" {
			f, err := parsePostgresFormat(opts.PostgresFormat)
			if err != nil {
				return err
			}
			forced = f
		}
		return importPostgres(ctx, dsn, forced, src)
	case DBMongo:
		return importMongo(ctx, dsn, src)
	default:
		return ErrUnsupportedDBType
	}
}

// ProjectName fetches the project's user-visible name for filename generation.
// Falls back to the project ID's short form if the project is missing or the
// name is empty.
func (s *Service) ProjectName(ctx context.Context, userID, projectID uuid.UUID) string {
	project, err := s.projectRepo.GetByIDAndUserID(ctx, projectID, userID)
	if err != nil || project == nil {
		return shortID(projectID)
	}
	name := strings.TrimSpace(project.Name)
	if name == "" {
		return shortID(projectID)
	}
	return name
}

func (s *Service) resolveKind(ctx context.Context, userID, projectID uuid.UUID) (DBKind, error) {
	project, err := s.projectRepo.GetByIDAndUserID(ctx, projectID, userID)
	if err != nil {
		return "", err
	}
	if project == nil {
		return "", service.ErrProjectNotAccessible
	}
	switch strings.ToLower(strings.TrimSpace(project.DBType)) {
	case "postgres", "postgresql", "sql":
		return DBPostgres, nil
	case "mongodb", "nosql":
		return DBMongo, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedDBType, project.DBType)
	}
}

func shortID(id uuid.UUID) string {
	s := id.String()
	if len(s) >= 8 {
		return s[:8]
	}
	return s
}
