package backup

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func succeedCmd(ctx context.Context) *exec.Cmd {
	return exec.CommandContext(ctx, "/bin/sh", "-c", "exit 0")
}

func swapCommandContext(t *testing.T, fn func(ctx context.Context, name string, args ...string) *exec.Cmd) {
	t.Helper()
	prev := commandContext
	commandContext = fn
	t.Cleanup(func() { commandContext = prev })
}

func TestParsePostgresFormat(t *testing.T) {
	tests := []struct {
		in      string
		want    PostgresFormat
		wantErr bool
	}{
		{"", PgFormatSQL, false},
		{"sql", PgFormatSQL, false},
		{"plain", PgFormatSQL, false},
		{"p", PgFormatSQL, false},
		{"custom", PgFormatCustom, false},
		{"c", PgFormatCustom, false},
		{"dump", PgFormatCustom, false},
		{"invalid", "", true},
	}
	for _, tt := range tests {
		got, err := parsePostgresFormat(tt.in)
		if tt.wantErr {
			assert.Error(t, err)
		} else {
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		}
	}
}

func TestIsCopyTerminator(t *testing.T) {
	assert.True(t, isCopyTerminator([]byte(`\.`+"\n")))
	assert.False(t, isCopyTerminator([]byte("COPY public.t FROM stdin;\n")))
}

func TestCopyPlainSQLFilteringExtensions(t *testing.T) {
	in := strings.Join([]string{
		"DROP EXTENSION IF EXISTS pg_stat_statements;",
		"COMMENT ON EXTENSION pg_stat_statements IS 'stats';",
		"CREATE TABLE public.t (id int);",
		"COPY public.t (id) FROM stdin;",
		"1",
		`\.`,
		"SELECT 1;",
	}, "\n") + "\n"

	var out bytes.Buffer
	err := copyPlainSQLFilteringExtensions(&out, bufio.NewReader(strings.NewReader(in)))
	require.NoError(t, err)
	got := out.String()
	assert.NotContains(t, got, "DROP EXTENSION")
	assert.NotContains(t, got, "COMMENT ON EXTENSION")
	assert.Contains(t, got, "CREATE TABLE public.t")
	assert.Contains(t, got, "COPY public.t")
	assert.Contains(t, got, "1\n\\.\n")
}

func TestWriteFilteredTOC(t *testing.T) {
	toc := []byte(`; Archive created
123; 1259 16384 TABLE public users
456; 3079 16384 EXTENSION - pg_stat_statements
789; 0 0 COMMENT - EXTENSION pg_stat_statements
`)
	path, err := writeFilteredTOC(toc)
	require.NoError(t, err)
	defer func() { _ = os.Remove(path) }()

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	body := string(data)
	assert.Contains(t, body, "TABLE public users")
	assert.NotContains(t, body, " EXTENSION ")
}

func TestExportPostgres_plainSQL_success(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		require.Equal(t, "pg_dump", name)
		return exec.CommandContext(ctx, "/bin/sh", "-c", `printf 'CREATE TABLE t (id int);\n'`)
	})

	var buf bytes.Buffer
	err := exportPostgres(context.Background(), "postgresql://u:p@localhost/db", PgFormatSQL, &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "CREATE TABLE t")
}

func TestExportPostgres_custom_success(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/sh", "-c", `printf 'PGDMP\x01\x02\x03'`)
	})

	var buf bytes.Buffer
	err := exportPostgres(context.Background(), "postgresql://u:p@localhost/db", PgFormatCustom, &buf)
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(buf.Bytes(), pgCustomMagic))
}

func TestExportPostgres_noOutput(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/sh", "-c", "exit 1")
	})

	err := exportPostgres(context.Background(), "postgresql://u:p@localhost/db", PgFormatSQL, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pg_dump")
}

func TestExportPostgres_copyError(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/sh", "-c", `printf 'CREATE TABLE t;\n'; sleep 0.1`)
	})

	err := exportPostgres(context.Background(), "postgresql://u:p@localhost/db", PgFormatSQL, failingWriter{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "copy pg_dump output")
}

type failingWriter struct{}

func (failingWriter) Write(p []byte) (int, error) { return 0, io.ErrClosedPipe }

func TestImportPostgresSQL_success(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		require.Equal(t, "psql", name)
		return exec.CommandContext(ctx, "/bin/cat")
	})

	err := importPostgresSQL(context.Background(), "postgresql://u:p@localhost/db", strings.NewReader("SELECT 1;\n"))
	require.NoError(t, err)
}

func TestImportPostgresSQL_failure(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/sh", "-c", "exit 2")
	})

	err := importPostgresSQL(context.Background(), "postgresql://u:p@localhost/db", strings.NewReader("BAD SQL;"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "postgres restore failed")
}

func TestImportPostgres_autoDetectSQL(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		require.Equal(t, "psql", name)
		return exec.CommandContext(ctx, "/bin/cat")
	})

	err := importPostgres(context.Background(), "postgresql://u:p@localhost/db", "", strings.NewReader("CREATE TABLE t (id int);"))
	require.NoError(t, err)
}

func TestImportPostgres_customFormat(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		if name != "pg_restore" {
			t.Fatalf("unexpected command %q", name)
		}
		for _, a := range args {
			if a == "--list" {
				toc := "; Archive created\n123; 1259 TABLE public t\n456; 3079 EXTENSION - pg_stat_statements\n"
				return exec.CommandContext(ctx, "/bin/sh", "-c", "printf '"+toc+"'")
			}
		}
		return succeedCmd(ctx)
	})

	payload := append(append([]byte(nil), pgCustomMagic...), []byte("rest-of-archive")...)
	err := importPostgres(context.Background(), "postgresql://u:p@localhost/db", "", bytes.NewReader(payload))
	require.NoError(t, err)
}

func TestImportPostgres_forcedCustom(t *testing.T) {
	swapCommandContext(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		if name == "pg_restore" {
			for _, a := range args {
				if a == "--list" {
					return exec.CommandContext(ctx, "/bin/echo", "; header")
				}
			}
			return succeedCmd(ctx)
		}
		t.Fatalf("unexpected %q", name)
		return nil
	})

	err := importPostgres(context.Background(), "postgresql://u:p@localhost/db", PgFormatCustom, bytes.NewReader([]byte("not-magic")))
	require.NoError(t, err)
}

func TestTrimStderr(t *testing.T) {
	short := "err"
	assert.Equal(t, short, trimStderr(short))
	long := strings.Repeat("x", 2000)
	out := trimStderr(long)
	assert.True(t, strings.HasSuffix(out, "...(truncated)"))
	assert.LessOrEqual(t, len(out), 1100)
}
