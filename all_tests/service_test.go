package backup

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"backend/internal/mocks"
	"backend/internal/service"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubDSN struct {
	dsn string
	err error
}

func (s stubDSN) GetConnectionDSN(ctx context.Context, userID, projectID uuid.UUID) (string, uuid.UUID, error) {
	if s.err != nil {
		return "", uuid.Nil, s.err
	}
	dsn := s.dsn
	if dsn == "" {
		dsn = "postgresql://u:p@localhost:5432/app"
	}
	return dsn, uuid.New(), nil
}

func TestService_PrepareExport(t *testing.T) {
	projects := mocks.NewProjectStore()
	userID := uuid.New()
	pg := projects.SeedProject(userID, "postgresql")
	svc := NewService(projects, stubDSN{})

	tests := []struct {
		name      string
		projectID uuid.UUID
		opts      ExportOptions
		wantKind  DBKind
		wantErr   bool
	}{
		{"postgres sql default", pg.ID, ExportOptions{}, DBPostgres, false},
		{"postgres custom", pg.ID, ExportOptions{PostgresFormat: "custom"}, DBPostgres, false},
		{"invalid format", pg.ID, ExportOptions{PostgresFormat: "bad"}, "", true},
		{"project not found", uuid.New(), ExportOptions{}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.PrepareExport(context.Background(), userID, tt.projectID, tt.opts)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantKind, result.Kind)
			if tt.opts.PostgresFormat == "custom" {
				assert.Equal(t, "dump", result.FilenameExt)
			}
		})
	}

	mongo := projects.SeedProject(userID, "mongodb")
	result, err := svc.PrepareExport(context.Background(), userID, mongo.ID, ExportOptions{})
	require.NoError(t, err)
	assert.Equal(t, DBMongo, result.Kind)
}

func TestService_resolveKind(t *testing.T) {
	projects := mocks.NewProjectStore()
	userID := uuid.New()
	svc := NewService(projects, stubDSN{})

	unsupported := projects.SeedProject(userID, "redis")
	_, err := svc.PrepareExport(context.Background(), userID, unsupported.ID, ExportOptions{})
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnsupportedDBType) || errors.Is(err, service.ErrProjectNotAccessible))
}

func TestService_ProjectName(t *testing.T) {
	projects := mocks.NewProjectStore()
	userID := uuid.New()
	svc := NewService(projects, stubDSN{})

	p := projects.SeedProject(userID, "postgresql")
	p.Name = "My Project"
	_ = projects.Update(context.Background(), p)
	assert.Equal(t, "My Project", svc.ProjectName(context.Background(), userID, p.ID))

	p.Name = "   "
	_ = projects.Update(context.Background(), p)
	assert.Equal(t, p.ID.String()[:8], svc.ProjectName(context.Background(), userID, p.ID))

	assert.NotEmpty(t, svc.ProjectName(context.Background(), userID, uuid.New()))
}

func TestService_Export_resolveErrors(t *testing.T) {
	projects := mocks.NewProjectStore()
	userID := uuid.New()
	pg := projects.SeedProject(userID, "postgresql")
	svc := NewService(projects, stubDSN{err: errors.New("dsn failed")})

	err := svc.Export(context.Background(), userID, pg.ID, ExportOptions{}, discardWriter{})
	assert.Error(t, err)
}

func TestService_Export_postgresSuccess(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/sh", "-c", `printf 'SELECT 1;\n'`)
	})

	projects := mocks.NewProjectStore()
	userID := uuid.New()
	pg := projects.SeedProject(userID, "postgresql")
	svc := NewService(projects, stubDSN{})

	var buf bytes.Buffer
	err := svc.Export(context.Background(), userID, pg.ID, ExportOptions{}, &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "SELECT 1")
}

func TestService_Export_mongoSuccess(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/sh", "-c", `printf 'gz'`)
	})

	projects := mocks.NewProjectStore()
	userID := uuid.New()
	mongo := projects.SeedProject(userID, "mongodb")
	svc := NewService(projects, stubDSN{})

	var buf bytes.Buffer
	err := svc.Export(context.Background(), userID, mongo.ID, ExportOptions{}, &buf)
	require.NoError(t, err)
	assert.NotEmpty(t, buf.Bytes())
}

func TestService_Import_postgresAndMongo(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		switch name {
		case "psql", "mongorestore":
			return exec.CommandContext(ctx, "/bin/cat")
		default:
			t.Fatalf("unexpected command %q", name)
			return nil
		}
	})

	projects := mocks.NewProjectStore()
	userID := uuid.New()
	pg := projects.SeedProject(userID, "postgresql")
	mongo := projects.SeedProject(userID, "mongodb")
	svc := NewService(projects, stubDSN{})

	err := svc.Import(context.Background(), userID, pg.ID, ImportOptions{}, strings.NewReader("SELECT 1;"))
	require.NoError(t, err)

	err = svc.Import(context.Background(), userID, mongo.ID, ImportOptions{}, strings.NewReader("archive"))
	require.NoError(t, err)
}

func TestService_Import_invalidFormat(t *testing.T) {
	projects := mocks.NewProjectStore()
	userID := uuid.New()
	pg := projects.SeedProject(userID, "postgresql")
	svc := NewService(projects, stubDSN{})

	err := svc.Import(context.Background(), userID, pg.ID, ImportOptions{PostgresFormat: "bad"}, strings.NewReader("x"))
	assert.Error(t, err)
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func Test_shortID(t *testing.T) {
	id := uuid.MustParse("12345678-1234-1234-1234-123456789abc")
	assert.Equal(t, "12345678", shortID(id))
}
