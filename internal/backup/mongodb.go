package backup

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"strings"
)

// exportMongo runs mongodump --archive --gzip and streams the resulting archive to dst.
// The archive contains all collections in the project's database, packaged as a single
// gzipped stream suitable for `mongorestore --archive --gzip`.
func exportMongo(ctx context.Context, dsn string, dst io.Writer) error {
	cmd := exec.CommandContext(ctx,
		"mongodump",
		"--uri="+dsn,
		"--archive",
		"--gzip",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("mongodump stdout pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start mongodump: %w", err)
	}

	reader := bufio.NewReader(stdout)
	if _, err := reader.Peek(1); err != nil {
		waitErr := cmd.Wait()
		if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) {
			return fmt.Errorf("mongodump produced no output (exit: %v): %s", waitErr, trimStderr(stderr.String()))
		}
		return fmt.Errorf("mongodump read: %w (exit: %v): %s", err, waitErr, trimStderr(stderr.String()))
	}

	if _, copyErr := io.Copy(dst, reader); copyErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("copy mongodump output: %w", copyErr)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("mongodump exited with error: %w: %s", err, trimStderr(stderr.String()))
	}
	return nil
}

// stripMongoDatabase removes the database component from a MongoDB connection string.
// mongorestore with --archive treats a database in the URI as an implicit --db flag,
// which causes a fatal deprecation error.
func stripMongoDatabase(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	u.Path = "/"
	return u.String()
}

// importMongo streams src into mongorestore --archive --gzip --drop.
// The archive is assumed to be a gzipped mongodump --archive stream; mongorestore
// detects the format automatically from --archive + --gzip flags.
func importMongo(ctx context.Context, dsn string, src io.Reader) error {
	restoreURI := stripMongoDatabase(dsn)
	cmd := exec.CommandContext(ctx,
		"mongorestore",
		"--uri="+restoreURI,
		"--archive",
		"--gzip",
		"--drop",
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mongorestore stdin pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	// mongorestore writes progress/diagnostics to stdout as well; capture it
	// so failures are diagnosable when stderr alone is empty.
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start mongorestore: %w", err)
	}

	var copyErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, copyErr = io.Copy(stdin, src)
		_ = stdin.Close()
	}()

	// Wait for the copy goroutine, but bail out early if the context is
	// cancelled (e.g. client disconnect). Closing stdin unblocks a pending
	// write; the process kill unblocks a stuck read from the HTTP body.
	select {
	case <-done:
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		_ = stdin.Close()
		<-done
	}

	if err := cmd.Wait(); err != nil {
		combined := trimStderr(stderr.String() + stdout.String())
		
		// mongorestore fails with EOF if the archive contains no collections/documents.
		// We treat restoring an empty database as a success rather than a 500 error.
		if strings.Contains(combined, "Failed: EOF") && strings.Contains(combined, "0 document(s) restored successfully") {
			return nil
		}
		
		if copyErr != nil {
			return fmt.Errorf("mongorestore failed: %w (copy: %v): %s", err, copyErr, combined)
		}
		return fmt.Errorf("mongorestore failed: %w: %s", err, combined)
	}
	if copyErr != nil {
		return fmt.Errorf("copy import data: %w: %s", copyErr, trimStderr(stderr.String()))
	}
	return nil
}
