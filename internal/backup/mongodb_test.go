package backup

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExportMongo_success(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		require.Equal(t, "mongodump", name)
		return exec.CommandContext(ctx, "/bin/sh", "-c", `printf '\x1f\x8b\x08'`)
	})

	var buf bytes.Buffer
	err := exportMongo(context.Background(), "mongodb://localhost:27017", &buf)
	require.NoError(t, err)
	assert.NotEmpty(t, buf.Bytes())
}

func TestExportMongo_noOutput(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/sh", "-c", "exit 1")
	})

	err := exportMongo(context.Background(), "mongodb://localhost:27017", &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mongodump")
}

func TestExportMongo_copyError(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/sh", "-c", `printf 'data'`)
	})

	err := exportMongo(context.Background(), "mongodb://localhost:27017", failingWriter{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "copy mongodump output")
}

func TestImportMongo_success(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		require.Equal(t, "mongorestore", name)
		return exec.CommandContext(ctx, "/bin/cat")
	})

	err := importMongo(context.Background(), "mongodb://localhost:27017", strings.NewReader("archive-bytes"))
	require.NoError(t, err)
}

func TestImportMongo_failure(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/sh", "-c", "exit 3")
	})

	err := importMongo(context.Background(), "mongodb://localhost:27017", strings.NewReader("bad"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mongorestore failed")
}

func TestImportMongo_copyError(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "/bin/cat")
		return cmd
	})

	err := importMongo(context.Background(), "mongodb://localhost:27017", errReader{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "copy import data")
}

type errReader struct{}

func (errReader) Read(p []byte) (int, error) { return 0, io.ErrUnexpectedEOF }
