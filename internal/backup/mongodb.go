package backup

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

// exportMongo runs mongodump --archive --gzip and streams the resulting archive to dst.
// The archive contains all databases the user can read, packaged as a single gzipped stream
// suitable for `mongorestore --archive --gzip`.
func exportMongo(ctx context.Context, dsn string, dst io.Writer) error {
	cmd := exec.CommandContext(ctx,
		"mongodump",
		"--uri="+dsn,
		"--archive",
		"--gzip",
		"--quiet",
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
		_ = cmd.Wait()
		if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) {
			return fmt.Errorf("mongodump produced no output: %s", trimStderr(stderr.String()))
		}
		return fmt.Errorf("mongodump read: %w: %s", err, trimStderr(stderr.String()))
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

// importMongo streams src into mongorestore --archive --gzip --drop.
// The archive is assumed to be a gzipped mongodump --archive stream; mongorestore
// detects the format automatically from --archive + --gzip flags.
func importMongo(ctx context.Context, dsn string, src io.Reader) error {
	cmd := exec.CommandContext(ctx,
		"mongorestore",
		"--uri="+dsn,
		"--archive",
		"--gzip",
		"--drop",
		"--quiet",
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mongorestore stdin pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

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

	<-done

	if err := cmd.Wait(); err != nil {
		if copyErr != nil {
			return fmt.Errorf("mongorestore failed: %w (copy: %v): %s", err, copyErr, trimStderr(stderr.String()))
		}
		return fmt.Errorf("mongorestore failed: %w: %s", err, trimStderr(stderr.String()))
	}
	if copyErr != nil {
		return fmt.Errorf("copy import data: %w: %s", copyErr, trimStderr(stderr.String()))
	}
	return nil
}
