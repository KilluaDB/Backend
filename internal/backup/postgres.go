package backup

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// pgCustomMagic is the 5-byte header pg_dump writes at the start of a custom-format dump.
// pg_restore consumes this format; psql does not.
var pgCustomMagic = []byte("PGDMP")

// PostgresFormat selects pg_dump output format.
type PostgresFormat string

const (
	PgFormatSQL    PostgresFormat = "sql"    // plain SQL (pg_dump -Fp); restorable with psql
	PgFormatCustom PostgresFormat = "custom" // pg_dump -Fc binary archive; restorable with pg_restore
)

func parsePostgresFormat(s string) (PostgresFormat, error) {
	switch s {
	case "", "sql", "plain", "p":
		return PgFormatSQL, nil
	case "custom", "c", "dump":
		return PgFormatCustom, nil
	default:
		return "", fmt.Errorf("unsupported postgres format: %q (use sql or custom)", s)
	}
}

// exportPostgres runs pg_dump against the given DSN and streams stdout to dst.
// dst must be safe to write to before the first byte is produced (we capture stderr
// separately so a failed dump won't corrupt dst beyond what's already streamed).
func exportPostgres(ctx context.Context, dsn string, format PostgresFormat, dst io.Writer) error {
	// --clean / --if-exists make the dump idempotent when restored over an
	// existing database (pg_restore --clean handles the equivalent for custom
	// format, but for plain SQL the DROP statements must be in the dump itself).
	args := []string{
		"--no-owner",
		"--no-privileges",
		"--clean",
		"--if-exists",
	}
	switch format {
	case PgFormatCustom:
		args = append(args, "--format=c")
	default: // sql
		args = append(args, "--format=p")
	}
	args = append(args, "--dbname="+dsn)

	cmd := exec.CommandContext(ctx, "pg_dump", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("pg_dump stdout pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start pg_dump: %w", err)
	}

	// Peek the first byte so we can fail before writing to the response.
	reader := bufio.NewReader(stdout)
	if _, err := reader.Peek(1); err != nil {
		// Process produced no data — wait for it to exit and report stderr.
		waitErr := cmd.Wait()
		if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) {
			return fmt.Errorf("pg_dump produced no output (exit: %v): %s", waitErr, trimStderr(stderr.String()))
		}
		return fmt.Errorf("pg_dump read: %w (exit: %v): %s", err, waitErr, trimStderr(stderr.String()))
	}

	var copyErr error
	if format == PgFormatCustom {
		_, copyErr = io.Copy(dst, reader)
	} else {
		// Plain SQL: strip extension statements only a superuser/owner may run.
		copyErr = copyPlainSQLFilteringExtensions(dst, reader)
	}
	if copyErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("copy pg_dump output: %w", copyErr)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("pg_dump exited with error: %w: %s", err, trimStderr(stderr.String()))
	}
	return nil
}

// copyPlainSQLFilteringExtensions streams a plain-SQL pg_dump to dst, dropping the
// extension statements that only an extension owner (the bootstrap superuser) may
// run: `DROP EXTENSION ...` and `COMMENT ON EXTENSION ...`. CNPG preinstalls
// extensions such as pg_stat_statements owned by the superuser, so a restore run as
// the unprivileged app role aborts on these (psql --set ON_ERROR_STOP=1). The
// `CREATE EXTENSION IF NOT EXISTS` lines are kept on purpose: they are a harmless
// no-op when the extension already exists on the target, and still recreate
// user-installed trusted extensions. Filtering is suppressed inside
// `COPY ... FROM stdin` data blocks so row data streams through byte-for-byte.
func copyPlainSQLFilteringExtensions(dst io.Writer, r *bufio.Reader) error {
	inCopy := false
	for {
		line, readErr := r.ReadBytes('\n')
		if len(line) > 0 {
			if inCopy {
				if _, err := dst.Write(line); err != nil {
					return err
				}
				if isCopyTerminator(line) {
					inCopy = false
				}
			} else {
				trimmed := strings.TrimRight(string(line), "\r\n")
				switch {
				case strings.HasPrefix(trimmed, "DROP EXTENSION "),
					strings.HasPrefix(trimmed, "COMMENT ON EXTENSION "):
					// skip: owner-only statement, unnecessary for a data restore
				default:
					if _, err := dst.Write(line); err != nil {
						return err
					}
					if strings.HasPrefix(trimmed, "COPY ") && strings.HasSuffix(trimmed, "FROM stdin;") {
						inCopy = true
					}
				}
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

// isCopyTerminator reports whether line is the pg_dump COPY end marker `\.`.
func isCopyTerminator(line []byte) bool {
	return strings.TrimRight(string(line), "\r\n") == `\.`
}

// importPostgres restores src into the database chosen by sniffing the first bytes.
// If the stream starts with the pg_dump custom-format magic ("PGDMP"), pg_restore is
// used; otherwise psql is used (plain SQL). Pass forced=PgFormatSQL or PgFormatCustom
// to skip auto-detect.
func importPostgres(ctx context.Context, dsn string, forced PostgresFormat, src io.Reader) error {
	br := bufio.NewReader(src)
	// Peek up to len(pgCustomMagic) bytes — fewer is fine for tiny inputs.
	head, _ := br.Peek(len(pgCustomMagic))

	format := forced
	if format == "" {
		if len(head) >= len(pgCustomMagic) && bytes.Equal(head[:len(pgCustomMagic)], pgCustomMagic) {
			format = PgFormatCustom
		} else {
			format = PgFormatSQL
		}
	}

	if format == PgFormatCustom {
		return importPostgresCustom(ctx, dsn, br)
	}
	return importPostgresSQL(ctx, dsn, br)
}

// importPostgresSQL pipes a plain-SQL dump straight into psql. ON_ERROR_STOP makes
// any failed statement abort the whole restore (the export-side filter already
// removed the owner-only extension statements, so a well-formed dump runs clean).
func importPostgresSQL(ctx context.Context, dsn string, src io.Reader) error {
	cmd := exec.CommandContext(ctx,
		"psql",
		"--no-psqlrc",
		"--quiet",
		"--variable=ON_ERROR_STOP=1",
		"--dbname="+dsn,
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("postgres restore stdin pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start postgres restore: %w", err)
	}

	var copyErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, copyErr = io.Copy(stdin, src)
		_ = stdin.Close()
	}()

	select {
	case <-done:
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		_ = stdin.Close()
		<-done
	}

	if err := cmd.Wait(); err != nil {
		if copyErr != nil {
			return fmt.Errorf("postgres restore failed: %w (copy: %v): %s", err, copyErr, trimStderr(stderr.String()))
		}
		return fmt.Errorf("postgres restore failed: %w: %s", err, trimStderr(stderr.String()))
	}
	if copyErr != nil {
		return fmt.Errorf("copy import data: %w: %s", copyErr, trimStderr(stderr.String()))
	}
	return nil
}

// importPostgresCustom restores a pg_dump custom-format archive with pg_restore.
//
// A custom archive is a binary container, so we can't line-filter it the way we do
// the plain-SQL dump. Instead we buffer it to a temp file (pg_restore needs a
// seekable archive to read its table of contents) and drive the restore from a
// filtered TOC: every EXTENSION entry — the CREATE/DROP for the extension and its
// COMMENT — is dropped. CNPG preinstalls extensions such as pg_stat_statements owned
// by the bootstrap superuser, and the unprivileged app role may not DROP, CREATE, or
// COMMENT them; pg_restore would otherwise emit those statements, fail two of them,
// and exit non-zero even though every table and row restored fine. The extensions
// already exist on the freshly provisioned target, so skipping their TOC entries is
// safe.
func importPostgresCustom(ctx context.Context, dsn string, src io.Reader) error {
	archive, err := os.CreateTemp("", "pgrestore-*.dump")
	if err != nil {
		return fmt.Errorf("create temp archive: %w", err)
	}
	archivePath := archive.Name()
	defer func() {
		_ = archive.Close()
		_ = os.Remove(archivePath)
	}()

	if _, err := io.Copy(archive, src); err != nil {
		return fmt.Errorf("buffer custom archive: %w", err)
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("flush custom archive: %w", err)
	}

	// List the archive's table of contents, then drop the EXTENSION entries.
	var tocBuf, listErr bytes.Buffer
	listCmd := exec.CommandContext(ctx, "pg_restore", "--list", archivePath)
	listCmd.Stdout = &tocBuf
	listCmd.Stderr = &listErr
	if err := listCmd.Run(); err != nil {
		return fmt.Errorf("pg_restore --list failed: %w: %s", err, trimStderr(listErr.String()))
	}

	listPath, err := writeFilteredTOC(tocBuf.Bytes())
	if err != nil {
		return fmt.Errorf("filter restore TOC: %w", err)
	}
	defer func() { _ = os.Remove(listPath) }()

	var stderr bytes.Buffer
	restoreCmd := exec.CommandContext(ctx,
		"pg_restore",
		"--clean",
		"--if-exists",
		"--no-owner",
		"--no-privileges",
		"--use-list="+listPath,
		"--dbname="+dsn,
		archivePath,
	)
	restoreCmd.Stderr = &stderr
	if err := restoreCmd.Run(); err != nil {
		return fmt.Errorf("postgres restore failed: %w: %s", err, trimStderr(stderr.String()))
	}
	return nil
}

// writeFilteredTOC writes a pg_restore --use-list file derived from a `pg_restore
// --list` dump, with every EXTENSION-related entry removed. TOC lines look like
// `123; 3079 16384 EXTENSION - pg_stat_statements` and `456; 0 0 COMMENT - EXTENSION
// pg_stat_statements`; both carry the word EXTENSION as a space-delimited token, so
// matching " EXTENSION " on the (non-comment) data lines catches them without
// touching tables, sequences, or data. Returns the path to a temp list file.
func writeFilteredTOC(toc []byte) (string, error) {
	list, err := os.CreateTemp("", "pgrestore-toc-*.lst")
	if err != nil {
		return "", err
	}
	defer func() { _ = list.Close() }()

	w := bufio.NewWriter(list)
	sc := bufio.NewScanner(bytes.NewReader(toc))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		// Keep blank lines and pg_restore's own `;`-prefixed comment header as-is.
		if !strings.HasPrefix(trimmed, ";") && strings.Contains(line, " EXTENSION ") {
			continue
		}
		if _, err := w.WriteString(line + "\n"); err != nil {
			return "", err
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	if err := w.Flush(); err != nil {
		return "", err
	}
	return list.Name(), nil
}

func trimStderr(s string) string {
	const max = 1024
	if len(s) > max {
		s = s[:max] + "...(truncated)"
	}
	return redactDSN(s)
}

var dsnCredentialPattern = regexp.MustCompile(`(mongodb|postgresql|postgres)://[^\s@]*@`)

// redactDSN replaces user:password credentials in connection-string URIs with
// ***:*** to prevent password leakage in error messages and logs.
func redactDSN(s string) string {
	return dsnCredentialPattern.ReplaceAllString(s, "${1}://***:***@")
}
